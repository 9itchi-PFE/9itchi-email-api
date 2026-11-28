package services

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

const (
	// maxSearchSkills is how many skills of the CV go into a search.
	maxSearchSkills = 10
	// recruiterListLimit is how many people GET /recruiters returns.
	recruiterListLimit = 200
	// defaultContacts is how many people a search finds when the student does not say; maxContacts the most.
	defaultContacts = 5
	maxContacts     = 10
	// searchProvider is what recruiter_searches.provider says: the pool and the finders chain.
	searchProvider = "chain"
)

// tunis is the time zone the daily limit follows (no daylight saving since 2009).
var tunis = time.FixedZone("Africa/Tunis", 3600)

// RecruiterService finds recruiters for a student (Recruiter Radar). Handlers stay thin.
type RecruiterService interface {
	// POST /recruiters/search: companies that fit what the student looks for and the right people there
	Search(ctx context.Context, userID string, req api.RecruiterSearchRequest) (*api.RecruiterSearchResponse, *models.ApiError)
	// POST /recruiters/company: the right people at one company (an offer's), counted only when someone is found
	AtCompany(ctx context.Context, userID string, req api.CompanyRecruiterRequest) (*api.RecruiterSearchResponse, *models.ApiError)
	// GET /recruiters: every recruiter found for the student, each person once
	List(ctx context.Context, userID string) (*api.RecruitersResponse, *models.ApiError)
	// GET /recruiters/allowance: the plan's daily limit of searches and how much of it is used today
	Allowance(ctx context.Context, userID string) (*api.RecruiterAllowance, *models.ApiError)
}

// RecruiterDeps are the collaborators of the recruiter service (all mockable).
type RecruiterDeps struct {
	Repo repositories.RecruiterRepository
	// Pool is the shared prospect pool (companies, contacts, lookups).
	Pool repositories.ProspectRepository
	// Offers gives the companies of the platform's open offers (nil: none).
	Offers core.OfferCompanySource
	// Finders are the contact finders in chain order; Discoverers the company discoverers.
	Finders     []core.ContactFinder
	Discoverers []core.CompanyDiscoverer
	// Caps is the monthly credit cap per provider name (absent: no cap). Credits receives the credits a provider
	// spent (statistics-api); nil drops them.
	Caps    map[string]int
	Credits func(provider string, amount int64)
	Skills  core.SkillSource
	Quotas  core.QuotaSource
	Config  utils.AppConfig
	Now     func() time.Time
}

type recruiterService struct {
	RecruiterDeps
}

func NewRecruiterService(d RecruiterDeps) RecruiterService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &recruiterService{d}
}

// tunisDay is the start of the current day in Tunis and the start of the next one.
func (s *recruiterService) tunisDay() (time.Time, time.Time) {
	return tunisDayOf(s.Now())
}

// tunisDayOf is the start of the day of t in Tunis and the start of the next one.
func tunisDayOf(t time.Time) (time.Time, time.Time) {
	now := t.In(tunis)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tunis)
	return start, start.AddDate(0, 0, 1)
}

// allowance reads the plan's daily limit and how many searches the user made today. Limit is nil when unlimited
// (key absent or negative).
func (s *recruiterService) allowance(ctx context.Context, userID string) (*api.RecruiterAllowance, *models.ApiError) {
	quotas, err := s.Quotas.Quotas(ctx, userID)
	if err != nil {
		return nil, peerError(err, "plan not found")
	}
	start, end := s.tunisDay()
	n, err := s.Repo.CountSearchesSince(ctx, userID, start)
	if err != nil {
		return nil, repoError(err, "not found")
	}
	out := &api.RecruiterAllowance{Used: n, ResetsAt: end.UTC()}
	if limit, ok := quotas[core.QuotaRecruiterSearches]; ok && limit >= 0 {
		left := max(limit-n, 0)
		out.Limit, out.Remaining = &limit, &left
	}
	return out, nil
}

func (s *recruiterService) Allowance(ctx context.Context, userID string) (*api.RecruiterAllowance, *models.ApiError) {
	return s.allowance(ctx, userID)
}

// searchLeft is a 402 when the plan has no recruiter search or today's are used up.
func (s *recruiterService) searchLeft(ctx context.Context, userID string) *models.ApiError {
	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return apiErr
	}
	switch {
	case a.Limit != nil && *a.Limit == 0:
		return apiError(http.StatusPaymentRequired, "recruiter search is part of the subscription")
	case a.Limit != nil && a.Used >= *a.Limit:
		return apiError(http.StatusPaymentRequired, "daily limit of recruiter searches reached")
	}
	return nil
}

