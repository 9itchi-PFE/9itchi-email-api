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
	// recruiterLimit is how many people one search asks the provider for.
	recruiterLimit = 20
	// maxSearchSkills is how many skills of the CV go into a search.
	maxSearchSkills = 10
	// recruiterListLimit is how many people GET /recruiters returns.
	recruiterListLimit = 200
)

// tunis is the time zone the daily limit follows (no daylight saving since 2009).
var tunis = time.FixedZone("Africa/Tunis", 3600)

// RecruiterService finds recruiters for a student (Recruiter Radar). Handlers stay thin.
type RecruiterService interface {
	// POST /recruiters/search: recruiters for a CV's skills, keywords, a company or a domain
	Search(ctx context.Context, userID string, req api.RecruiterSearchRequest) (*api.RecruiterSearchResponse, *models.ApiError)
	// GET /recruiters: every recruiter found for the student, each person once
	List(ctx context.Context, userID string) (*api.RecruitersResponse, *models.ApiError)
	// GET /recruiters/allowance: the plan's daily limit of searches and how much of it is used today
	Allowance(ctx context.Context, userID string) (*api.RecruiterAllowance, *models.ApiError)
}

// RecruiterDeps are the collaborators of the recruiter service (all mockable).
type RecruiterDeps struct {
	Repo   repositories.RecruiterRepository
	Finder core.RecruiterFinder
	Skills core.SkillSource
	Quotas core.QuotaSource
	Config utils.AppConfig
	Now    func() time.Time
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
	now := s.Now().In(tunis)
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

func (s *recruiterService) Search(ctx context.Context, userID string, req api.RecruiterSearchRequest) (*api.RecruiterSearchResponse, *models.ApiError) {
	domain, ok := hostOf(req.Domain)
	if !ok {
		return nil, apiError(http.StatusBadRequest, "invalid domain")
	}
	q := core.RecruiterQuery{
		Keywords: normalizeTerms(req.Keywords), Company: strings.Join(strings.Fields(req.Company), " "),
		Domain: domain, Location: strings.Join(strings.Fields(req.Location), " "), Limit: recruiterLimit,
	}
	if req.ResumeID == "" && len(q.Keywords) == 0 && q.Company == "" && q.Domain == "" {
		return nil, apiError(http.StatusBadRequest, "give a CV, keywords, a company or a domain")
	}
	if req.ResumeID != "" {
		token, err := utils.IssueToken(s.Config, utils.Claims{ID: userID, Role: "user", Type: utils.TokenAccess}, accessTokenTTL)
		if err != nil {
			return nil, apiError(http.StatusInternalServerError, "internal error")
		}
		skills, err := s.Skills.Skills(ctx, token, req.ResumeID)
		if err != nil {
			return nil, peerError(err, "resume not found")
		}
		q.Skills = normalizeTerms(skills)
		if len(q.Skills) > maxSearchSkills {
			q.Skills = q.Skills[:maxSearchSkills]
		}
	}
	key, _ := json.Marshal(core.RecruiterQuery{
		Skills: q.Skills, Keywords: q.Keywords, Company: strings.ToLower(q.Company), Domain: q.Domain,
		Location: strings.ToLower(q.Location), Limit: q.Limit,
	})
	sum := sha256.Sum256(key)
	hash := hex.EncodeToString(sum[:])
	start, _ := s.tunisDay()

	if prev, err := s.Repo.LatestSearch(ctx, userID, hash, start); err == nil {
		items, err := s.Repo.ListBySearch(ctx, userID, prev.ID)
		if err != nil {
			return nil, repoError(err, "search not found")
		}
		return s.searchResponse(ctx, userID, prev, items, true)
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, repoError(err, "search not found")
	}

	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	switch {
	case a.Limit != nil && *a.Limit == 0:
		return nil, apiError(http.StatusPaymentRequired, "recruiter search is part of the subscription")
	case a.Limit != nil && a.Used >= *a.Limit:
		return nil, apiError(http.StatusPaymentRequired, "daily limit of recruiter searches reached")
	}

	found, err := s.Finder.Find(ctx, q)
	if err != nil {
		// Only the provider and the error: never the people, nor what the student searched for.
		utils.Logger.Logger.Warnw("recruiter provider error", "provider", s.Finder.Name(), "error", err)
		return nil, apiError(http.StatusBadGateway, "recruiter search is unavailable")
	}
	rows := toRecruiterRows(found, q.Limit)
	search, saved, err := s.Repo.SaveSearch(ctx, &db.RecruiterSearch{
		UserID: userID, Query: key, QueryHash: hash, Provider: s.Finder.Name(),
	}, rows)
	if err != nil {
		return nil, repoError(err, "search not found")
	}
	return s.searchResponse(ctx, userID, search, saved, false)
}

func (s *recruiterService) searchResponse(ctx context.Context, userID string, search *db.RecruiterSearch, rows []db.Recruiter, cached bool) (*api.RecruiterSearchResponse, *models.ApiError) {
	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	return &api.RecruiterSearchResponse{
		SearchID: search.ID, Provider: search.Provider, Cached: cached, Items: toRecruiterItems(rows), Allowance: *a,
	}, nil
}

func (s *recruiterService) List(ctx context.Context, userID string) (*api.RecruitersResponse, *models.ApiError) {
	rows, err := s.Repo.List(ctx, userID, recruiterListLimit)
	if err != nil {
		return nil, repoError(err, "recruiter not found")
	}
	return &api.RecruitersResponse{Items: toRecruiterItems(rows)}, nil
}

// toRecruiterRows keeps at most limit people with a name, confidence within 0 to 100, most certain first, each
// with the key that tells the same person apart across searches.
func toRecruiterRows(found []core.Recruiter, limit int) []db.Recruiter {
	out := make([]db.Recruiter, 0, len(found))
	for _, f := range found {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		r := db.Recruiter{
			Name: name, Title: strings.TrimSpace(f.Title), Company: strings.TrimSpace(f.Company),
			Domain: strings.ToLower(strings.TrimSpace(f.Domain)), Email: strings.TrimSpace(f.Email),
			LinkedInURL: strings.TrimSpace(f.LinkedInURL), Confidence: min(max(f.Confidence, 0), 100),
		}
		switch {
		case r.Email != "":
			r.DedupeKey = strings.ToLower(r.Email)
		case r.LinkedInURL != "":
			r.DedupeKey = strings.ToLower(r.LinkedInURL)
		default:
			r.DedupeKey = strings.ToLower(r.Name + "|" + r.Company)
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b db.Recruiter) int {
		return cmp.Or(cmp.Compare(b.Confidence, a.Confidence), strings.Compare(a.Name, b.Name))
	})
	return out
}

func toRecruiterItems(rows []db.Recruiter) []api.RecruiterItem {
	out := make([]api.RecruiterItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.RecruiterItem{
			ID: r.ID, Name: r.Name, Title: r.Title, Company: r.Company, Domain: r.Domain, Email: r.Email,
			LinkedInURL: r.LinkedInURL, Confidence: r.Confidence, CreatedAt: r.CreatedAt,
		})
	}
	return out
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
