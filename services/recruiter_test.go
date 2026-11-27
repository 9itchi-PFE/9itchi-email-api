package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memRecruiters keeps searches and recruiters like the repository.
type memRecruiters struct {
	mu       sync.Mutex
	searches []db.RecruiterSearch
	people   []db.Recruiter
	at       func() time.Time
}

func (m *memRecruiters) CountSearchesSince(_ context.Context, userID string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.searches {
		if s.UserID == userID && !s.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (m *memRecruiters) LatestSearch(_ context.Context, userID, hash string, since time.Time) (*db.RecruiterSearch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.searches) - 1; i >= 0; i-- {
		s := m.searches[i]
		if s.UserID == userID && s.QueryHash == hash && !s.CreatedAt.Before(since) {
			return &s, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memRecruiters) SaveSearch(_ context.Context, s *db.RecruiterSearch, found []db.Recruiter) (*db.RecruiterSearch, []db.Recruiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *s
	c.ID = "search-" + string(rune('a'+len(m.searches)))
	c.CreatedAt = m.at()
	c.ResultCount = len(found)
	m.searches = append(m.searches, c)
	out := make([]db.Recruiter, 0, len(found))
	for _, f := range found {
		f.ID = "rec-" + string(rune('a'+len(m.people)))
		f.UserID, f.SearchID, f.Provider, f.CreatedAt = c.UserID, c.ID, c.Provider, c.CreatedAt
		m.people = append(m.people, f)
		out = append(out, f)
	}
	return &c, out, nil
}

func (m *memRecruiters) ListBySearch(_ context.Context, userID, searchID string) ([]db.Recruiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []db.Recruiter{}
	for _, p := range m.people {
		if p.UserID == userID && p.SearchID == searchID {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out, nil
}

func (m *memRecruiters) Get(_ context.Context, userID, id string) (*db.Recruiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.people {
		if p.UserID == userID && p.ID == id {
			return &p, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memRecruiters) List(_ context.Context, userID string, _ int) ([]db.Recruiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	out := []db.Recruiter{}
	for i := len(m.people) - 1; i >= 0; i-- {
		p := m.people[i]
		if p.UserID == userID && !seen[p.DedupeKey] {
			seen[p.DedupeKey] = true
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memRecruiters) GetSearch(_ context.Context, userID, id string) (*db.RecruiterSearch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.searches {
		if s.UserID == userID && s.ID == id {
			return &s, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memRecruiters) RecentDomains(_ context.Context, userID string, since time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for _, p := range m.people {
		if p.UserID == userID && !p.CreatedAt.Before(since) && p.Domain != "" {
			out = append(out, p.Domain)
		}
	}
	return out, nil
}

// memPool keeps the prospect pool like the repository (the full-text match is a whole-word match on search_text).
type memPool struct {
	mu        sync.Mutex
	companies []db.Company
	contacts  []db.Contact
	lookups   map[string]memLookup
	at        func() time.Time
}

type memLookup struct {
	count, credits int
	at             time.Time
}

func (m *memPool) SearchCompanies(_ context.Context, ts, country string, limit int) ([]db.Company, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []db.Company{}
	for _, c := range m.companies {
		if country != "" && c.Country != country {
			continue
		}
		for _, term := range strings.Split(ts, " | ") {
			if strings.Contains(" "+c.SearchText+" ", " "+strings.ReplaceAll(term, " <-> ", " ")+" ") {
				out = append(out, c)
				break
			}
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *memPool) CompanyByDomain(_ context.Context, domain string) (*db.Company, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.companies {
		if c.Domain == domain {
			return &c, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memPool) SaveCompany(_ context.Context, c *db.Company) (*db.Company, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := *c
	x.UpdatedAt = m.at()
	for i, p := range m.companies {
		if p.Domain == x.Domain {
			x.ID, x.Source, x.CreatedAt = p.ID, p.Source, p.CreatedAt
			m.companies[i] = x
			return &x, nil
		}
	}
	x.ID, x.CreatedAt = fmt.Sprintf("co-%d", len(m.companies)+1), m.at()
	m.companies = append(m.companies, x)
	return &x, nil
}

func (m *memPool) CompanyContacts(_ context.Context, companyID string, since time.Time, limit int) ([]db.Contact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []db.Contact{}
	for _, c := range m.contacts {
		if c.CompanyID == companyID && !c.OptedOut && !c.UpdatedAt.Before(since) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memPool) SaveContact(_ context.Context, c *db.Contact) (*db.Contact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := *c
	x.UpdatedAt = m.at()
	for i, p := range m.contacts {
		if strings.EqualFold(p.Email, x.Email) {
			x.ID, x.OptedOut, x.CreatedAt = p.ID, p.OptedOut, p.CreatedAt
			m.contacts[i] = x
			return &x, nil
		}
	}
	x.ID, x.CreatedAt = fmt.Sprintf("ct-%d", len(m.contacts)+1), m.at()
	m.contacts = append(m.contacts, x)
	return &x, nil
}

func (m *memPool) LookupSince(_ context.Context, domain, provider string, since time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.lookups[domain+"|"+provider]
	return ok && !l.at.Before(since), nil
}

func (m *memPool) SaveLookup(_ context.Context, domain, provider string, count, credits int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookups[domain+"|"+provider] = memLookup{count: count, credits: credits, at: m.at()}
	return nil
}

func (m *memPool) CreditsSince(_ context.Context, provider string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, l := range m.lookups {
		if strings.HasSuffix(k, "|"+provider) && !l.at.Before(since) {
			n += l.credits
		}
	}
	return n, nil
}

// testFinder answers two people at the domain asked for (a CTO and a recruiter with a verified address).
type testFinder struct {
	name    string
	ready   bool
	credits int
	err     error
	calls   int
	got     []core.ContactQuery
}

func (f *testFinder) Name() string { return f.name }

func (f *testFinder) Ready() bool { return f.ready }

func (f *testFinder) Find(_ context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	f.calls++
	f.got = append(f.got, q)
	if f.err != nil {
		return nil, f.err
	}
	return &core.ContactLookup{Credits: f.credits, Contacts: []core.Contact{
		{Email: "CTO@" + q.Domain, FirstName: "Karim", LastName: "Sassi", Title: "CTO", Department: "executive", Confidence: 95},
		{Email: "sarra@" + q.Domain, FirstName: "Sarra", LastName: "Gharbi", Title: "Responsable RH", Department: "hr",
			Confidence: 80, Verification: "valid"},
	}}, nil
}

type testDiscoverer struct {
	calls int
	got   core.CompanyQuery
	out   []core.Company
}

func (d *testDiscoverer) Name() string { return "hunter" }

func (d *testDiscoverer) Ready() bool { return true }

func (d *testDiscoverer) Discover(_ context.Context, q core.CompanyQuery) ([]core.Company, error) {
	d.calls++
	d.got = q
	return d.out, nil
}

type testOffers struct {
	calls int
	got   []string
	out   []core.OfferCompany
	err   error
}

func (o *testOffers) OfferCompanies(_ context.Context, terms []string, _ string, _ int) ([]core.OfferCompany, error) {
	o.calls++
	o.got = terms
	return o.out, o.err
}

type fakeSkills struct {
	skills []string
	err    error
}

func (f fakeSkills) Skills(_ context.Context, bearer, _ string) ([]string, error) {
	if bearer == "" {
		return nil, errors.New("no bearer")
	}
	return f.skills, f.err
}

type recruiterHarness struct {
	svc      *recruiterService
	repo     *memRecruiters
	pool     *memPool
	finder   *testFinder
	disc     *testDiscoverer
	offers   *testOffers
	credited map[string]int64
	now      time.Time
}

func newRecruiterHarness(quotas map[string]int) *recruiterHarness {
	h := &recruiterHarness{
		finder: &testFinder{name: "hunter", ready: true, credits: 1}, disc: &testDiscoverer{}, credited: map[string]int64{},
		offers: &testOffers{out: []core.OfferCompany{{Company: "Acme", Domain: "https://www.Acme.tn/jobs",
			Description: "Acme fait de la data et du Python à Tunis.", Country: "TN", MatchedTerms: []string{"Python"}}}},
		now: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}
	at := func() time.Time { return h.now }
	h.repo = &memRecruiters{at: at}
	h.pool = &memPool{lookups: map[string]memLookup{}, at: at}
	h.svc = NewRecruiterService(RecruiterDeps{
		Repo: h.repo, Pool: h.pool, Offers: h.offers, Finders: []core.ContactFinder{h.finder},
		Discoverers: []core.CompanyDiscoverer{h.disc}, Caps: map[string]int{"hunter": 50},
		Credits: func(p string, n int64) { h.credited[p] += n },
		Skills:  fakeSkills{skills: []string{"Go", "react", "GO", " SQL "}}, Quotas: mapQuota(quotas),
		Config: testCfg, Now: at,
	}).(*recruiterService)
	return h
}

func searchReq(contacts int, keywords ...string) api.RecruiterSearchRequest {
	return api.RecruiterSearchRequest{ResumeID: resumID, Goal: "PFE de 6 mois en data à Tunis", Contract: "pfe",
		Keywords: keywords, Contacts: contacts}
}

func TestRecruiterSearchValidation(t *testing.T) {
	h := newRecruiterHarness(nil)
	for name, req := range map[string]api.RecruiterSearchRequest{
		"no CV":            {Goal: "data"},
		"too many people":  {ResumeID: resumID, Contacts: 11},
		"negative people":  {ResumeID: resumID, Contacts: -1},
		"bad country":      {ResumeID: resumID, Country: "T1"},
		"one-letter place": {ResumeID: resumID, Country: "T"},
	} {
		_, apiErr := h.svc.Search(context.Background(), uid, req)
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusBadRequest, apiErr.Code, name)
	}
	assert.Zero(t, h.finder.calls)
	assert.Zero(t, h.offers.calls)

	h.svc.Skills = fakeSkills{err: core.ErrPeerNotFound}
	_, apiErr := h.svc.Search(context.Background(), uid, searchReq(0))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)
}

func TestRecruiterSearchPipeline(t *testing.T) {
	h := newRecruiterHarness(nil)
	h.disc.out = []core.Company{{Domain: "beta.tn", Name: "Beta", Description: "Beta builds React apps.", Country: "TN"}}
	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(2, "Python"))
	require.Nil(t, apiErr)

	assert.Equal(t, []string{"python", "go", "react", "sql", "data"}, h.offers.got, "keywords, CV skills, then the goal's words")
	assert.Equal(t, 1, h.disc.calls, "one offer company for two people: discovery runs")
	assert.Equal(t, "TN", h.disc.got.Country, "Tunisia by default")
	require.Len(t, out.Items, 2)
	first := out.Items[0]
	assert.Equal(t, "Sarra Gharbi", first.Name, "the recruiter before the CTO")
	assert.Equal(t, "Acme", first.Company)
	assert.Equal(t, "acme.tn", first.Domain)
	assert.True(t, first.Verified)
	assert.Equal(t, "Acme fait de la data et du Python à Tunis.", first.CompanySummary)
	assert.Equal(t, []string{"python", "data"}, first.MatchedKeywords)
	assert.Equal(t, "beta.tn", out.Items[1].Domain, "one person per company first")
	assert.Equal(t, api.ProspectStats{CompaniesDiscovered: 2, ContactsFound: 2}, out.Stats)
	assert.False(t, out.Cached)

	require.Len(t, h.pool.companies, 2, "every company found is in the pool")
	assert.Equal(t, "offer", h.pool.companies[0].Source)
	assert.Equal(t, "acme acme fait de la data et du python a tunis", h.pool.companies[0].SearchText)
	assert.Len(t, h.pool.contacts, 4, "every person found is in the pool")
	assert.Equal(t, "cto@acme.tn", h.pool.contacts[0].Email, "addresses lower case")
	assert.Equal(t, memLookup{count: 2, credits: 1, at: h.now}, h.pool.lookups["acme.tn|hunter"])
	assert.Equal(t, int64(2), h.credited["hunter"], "credits reported to statistics")
	assert.Equal(t, 50, h.finder.got[0].MaxCredits)

	require.Len(t, h.repo.searches, 1)
	assert.Equal(t, "chain", h.repo.searches[0].Provider)
	assert.Contains(t, string(h.repo.searches[0].Query), `"contract":"pfe"`)
	assert.Equal(t, "ct-2", h.repo.people[0].ContactID)

	// The next day another search does not propose the companies the student got people of.
	h.now = h.now.Add(24 * time.Hour)
	h.disc.out = nil
	again, apiErr := h.svc.Search(context.Background(), uid, searchReq(2, "Java"))
	require.Nil(t, apiErr)
	assert.Empty(t, again.Items)
}

func TestRecruiterSearchPoolHitAsksNoProvider(t *testing.T) {
	h := newRecruiterHarness(nil)
	h.offers.err = core.ErrPeerUnavailable
	h.offers.out = nil
	co, _ := h.pool.SaveCompany(context.Background(), &db.Company{Domain: "gamma.tn", Name: "Gamma", Country: "TN",
		Description: "Data et Python.", SearchText: "gamma data et python", Source: "hunter"})
	_, _ = h.pool.SaveContact(context.Background(), &db.Contact{CompanyID: co.ID, Email: "rh@gamma.tn", FirstName: "Service RH",
		Title: "Ressources humaines", Department: "hr", Confidence: 50, Source: "website"})

	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(1, "python"))
	require.Nil(t, apiErr, "job-api down is no offer company, never a failure")
	require.Len(t, out.Items, 1)
	assert.Equal(t, "rh@gamma.tn", out.Items[0].Email)
	assert.Equal(t, []string{"python", "data"}, out.Items[0].MatchedKeywords)
	assert.Zero(t, h.finder.calls, "the pool had someone")
	assert.Zero(t, h.disc.calls, "enough companies")
	assert.Equal(t, api.ProspectStats{CompaniesFromPool: 1, ContactsFromPool: 1}, out.Stats)
}

func TestRecruiterSearchSkipsAskedFindersAndFindersWithoutKey(t *testing.T) {
	h := newRecruiterHarness(nil)
	website := &testFinder{name: "website", ready: true}
	noKey := &testFinder{name: "apollo", ready: false}
	h.svc.Finders = []core.ContactFinder{website, noKey, h.finder}

	h.pool.lookups["acme.tn|website"] = memLookup{at: h.now.Add(-30 * 24 * time.Hour)} // asked, nobody found: negative cache
	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(1, "python"))
	require.Nil(t, apiErr)
	require.Len(t, out.Items, 1)
	assert.Zero(t, website.calls, "asked for this domain within 90 days")
	assert.Zero(t, noKey.calls, "a finder without its key is skipped")
	assert.Equal(t, 1, h.finder.calls)

	// A lookup older than 90 days is asked again.
	h2 := newRecruiterHarness(nil)
	website2 := &testFinder{name: "website", ready: true}
	h2.svc.Finders = []core.ContactFinder{website2, h2.finder}
	h2.pool.lookups["acme.tn|website"] = memLookup{at: h2.now.Add(-91 * 24 * time.Hour)}
	_, apiErr = h2.svc.Search(context.Background(), uid, searchReq(1, "python"))
	require.Nil(t, apiErr)
	assert.Equal(t, 1, website2.calls)
	assert.Zero(t, h2.finder.calls, "the chain stops once the company has someone")
}

func TestRecruiterSearchMonthlyCap(t *testing.T) {
	h := newRecruiterHarness(nil)
	h.pool.lookups["other.tn|hunter"] = memLookup{credits: 50, at: h.now.Add(-time.Hour)}
	h.pool.lookups["old.tn|hunter"] = memLookup{credits: 50, at: h.now.AddDate(0, -1, 0)}
	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(1, "python"))
	require.Nil(t, apiErr)
	assert.Zero(t, h.finder.calls, "50 credits used this month: the cap is reached")
	assert.Empty(t, out.Items)
	assert.Zero(t, h.credited["hunter"])

	h.pool.lookups["other.tn|hunter"] = memLookup{credits: 48, at: h.now.Add(-time.Hour)}
	_, apiErr = h.svc.Search(context.Background(), uid, searchReq(1, "go"))
	require.Nil(t, apiErr)
	require.Equal(t, 1, h.finder.calls)
	assert.Equal(t, 2, h.finder.got[0].MaxCredits, "the finder may spend what is left")
}

func TestRecruiterSearchFinderErrorIsSkipped(t *testing.T) {
	h := newRecruiterHarness(nil)
	h.finder.err = core.ErrPeerUnavailable
	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(1, "python"))
	require.Nil(t, apiErr)
	assert.Empty(t, out.Items)
	_, cached := h.pool.lookups["acme.tn|hunter"]
	assert.False(t, cached, "a failure is not a negative answer")
}

func TestRecruiterSearchAgainTodayIsNotCounted(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 2})
	first, apiErr := h.svc.Search(context.Background(), uid, searchReq(0, "React"))
	require.Nil(t, apiErr)
	require.NotNil(t, first.Allowance.Remaining)
	assert.Equal(t, 1, *first.Allowance.Remaining)

	again, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{ResumeID: resumID,
		Goal: "PFE  de 6 mois en DATA à Tunis", Contract: "pfe", Keywords: []string{" react"}, Country: "tn", Contacts: 5})
	require.Nil(t, apiErr)
	assert.True(t, again.Cached)
	assert.Equal(t, first.SearchID, again.SearchID)
	assert.Equal(t, first.Items, again.Items)
	assert.Equal(t, 1, h.offers.calls, "nothing is searched again")
	assert.Equal(t, 1, again.Allowance.Used, "nor is it counted")

	_, apiErr = h.svc.Search(context.Background(), uid, searchReq(0, "Java"))
	require.Nil(t, apiErr)
	_, apiErr = h.svc.Search(context.Background(), uid, searchReq(0, "Python"))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Contains(t, apiErr.Error.Error(), "daily limit")
	again, apiErr = h.svc.Search(context.Background(), uid, searchReq(0, "React"))
	require.Nil(t, apiErr, "a search already made today still comes back at the limit")
	assert.True(t, again.Cached)
}

func TestRecruiterSearchTunisDay(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 1})
	// 22:30 UTC on May 10 is 23:30 in Tunis: still May 10 there.
	h.now = time.Date(2026, 5, 10, 22, 30, 0, 0, time.UTC)
	out, apiErr := h.svc.Search(context.Background(), uid, searchReq(0, "Go"))
	require.Nil(t, apiErr)
	assert.Equal(t, time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC), out.Allowance.ResetsAt, "midnight in Tunis")

	h.now = time.Date(2026, 5, 10, 22, 59, 0, 0, time.UTC)
	_, apiErr = h.svc.Search(context.Background(), uid, searchReq(0, "Java"))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)

	// 23:00 UTC is midnight in Tunis: a new day.
	h.now = time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC)
	out, apiErr = h.svc.Search(context.Background(), uid, searchReq(0, "Go"))
	require.Nil(t, apiErr)
	assert.False(t, out.Cached, "yesterday's search is not given back")
	assert.Equal(t, 2, h.offers.calls)
}

func TestRecruiterSearchNotInThePlan(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 0})
	_, apiErr := h.svc.Search(context.Background(), uid, searchReq(0, "Go"))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Contains(t, apiErr.Error.Error(), "part of the subscription")
	assert.Zero(t, h.finder.calls)
	assert.Zero(t, h.offers.calls)

	a, apiErr := h.svc.Allowance(context.Background(), uid)
	require.Nil(t, apiErr)
	require.NotNil(t, a.Limit)
	assert.Zero(t, *a.Limit)
	assert.Zero(t, *a.Remaining)

	h = newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: -1})
	a, apiErr = h.svc.Allowance(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Nil(t, a.Limit, "negative: no limit")
}

