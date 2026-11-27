//go:build integration

package repositories

import (
	"context"
	"strings"
	"testing"
	"time"

	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolStatsRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewPoolStatsRepository(store)
	pool := NewProspectRepository(store)
	searches := NewRecruiterRepository(store)
	const user = "00000000-0000-4000-8000-0000000000e1"
	now := time.Now()
	since := now.Add(-time.Hour)

	// Empty pool and no searches: zeros, not errors.
	counts, err := repo.PoolCounts(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, db.PoolCounts{BySource: map[string]int{}}, *counts)
	totals, err := repo.SearchTotals(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, db.SearchTotals{}, *totals)

	acme, err := pool.SaveCompany(ctx, &db.Company{Domain: "acme.tn", Name: "Acme", SearchText: "acme", Source: "offer"})
	require.NoError(t, err)
	_, err = pool.SaveCompany(ctx, &db.Company{Domain: "beta.tn", Name: "Beta", SearchText: "beta", Source: "hunter"})
	require.NoError(t, err)
	verifiedAt := now
	for _, c := range []db.Contact{
		{Email: "rh@acme.tn", Department: "hr", Source: "website"},
		{Email: "sarra@acme.tn", Department: "hr", Source: "hunter", Verification: "valid", VerifiedAt: &verifiedAt},
		{Email: "cto@acme.tn", Department: "executive", Source: "hunter", Verification: "valid"},
		{Email: "gone@acme.tn", Department: "hr", Source: "apollo", Verification: "valid"},
	} {
		c.CompanyID = acme.ID
		_, err := pool.SaveContact(ctx, &c)
		require.NoError(t, err)
	}
	_, err = store.Conn(ctx).Exec(ctx, `UPDATE contacts SET opted_out = true WHERE email = 'gone@acme.tn'`)
	require.NoError(t, err)
	_, err = store.Conn(ctx).Exec(ctx, `UPDATE contacts SET updated_at = now() - interval '1 year' WHERE email IN ('rh@acme.tn', 'sarra@acme.tn')`)
	require.NoError(t, err)

	counts, err = repo.PoolCounts(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, db.PoolCounts{Companies: 2, Contacts: 3, Verified: 2, HR: 2, Fresh: 2,
		BySource: map[string]int{"website": 1, "hunter": 2}}, *counts, "opted out left out; fresh = verified or updated since")

	save := func(hash string, stats *db.SearchStats) {
		_, _, err := searches.SaveSearch(ctx, &db.RecruiterSearch{UserID: user, Query: []byte(`{}`), QueryHash: strings.Repeat(hash, 64),
			Provider: "chain", Stats: stats}, nil)
		require.NoError(t, err)
	}
	save("a", &db.SearchStats{ContactsFromPool: 2, ContactsFound: 0, CompaniesFromPool: 2})
	save("b", &db.SearchStats{ContactsFromPool: 1, ContactsFound: 2, CompaniesDiscovered: 1})
	save("c", &db.SearchStats{})
	save("d", nil)
	var stored *int
	require.NoError(t, store.Conn(ctx).QueryRow(ctx, `SELECT companies_discovered FROM recruiter_searches WHERE query_hash = $1`,
		strings.Repeat("b", 64)).Scan(&stored))
	require.NotNil(t, stored)
	assert.Equal(t, 1, *stored)
	_, err = store.Conn(ctx).Exec(ctx, `UPDATE recruiter_searches SET created_at = now() - interval '40 days' WHERE query_hash = $1`,
		strings.Repeat("c", 64))
	require.NoError(t, err)

	totals, err = repo.SearchTotals(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, db.SearchTotals{Searches: 2, ContactsFromPool: 3, ContactsFound: 2, PoolOnly: 1}, *totals,
		"NULL stats and older searches left out")
	days, err := repo.SearchDays(ctx, now.AddDate(0, 0, -60))
	require.NoError(t, err)
	require.Len(t, days, 2)
	assert.Equal(t, now.AddDate(0, 0, -40).UTC().Format(time.DateOnly), days[0].Day)
	assert.Equal(t, db.SearchDay{Day: now.UTC().Format(time.DateOnly),
		SearchTotals: db.SearchTotals{Searches: 2, ContactsFromPool: 3, ContactsFound: 2, PoolOnly: 1}}, days[1])

	require.NoError(t, pool.SaveCredits(ctx, "hunter", 1))
	require.NoError(t, pool.SaveCredits(ctx, "hunter", 2))
	require.NoError(t, pool.SaveCredits(ctx, "apollo", 1))
	assert.Error(t, pool.SaveCredits(ctx, "apollo", 0), "credits > 0")
	credits, err := repo.CreditDays(ctx, since)
	require.NoError(t, err)
	today := now.UTC().Format(time.DateOnly)
	assert.Equal(t, []db.ProviderCreditsDay{{Day: today, Provider: "apollo", Credits: 1}, {Day: today, Provider: "hunter", Credits: 3}}, credits)
}
