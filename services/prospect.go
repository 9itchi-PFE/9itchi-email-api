package services

import (
	"cmp"
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

const (
	// prospectTimeout is the time budget of one search.
	prospectTimeout = 45 * time.Second
	// poolCompanyLimit is how many pool companies one search reads; offerCompanyLimit how many job-api gives.
	poolCompanyLimit  = 60
	offerCompanyLimit = 30
	// discoverLimit is how many companies one discoverer is asked for.
	discoverLimit = 20
	// maxCompanyVisits caps the companies whose people one search looks for.
	maxCompanyVisits = 25
	// perCompany is the most people of one company in a search.
	perCompany = 2
	// contactedWindow: a company the student got people of within it is not proposed again.
	contactedWindow = 90 * 24 * time.Hour
	// lookupWindow: a provider asked for a domain within it is not asked again (0 results included).
	lookupWindow = 90 * 24 * time.Hour
	// contactFreshness: pool people checked or updated within it are used without asking anyone.
	contactFreshness = 180 * 24 * time.Hour
	// companySummaryLen is the length of the company summary shown with a person.
	companySummaryLen = 300
)

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// prospectQuery is a search as normalized: the cache key and the query stored with the search.
type prospectQuery struct {
	Skills   []string `json:"skills"`
	Keywords []string `json:"keywords"`
	Goal     string   `json:"goal"`
	Contract string   `json:"contract"`
	Country  string   `json:"country"`
	City     string   `json:"city"`
	Contacts int      `json:"contacts"`
}

// candidate is a company that may fit the search, best first by order.
type candidate struct {
	company db.Company // ID "" while it is not in the pool (an offer's company without a domain)
	matched []string
	order   int
}

// prospect is one person picked for the search.
type prospect struct {
	cand     *candidate
	contact  db.Contact
	fromPool bool
}

// prospect runs the pool-first pipeline: companies from the pool, the platform's offers, then discoverers when
// short; their people from the pool, else from the finders. Provider failures are logged and skipped; only a
// database error fails the search.
func (s *recruiterService) prospect(ctx context.Context, userID string, q prospectQuery) ([]prospect, api.ProspectStats, error) {
	var stats api.ProspectStats
	terms := searchTerms(q)
	recent, err := s.Repo.RecentDomains(ctx, userID, s.Now().Add(-contactedWindow))
	if err != nil {
		return nil, stats, err
	}
	skip := map[string]bool{}
	for _, d := range recent {
		skip[d] = true
	}
	var cands []*candidate
	byKey := map[string]*candidate{}
	add := func(c db.Company, extra []string) bool {
		key := c.Domain
		if key == "" {
			key = "name:" + wordText(c.Name)
		}
		if skip[c.Domain] || key == "name:" {
			return false
		}
		matched := matchedTerms(terms, c.SearchText)
		for _, t := range extra {
			if t = wordText(t); t != "" && !slices.Contains(matched, t) && slices.Contains(terms, t) {
				matched = append(matched, t)
			}
		}
		if prev, ok := byKey[key]; ok {
			for _, t := range matched {
				if !slices.Contains(prev.matched, t) {
					prev.matched = append(prev.matched, t)
				}
			}
			return false
		}
		cand := &candidate{company: c, matched: matched, order: len(cands)}
		byKey[key] = cand
		cands = append(cands, cand)
		return true
	}

	if ts := tsQuery(terms); ts != "" {
		rows, err := s.Pool.SearchCompanies(ctx, ts, q.Country, poolCompanyLimit)
		if err != nil {
			return nil, stats, err
		}
		for _, c := range rows {
			if add(c, nil) {
				stats.CompaniesFromPool++
			}
		}
	}

	if s.Offers != nil && len(terms) > 0 {
		items, err := s.Offers.OfferCompanies(ctx, terms, q.Country, offerCompanyLimit)
		if err != nil {
			utils.Logger.Logger.Warnw("offer companies unavailable", "error", err)
		}
		for _, it := range items {
			c := db.Company{
				Name: strings.TrimSpace(it.Company), Description: cut(it.Description, 2000), City: strings.TrimSpace(it.City),
				Country: validCountry(it.Country), Source: "offer",
			}
			created := false
			if domain, ok := hostOf(it.Domain); ok && domain != "" {
				c.Domain = domain
				saved, isNew, err := s.savePoolCompany(ctx, c)
				if err != nil {
					return nil, stats, err
				}
				c, created = *saved, isNew
			} else {
				c.SearchText = companySearchText(c)
			}
			if add(c, it.MatchedTerms) {
				if created {
					stats.CompaniesDiscovered++
				} else if c.ID != "" {
					stats.CompaniesFromPool++
				}
			}
		}
	}
	sortCandidates(cands)

	if len(cands) < q.Contacts && len(terms) > 0 {
		for _, d := range s.Discoverers {
			if len(cands) >= q.Contacts || ctx.Err() != nil {
				break
			}
			if !d.Ready() {
				continue
			}
			found, err := d.Discover(ctx, core.CompanyQuery{Terms: terms, Country: q.Country, City: q.City, Limit: discoverLimit})
			if err != nil {
				utils.Logger.Logger.Warnw("company discovery error", "provider", d.Name(), "error", err)
				continue
			}
			utils.Logger.Logger.Infow("company discovery", "provider", d.Name(), "companies", len(found))
			for _, f := range found {
				domain, ok := hostOf(f.Domain)
				if !ok || domain == "" {
					continue
				}
				saved, created, err := s.savePoolCompany(ctx, db.Company{
					Domain: domain, Name: strings.TrimSpace(f.Name), Description: cut(f.Description, 2000), Industry: f.Industry,
					SizeRange: f.SizeRange, Country: validCountry(f.Country), City: f.City, Keywords: f.Keywords, Source: d.Name(),
				})
				if err != nil {
					return nil, stats, err
				}
				if add(*saved, nil) {
					if created {
						stats.CompaniesDiscovered++
					} else {
						stats.CompaniesFromPool++
					}
				}
			}
		}
		sortCandidates(cands)
	}

	var picks []prospect
	rest := map[*candidate][]db.Contact{}
	fromPool := map[*candidate]bool{}
	for i, c := range cands {
		if len(picks) >= q.Contacts || i >= maxCompanyVisits || ctx.Err() != nil {
			break
		}
		people, pooled, err := s.companyPeople(ctx, c, skip)
		if err != nil {
			return nil, stats, err
		}
		if len(people) == 0 {
			continue
		}
		rankPeople(people, c.company.SizeRange)
		picks = append(picks, prospect{cand: c, contact: people[0], fromPool: pooled})
		rest[c], fromPool[c] = people[1:min(len(people), perCompany)], pooled
	}
	for _, c := range cands {
		if len(picks) >= q.Contacts {
			break
		}
		for _, p := range rest[c] {
			if len(picks) >= q.Contacts {
				break
			}
			picks = append(picks, prospect{cand: c, contact: p, fromPool: fromPool[c]})
		}
	}
	slices.SortStableFunc(picks, func(a, b prospect) int {
		return cmp.Or(
			cmp.Compare(titleBucket(b.contact.Title, b.cand.company.SizeRange), titleBucket(a.contact.Title, a.cand.company.SizeRange)),
			cmp.Compare(boolInt(b.contact.Verification == "valid"), boolInt(a.contact.Verification == "valid")),
			cmp.Compare(b.contact.Confidence, a.contact.Confidence), cmp.Compare(a.cand.order, b.cand.order))
	})
	for _, p := range picks {
		if p.fromPool {
			stats.ContactsFromPool++
		} else {
			stats.ContactsFound++
		}
	}
	utils.Logger.Logger.Infow("recruiter search", "companies", len(cands), "people", len(picks),
		"companies_from_pool", stats.CompaniesFromPool, "companies_discovered", stats.CompaniesDiscovered,
		"contacts_from_pool", stats.ContactsFromPool, "contacts_found", stats.ContactsFound)
	return picks, stats, nil
}

// companyPeople are the usable people of a company: from the pool when it has someone checked recently, else from
// the finders in chain order (a finder without its key, asked for the domain within 90 days or over its monthly cap
// is skipped), stopping at the first that gives someone. Every answer is written to the pool.
func (s *recruiterService) companyPeople(ctx context.Context, c *candidate, skip map[string]bool) ([]db.Contact, bool, error) {
	now := s.Now()
	if c.company.ID != "" {
		rows, err := s.Pool.CompanyContacts(ctx, c.company.ID, now.Add(-contactFreshness), 10)
		if err != nil {
			return nil, false, err
		}
		if people := usableContacts(rows); len(people) > 0 {
			return people, true, nil
		}
	}
	month := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, f := range s.Finders {
		if ctx.Err() != nil {
			break
		}
		name := f.Name()
		if !f.Ready() {
			continue
		}
		domain := c.company.Domain
		if domain != "" {
			asked, err := s.Pool.LookupSince(ctx, domain, name, now.Add(-lookupWindow))
			if err != nil {
				return nil, false, err
			}
			if asked {
				continue
			}
		}
		budget := math.MaxInt32
		if limit, ok := s.Caps[name]; ok {
			used, err := s.Pool.CreditsSince(ctx, name, month)
			if err != nil {
				return nil, false, err
			}
			if used >= limit {
				continue
			}
			budget = limit - used
		}
		res, err := f.Find(ctx, core.ContactQuery{Domain: domain, Company: c.company.Name, MaxCredits: budget})
		if res == nil {
			res = &core.ContactLookup{}
		}
		if res.Credits > 0 && s.Credits != nil {
			s.Credits(name, int64(res.Credits))
		}
		if res.Credits > 0 {
			if cerr := s.Pool.SaveCredits(ctx, name, res.Credits); cerr != nil {
				utils.Logger.Logger.Warnw("provider credits not logged", "provider", name, "error", cerr)
			}
		}
		if domain == "" && err == nil {
			// Asked by name: the finder told the domain (else the company is skipped).
			d, ok := hostOf(res.Domain)
			if !ok || d == "" {
				continue
			}
			if skip[d] {
				return nil, false, nil
			}
			saved, _, serr := s.savePoolCompany(ctx, db.Company{
				Domain: d, Name: cmp.Or(c.company.Name, res.Company), Description: c.company.Description,
				City: c.company.City, Country: c.company.Country, Source: cmp.Or(c.company.Source, name),
			})
			if serr != nil {
				return nil, false, serr
			}
			c.company, domain = *saved, d
		}
		if domain != "" && (err == nil || res.Credits > 0) {
			if lerr := s.Pool.SaveLookup(ctx, domain, name, len(res.Contacts), res.Credits); lerr != nil {
				return nil, false, lerr
			}
		}
		if err != nil {
			utils.Logger.Logger.Warnw("contact finder error", "provider", name, "error", err)
			continue
		}
		utils.Logger.Logger.Infow("contact finder", "provider", name, "people", len(res.Contacts), "credits", res.Credits)
		var people []db.Contact
		for _, ct := range res.Contacts {
			row, err := s.savePoolContact(ctx, c.company.ID, name, ct)
			if err != nil {
				return nil, false, err
			}
			if row != nil && usable(*row) {
				people = append(people, *row)
			}
		}
		if len(people) > 0 {
			return people, false, nil
		}
	}
	return nil, false, nil
}

// savePoolCompany writes a company to the pool, keeping what it already knew and filling what it did not. created
// is true when the domain was new to the pool.
func (s *recruiterService) savePoolCompany(ctx context.Context, c db.Company) (*db.Company, bool, error) {
	prev, err := s.Pool.CompanyByDomain(ctx, c.Domain)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		c.Keywords = normalizeTerms(c.Keywords)
		c.SearchText = companySearchText(c)
		saved, err := s.Pool.SaveCompany(ctx, &c)
		return saved, err == nil, err
	case err != nil:
		return nil, false, err
	}
	m := *prev
	m.Name = cmp.Or(m.Name, c.Name)
	m.Description = cmp.Or(m.Description, c.Description)
	m.Industry = cmp.Or(m.Industry, c.Industry)
	m.SizeRange = cmp.Or(m.SizeRange, c.SizeRange)
	m.Country = cmp.Or(m.Country, c.Country)
	m.City = cmp.Or(m.City, c.City)
	m.Keywords = normalizeTerms(append(slices.Clone(m.Keywords), c.Keywords...))
	m.SearchText = companySearchText(m)
	if m.Name == prev.Name && m.Description == prev.Description && m.Industry == prev.Industry && m.SizeRange == prev.SizeRange &&
		m.Country == prev.Country && m.City == prev.City && slices.Equal(m.Keywords, prev.Keywords) && m.SearchText == prev.SearchText {
		return prev, false, nil
	}
	saved, err := s.Pool.SaveCompany(ctx, &m)
	return saved, false, err
}