func TestRecruiterListKeepsEachPersonOnce(t *testing.T) {
	h := newRecruiterHarness(nil)
	_, _ = h.svc.Search(context.Background(), uid, searchReq(1, "Go"))
	h.repo.people = append(h.repo.people, h.repo.people[0]) // the same person found again
	out, apiErr := h.svc.List(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Len(t, out.Items, 1)
	assert.NotNil(t, out.Items[0].MatchedKeywords)
	other, _ := h.svc.List(context.Background(), "someone-else")
	assert.Empty(t, other.Items)
}

func TestSearchTerms(t *testing.T) {
	q := prospectQuery{Keywords: []string{"Machine Learning", "Développement"}, Skills: []string{"python", "développement"},
		Goal: "Je cherche un stage PFE de 6 mois en intelligence artificielle à partir de février"}
	terms := searchTerms(q)
	assert.Equal(t, []string{"machine learning", "developpement", "python", "intelligence", "artificielle"}, terms)
	assert.Equal(t, "machine <-> learning | developpement | python | intelligence | artificielle", tsQuery(terms))
	assert.Equal(t, []string{"python"}, matchedTerms(terms, wordText("Python, Pythonistas")))
	assert.Equal(t, 3, titleBucket("Chargée de recrutement", "201-500"))
	assert.Equal(t, 2, titleBucket("CTO", ""))
	assert.Equal(t, 1, titleBucket("CTO", "1001-5000"))
	assert.Equal(t, 2, titleBucket("Head of Data", "201-500"))
	assert.Equal(t, 1, titleBucket("Comptable", ""))
}