func (s *recruiterService) Search(ctx context.Context, userID string, req api.RecruiterSearchRequest) (*api.RecruiterSearchResponse, *models.ApiError) {
	if req.ResumeID == "" {
		return nil, apiError(http.StatusBadRequest, "resume_id is required")
	}
	contacts := req.Contacts
	if contacts == 0 {
		contacts = defaultContacts
	}
	if contacts < 1 || contacts > maxContacts {
		return nil, apiError(http.StatusBadRequest, "contacts must be between 1 and 10")
	}
	country := strings.ToUpper(strings.TrimSpace(req.Country))
	if country == "" {
		country = "TN"
	}
	if !countryRe.MatchString(country) {
		return nil, apiError(http.StatusBadRequest, "invalid country")
	}
	token, err := utils.IssueToken(s.Config, utils.Claims{ID: userID, Role: "user", Type: utils.TokenAccess}, accessTokenTTL)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "internal error")
	}
	skills, err := s.Skills.Skills(ctx, token, req.ResumeID)
	if err != nil {
		return nil, peerError(err, "resume not found")
	}
	q := prospectQuery{
		Skills: normalizeTerms(skills), Keywords: normalizeTerms(req.Keywords),
		Goal: strings.ToLower(strings.Join(strings.Fields(req.Goal), " ")), Contract: req.Contract, Country: country,
		City: strings.Join(strings.Fields(req.City), " "), Contacts: contacts,
	}
	if len(q.Skills) > maxSearchSkills {
		q.Skills = q.Skills[:maxSearchSkills]
	}
	key, _ := json.Marshal(prospectQuery{
		Skills: q.Skills, Keywords: q.Keywords, Goal: q.Goal, Contract: q.Contract, Country: q.Country,
		City: strings.ToLower(q.City), Contacts: q.Contacts,
	})
	sum := sha256.Sum256(key)
	hash := hex.EncodeToString(sum[:])
	start, _ := s.tunisDay()

	if prev, err := s.Repo.LatestSearch(ctx, userID, hash, start); err == nil {
		items, err := s.Repo.ListBySearch(ctx, userID, prev.ID)
		if err != nil {
			return nil, repoError(err, "search not found")
		}
		return s.searchResponse(ctx, userID, prev, items, true, api.ProspectStats{})
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, repoError(err, "search not found")
	}

	if apiErr := s.searchLeft(ctx, userID); apiErr != nil {
		return nil, apiErr
	}

	pctx, cancel := context.WithTimeout(ctx, prospectTimeout)
	picks, stats, err := s.prospect(pctx, userID, q)
	cancel()
	if err != nil {
		return nil, repoError(err, "search not found")
	}
	search, saved, err := s.Repo.SaveSearch(ctx, &db.RecruiterSearch{
		UserID: userID, Query: key, QueryHash: hash, Provider: searchProvider,
		Stats: &db.SearchStats{
			CompaniesFromPool: stats.CompaniesFromPool, CompaniesDiscovered: stats.CompaniesDiscovered,
			ContactsFromPool: stats.ContactsFromPool, ContactsFound: stats.ContactsFound,
		},
	}, toProspectRows(picks))
	if err != nil {
		return nil, repoError(err, "search not found")
	}
	return s.searchResponse(ctx, userID, search, saved, false, stats)
}

func (s *recruiterService) searchResponse(ctx context.Context, userID string, search *db.RecruiterSearch, rows []db.Recruiter, cached bool, stats api.ProspectStats) (*api.RecruiterSearchResponse, *models.ApiError) {
	sortRecruiters(rows)
	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	return &api.RecruiterSearchResponse{
		SearchID: search.ID, Cached: cached, Items: toRecruiterItems(rows), Allowance: *a, Stats: stats,
	}, nil
}

func (s *recruiterService) List(ctx context.Context, userID string) (*api.RecruitersResponse, *models.ApiError) {
	rows, err := s.Repo.List(ctx, userID, recruiterListLimit)
	if err != nil {
		return nil, repoError(err, "recruiter not found")
	}
	return &api.RecruitersResponse{Items: toRecruiterItems(rows)}, nil
}

func toRecruiterItems(rows []db.Recruiter) []api.RecruiterItem {
	out := make([]api.RecruiterItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.RecruiterItem{
			ID: r.ID, Name: r.Name, Title: r.Title, Company: r.Company, Domain: r.Domain, Email: r.Email,
			LinkedInURL: r.LinkedInURL, Confidence: r.Confidence, Verified: r.Verified, CompanySummary: r.CompanySummary,
			MatchedKeywords: nonNilTerms(r.MatchedKeywords), LogoURL: httpLogoURL(r.LogoURL), CreatedAt: r.CreatedAt,
		})
	}
	return out
}

// httpLogoURL keeps a logo URL only when it is an absolute http(s) URL with a host and no credentials, else "".
func httpLogoURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return raw
}

// normalizeTerms trims, lower-cases, sorts and de-duplicates keywords or skills.
func normalizeTerms(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.Join(strings.Fields(s), " ")); s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// hostOf is the host name of a domain typed as "acme.tn", "www.acme.tn" or a whole link, lower case without
// "www.". Empty stays empty; ok is false when it is not a host name.
func hostOf(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == "" || !strings.Contains(host, ".") || strings.ContainsAny(host, " _") {
		return "", false
	}
	return host, true
}

// nonNilTerms keeps a list from reading as null in JSON.
func nonNilTerms(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// sortRecruiters orders the people of a search the same way fresh or given back: recruiters first, then verified
// addresses, then confidence.
func sortRecruiters(rows []db.Recruiter) {
	slices.SortStableFunc(rows, func(a, b db.Recruiter) int {
		return cmp.Or(cmp.Compare(titleBucket(b.Title, ""), titleBucket(a.Title, "")), cmp.Compare(boolInt(b.Verified), boolInt(a.Verified)),
			cmp.Compare(b.Confidence, a.Confidence), strings.Compare(a.Name, b.Name))
	})
}