// savePoolContact writes one person a finder gave to the pool; nil when the address is not usable.
func (s *recruiterService) savePoolContact(ctx context.Context, companyID, source string, ct core.Contact) (*db.Contact, error) {
	email := strings.ToLower(strings.TrimSpace(ct.Email))
	local, host, ok := strings.Cut(email, "@")
	if !ok || local == "" || !strings.Contains(host, ".") || len(email) > 254 || companyID == "" {
		return nil, nil
	}
	row := &db.Contact{
		CompanyID: companyID, Email: email, FirstName: strings.TrimSpace(ct.FirstName), LastName: strings.TrimSpace(ct.LastName),
		Title: strings.TrimSpace(ct.Title), Department: ct.Department, LinkedInURL: strings.TrimSpace(ct.LinkedInURL),
		Source: source, Confidence: min(max(ct.Confidence, 0), 100), Verification: ct.Verification,
	}
	switch row.Department {
	case "hr", "management", "executive", "engineering":
	default:
		row.Department = "other"
	}
	switch row.Verification {
	case "valid", "accept_all", "invalid", "unknown":
		at := s.Now()
		row.VerifiedAt = &at
	default:
		row.Verification = ""
	}
	return s.Pool.SaveContact(ctx, row)
}

// companySearchText is what the pool search matches: name, industry, description and keywords, folded.
func companySearchText(c db.Company) string {
	return wordText(strings.Join(append([]string{c.Name, c.Industry, c.Description}, c.Keywords...), " "))
}

