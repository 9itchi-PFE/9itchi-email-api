package services

import (
	"context"
	"errors"
	"net/http"
	"sort"
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

type fakeFinder struct {
	calls int
	got   core.RecruiterQuery
	err   error
	out   []core.Recruiter
}

func (f *fakeFinder) Name() string { return "test" }

func (f *fakeFinder) Find(_ context.Context, q core.RecruiterQuery) ([]core.Recruiter, error) {
	f.calls++
	f.got = q
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return []core.Recruiter{
		{Name: "Sarra Gharbi", Title: "Responsable RH", Company: "Acme", Domain: "acme.example", Email: "Sarra.Gharbi@acme.example", Confidence: 80},
		{Name: "Karim Sassi", Title: "Hiring Manager", Company: "Acme", Domain: "acme.example", LinkedInURL: "https://www.linkedin.com/in/karim", Confidence: 140},
		{Name: "  ", Title: "nobody"},
	}, nil
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
	svc    *recruiterService
	repo   *memRecruiters
	finder *fakeFinder
	now    time.Time
}

func newRecruiterHarness(quotas map[string]int) *recruiterHarness {
	h := &recruiterHarness{finder: &fakeFinder{}, now: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
	h.repo = &memRecruiters{at: func() time.Time { return h.now }}
	h.svc = NewRecruiterService(RecruiterDeps{
		Repo: h.repo, Finder: h.finder, Skills: fakeSkills{skills: []string{"Go", "react", "GO", " SQL "}}, Quotas: mapQuota(quotas),
		Config: testCfg, Now: func() time.Time { return h.now },
	}).(*recruiterService)
	return h
}

func TestRecruiterSearchNeedsSomethingToLookFor(t *testing.T) {
	h := newRecruiterHarness(nil)
	for name, req := range map[string]api.RecruiterSearchRequest{
		"nothing":        {},
		"only a place":   {Location: "Tunis"},
		"blank keywords": {Keywords: []string{" ", ""}},
		"bad domain":     {Domain: "not a domain"},
	} {
		_, apiErr := h.svc.Search(context.Background(), uid, req)
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusBadRequest, apiErr.Code, name)
	}
	assert.Zero(t, h.finder.calls)
}

func TestRecruiterSearchMergesTheCVSkills(t *testing.T) {
	h := newRecruiterHarness(nil)
	out, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{
		ResumeID: resumID, Keywords: []string{"Stage", "stage "}, Domain: "https://www.Acme.tn/careers",
	})
	require.Nil(t, apiErr)
	assert.Equal(t, []string{"go", "react", "sql"}, h.finder.got.Skills)
	assert.Equal(t, []string{"stage"}, h.finder.got.Keywords)
	assert.Equal(t, "acme.tn", h.finder.got.Domain, "a domain is its host name")
	assert.Equal(t, 20, h.finder.got.Limit)

	assert.Equal(t, "test", out.Provider)
	assert.False(t, out.Cached)
	require.Len(t, out.Items, 2, "a person without a name is dropped")
	assert.Equal(t, "Karim Sassi", out.Items[0].Name, "most certain first")
	assert.Equal(t, 100, out.Items[0].Confidence, "confidence stays within 0 to 100")
	assert.Equal(t, "https://www.linkedin.com/in/karim", h.repo.people[0].DedupeKey)
	assert.Equal(t, "sarra.gharbi@acme.example", h.repo.people[1].DedupeKey)
	assert.Nil(t, out.Allowance.Limit, "no key: no limit")
	assert.Nil(t, out.Allowance.Remaining)
	assert.Equal(t, 1, out.Allowance.Used)

	h2 := newRecruiterHarness(nil)
	h2.svc.Skills = fakeSkills{err: core.ErrPeerNotFound}
	_, apiErr = h2.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{ResumeID: resumID})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)
}

func TestRecruiterSearchAgainTodayIsNotCounted(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 2})
	req := api.RecruiterSearchRequest{Keywords: []string{"React"}, Company: "Acme"}
	first, apiErr := h.svc.Search(context.Background(), uid, req)
	require.Nil(t, apiErr)
	require.NotNil(t, first.Allowance.Remaining)
	assert.Equal(t, 1, *first.Allowance.Remaining)

	again, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{" react"}, Company: "ACME"})
	require.Nil(t, apiErr)
	assert.True(t, again.Cached)
	assert.Equal(t, first.SearchID, again.SearchID)
	assert.Equal(t, first.Items, again.Items)
	assert.Equal(t, 1, h.finder.calls, "the provider is not asked again")
	assert.Equal(t, 1, again.Allowance.Used, "nor is it counted")

	_, apiErr = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Java"}})
	require.Nil(t, apiErr)
	_, apiErr = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Python"}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Contains(t, apiErr.Error.Error(), "daily limit")
	again, apiErr = h.svc.Search(context.Background(), uid, req)
	require.Nil(t, apiErr, "a search already made today still comes back at the limit")
	assert.True(t, again.Cached)
}

func TestRecruiterSearchTunisDay(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 1})
	// 22:30 UTC on May 10 is 23:30 in Tunis: still May 10 there.
	h.now = time.Date(2026, 5, 10, 22, 30, 0, 0, time.UTC)
	out, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Go"}})
	require.Nil(t, apiErr)
	assert.Equal(t, time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC), out.Allowance.ResetsAt, "midnight in Tunis")

	h.now = time.Date(2026, 5, 10, 22, 59, 0, 0, time.UTC)
	_, apiErr = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Java"}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)

	// 23:00 UTC is midnight in Tunis: a new day.
	h.now = time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC)
	out, apiErr = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Go"}})
	require.Nil(t, apiErr)
	assert.False(t, out.Cached, "yesterday's search is not given back")
	assert.Equal(t, 2, h.finder.calls)
}

func TestRecruiterSearchNotInThePlan(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 0})
	_, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Go"}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Contains(t, apiErr.Error.Error(), "part of the subscription")
	assert.Zero(t, h.finder.calls)

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

func TestRecruiterProviderDown(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 6})
	h.finder.err = core.ErrPeerUnavailable
	_, apiErr := h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Go"}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Equal(t, "recruiter search is unavailable", apiErr.Error.Error())
	n, _ := h.repo.CountSearchesSince(context.Background(), uid, time.Time{})
	assert.Zero(t, n, "a failed search is not counted")
}

func TestRecruiterListKeepsEachPersonOnce(t *testing.T) {
	h := newRecruiterHarness(nil)
	_, _ = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Go"}})
	_, _ = h.svc.Search(context.Background(), uid, api.RecruiterSearchRequest{Keywords: []string{"Java"}})
	out, apiErr := h.svc.List(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Len(t, out.Items, 2)
	other, _ := h.svc.List(context.Background(), "someone-else")
	assert.Empty(t, other.Items)
}
