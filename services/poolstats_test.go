package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePoolStats answers the pool stats queries from its fields.
type fakePoolStats struct {
	counts                  db.PoolCounts
	totals                  db.SearchTotals
	days                    []db.SearchDay
	credits                 []db.ProviderCreditsDay
	err                     error
	freshSince, totalsSince time.Time
	daysSince, creditsSince time.Time
}

func (f *fakePoolStats) PoolCounts(_ context.Context, since time.Time) (*db.PoolCounts, error) {
	f.freshSince = since
	if f.err != nil {
		return nil, f.err
	}
	return &f.counts, nil
}

func (f *fakePoolStats) SearchTotals(_ context.Context, since time.Time) (*db.SearchTotals, error) {
	f.totalsSince = since
	return &f.totals, nil
}

func (f *fakePoolStats) SearchDays(_ context.Context, since time.Time) ([]db.SearchDay, error) {
	f.daysSince = since
	return f.days, nil
}

func (f *fakePoolStats) CreditDays(_ context.Context, since time.Time) ([]db.ProviderCreditsDay, error) {
	f.creditsSince = since
	return f.credits, nil
}

func TestPoolStats(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakePoolStats{
		counts: db.PoolCounts{Companies: 12, Contacts: 30, Verified: 9, HR: 14, Fresh: 25, BySource: map[string]int{"hunter": 20, "fake": 1}},
		totals: db.SearchTotals{Searches: 3, ContactsFromPool: 2, ContactsFound: 1, PoolOnly: 2},
		days: []db.SearchDay{
			{Day: "2026-10-28", SearchTotals: db.SearchTotals{Searches: 1, ContactsFromPool: 1}},
			{Day: "2026-11-27", SearchTotals: db.SearchTotals{Searches: 2, ContactsFromPool: 1, ContactsFound: 1}},
			{Day: "2026-10-01", SearchTotals: db.SearchTotals{Searches: 9}},
		},
		credits: []db.ProviderCreditsDay{{Day: "2026-11-27", Provider: "hunter", Credits: 3}, {Day: "2026-11-26", Provider: "apollo", Credits: 1}},
	}
	svc := NewPoolStatsService(PoolStatsDeps{Repo: repo, CreditProviders: []string{"hunter", "apollo"}, Now: func() time.Time { return now }})

	out, apiErr := svc.Stats(context.Background())
	require.Nil(t, apiErr)
	assert.Equal(t, now.Add(-180*24*time.Hour), repo.freshSince)
	assert.Equal(t, now.Add(-30*24*time.Hour), repo.totalsSince)
	assert.Equal(t, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), repo.daysSince, "UTC day 30 days before today")
	assert.Equal(t, repo.daysSince, repo.creditsSince)

	assert.Equal(t, 12, out.Pool.Companies)
	assert.Equal(t, 25, out.Pool.Fresh)
	assert.Equal(t, map[string]int{"website": 0, "hunter": 20, "apollo": 0, "offer": 0, "import": 0, "fake": 1}, out.Pool.BySource,
		"every source listed")

	assert.Equal(t, 3, out.Last30Days.ContactsShown)
	assert.Equal(t, 2, out.Last30Days.ContactsFromPool)
	assert.Equal(t, 67, out.Last30Days.PoolHitRate, "2/3 rounds to 67")
	assert.Equal(t, 2, out.Last30Days.PoolOnlySearches)
	assert.Equal(t, 67, out.Last30Days.PoolOnlyRate)

	require.Len(t, out.Daily, 31)
	assert.Equal(t, "2026-10-28", out.Daily[0].Day)
	assert.Equal(t, 1, out.Daily[0].Searches)
	assert.Equal(t, map[string]int{"hunter": 0, "apollo": 0}, out.Daily[0].Credits, "zero-filled")
	assert.Equal(t, "2026-11-01", out.Daily[4].Day)
	assert.Zero(t, out.Daily[4].Searches)
	assert.Equal(t, map[string]int{"hunter": 0, "apollo": 1}, out.Daily[29].Credits)
	last := out.Daily[30]
	assert.Equal(t, "2026-11-27", last.Day)
	assert.Equal(t, 2, last.Searches)
	assert.Equal(t, 1, last.ContactsFromPool)
	assert.Equal(t, 1, last.ContactsFound)
	assert.Equal(t, map[string]int{"hunter": 3, "apollo": 0}, last.Credits)
	for i, d := range out.Daily {
		assert.NotEqual(t, 9, d.Searches, "day %d: days before the window are dropped", i)
	}
}

func TestPoolStatsEmpty(t *testing.T) {
	svc := NewPoolStatsService(PoolStatsDeps{Repo: &fakePoolStats{}, CreditProviders: []string{"hunter"}})
	out, apiErr := svc.Stats(context.Background())
	require.Nil(t, apiErr)
	assert.Zero(t, out.Last30Days.PoolHitRate, "no contact shown: no division by zero")
	assert.Zero(t, out.Last30Days.PoolOnlyRate, "no search: no division by zero")
	require.Len(t, out.Daily, 31)
	assert.Equal(t, time.Now().UTC().Format(time.DateOnly), out.Daily[30].Day)
	assert.Equal(t, map[string]int{"hunter": 0}, out.Daily[30].Credits)
	assert.Len(t, out.Pool.BySource, 5)
}

func TestPoolStatsRepositoryError(t *testing.T) {
	svc := NewPoolStatsService(PoolStatsDeps{Repo: &fakePoolStats{err: assert.AnError}})
	_, apiErr := svc.Stats(context.Background())
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
}

func TestPercent(t *testing.T) {
	assert.Equal(t, 0, percent(0, 0))
	assert.Equal(t, 0, percent(5, 0))
	assert.Equal(t, 50, percent(1, 2))
	assert.Equal(t, 33, percent(1, 3))
	assert.Equal(t, 67, percent(2, 3))
	assert.Equal(t, 100, percent(3, 3))
	assert.Equal(t, 1, percent(1, 200), "0.5 rounds up")
}