// rankPeople orders the people of one company: recruiters first, then verified addresses, then confidence.
func rankPeople(people []db.Contact, size string) {
	slices.SortStableFunc(people, func(a, b db.Contact) int {
		return cmp.Or(cmp.Compare(titleBucket(b.Title, size), titleBucket(a.Title, size)),
			cmp.Compare(boolInt(b.Verification == "valid"), boolInt(a.Verification == "valid")), cmp.Compare(b.Confidence, a.Confidence))
	})
}

func usable(c db.Contact) bool {
	return !c.OptedOut && c.Email != "" && c.Verification != "invalid"
}

func usableContacts(rows []db.Contact) []db.Contact {
	out := make([]db.Contact, 0, len(rows))
	for _, r := range rows {
		if usable(r) {
			out = append(out, r)
		}
	}
	return out
}

func sortCandidates(cands []*candidate) {
	slices.SortStableFunc(cands, func(a, b *candidate) int {
		return cmp.Or(cmp.Compare(len(b.matched), len(a.matched)), cmp.Compare(a.order, b.order))
	})
	for i, c := range cands {
		c.order = i
	}
}

func validCountry(s string) string {
	if s = strings.ToUpper(strings.TrimSpace(s)); countryRe.MatchString(s) {
		return s
	}
	return ""
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// titleBucket ranks a title: recruiters 3; founders and chiefs of a small (or unknown size) company 2; heads of
// engineering, tech or data 2; anyone else 1.
func titleBucket(title, size string) int {
	t := " " + wordText(title) + " "
	has := func(parts ...string) bool {
		return slices.ContainsFunc(parts, func(p string) bool { return strings.Contains(t, p) })
	}
	switch {
	case has("recrut", "recruit", "talent", " rh ", " hr ", "ressources humaines", "human resources", " people "):
		return 3
	case has(" founder", "fondat", " ceo ", " cto ", " coo ", " gerant"):
		if size == "" || size == "1-10" || size == "11-50" {
			return 2
		}
		return 1
	case has("head", "lead", "manager", "responsable", "directeur", "director", "chef") &&
		has("engineer", "ingenier", "tech", "data", " it ", "informatique", "developpement", "software", "logiciel"):
		return 2
	}
	return 1
}

// toProspectRows are the people picked as recruiters rows, each person (address) once.
func toProspectRows(picks []prospect) []db.Recruiter {
	out := make([]db.Recruiter, 0, len(picks))
	seen := map[string]bool{}
	for _, p := range picks {
		c, co := p.contact, p.cand.company
		key := strings.ToLower(c.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, db.Recruiter{
			DedupeKey: key, Name: cmp.Or(strings.TrimSpace(c.FirstName+" "+c.LastName), "Contact"), Title: c.Title,
			Company: cmp.Or(co.Name, co.Domain), Domain: co.Domain, Email: c.Email, LinkedInURL: c.LinkedInURL,
			Confidence: c.Confidence, ContactID: c.ID, CompanySummary: cut(co.Description, companySummaryLen),
			MatchedKeywords: slices.Clone(p.cand.matched), Verified: c.Verification == "valid",
		})
	}
	return out
}
