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

func TestProspectRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewProspectRepository(store)

	_, err := repo.CompanyByDomain(ctx, "acme.tn")
	assert.ErrorIs(t, err, ErrNotFound)
	acme, err := repo.SaveCompany(ctx, &db.Company{Domain: "acme.tn", Name: "Acme", Description: "Data et Python", Country: "TN",
		SearchText: "acme data et python", Source: "offer"})
	require.NoError(t, err)
	assert.Empty(t, acme.Keywords, "no keywords: an empty list, not NULL")
	_, err = repo.SaveCompany(ctx, &db.Company{Domain: "beta.fr", Name: "Beta", Country: "FR", SearchText: "beta machine learning",
		Keywords: []string{"ml"}, Source: "hunter"})
	require.NoError(t, err)

	again, err := repo.SaveCompany(ctx, &db.Company{Domain: "acme.tn", Name: "Acme SA", Description: "Data et Python", Country: "TN",
		SearchText: "acme sa data et python", Source: "hunter"})
	require.NoError(t, err)
	assert.Equal(t, acme.ID, again.ID, "same domain, same company")
	assert.Equal(t, "offer", again.Source, "the first source stays")
	assert.Equal(t, "Acme SA", again.Name)
	assert.Empty(t, again.LogoURL)
	logo, err := repo.SaveCompany(ctx, &db.Company{Domain: "acme.tn", Name: "Acme SA", Description: "Data et Python", Country: "TN",
		SearchText: "acme sa data et python", Source: "hunter", LogoURL: "https://acme.tn/logo.png"})
	require.NoError(t, err)
	assert.Equal(t, "https://acme.tn/logo.png", logo.LogoURL)
	kept, err := repo.SaveCompany(ctx, &db.Company{Domain: "acme.tn", Name: "Acme SA", Description: "Data et Python", Country: "TN",
		SearchText: "acme sa data et python", Source: "offer"})
	require.NoError(t, err)
	assert.Equal(t, "https://acme.tn/logo.png", kept.LogoURL, "a save without logo keeps the stored one")
	_, err = repo.SaveCompany(ctx, &db.Company{Domain: "long.tn", Name: "Long", SearchText: "long", Source: "offer",
		LogoURL: "https://long.tn/" + strings.Repeat("a", 500)})
	assert.Error(t, err, "logo_url is at most 500 characters")

	found, err := repo.SearchCompanies(ctx, "python | machine <-> learning", "", 10)
	require.NoError(t, err)
	assert.Len(t, found, 2)
	found, err = repo.SearchCompanies(ctx, "python | machine <-> learning", "TN", 10)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "acme.tn", found[0].Domain)
	found, err = repo.SearchCompanies(ctx, "learning <-> machine", "", 10)
	require.NoError(t, err)
	assert.Empty(t, found, "words of a term in order")

	now := time.Now()
	c, err := repo.SaveContact(ctx, &db.Contact{CompanyID: acme.ID, Email: "Sarra@acme.tn", FirstName: "Sarra", Title: "RH",
		Department: "hr", Source: "hunter", Confidence: 80, Verification: "valid", VerifiedAt: &now})
	require.NoError(t, err)
	c2, err := repo.SaveContact(ctx, &db.Contact{CompanyID: acme.ID, Email: "sarra@ACME.tn", LastName: "Gharbi", Department: "other",
		Source: "apollo", Confidence: 90})
	require.NoError(t, err)
	assert.Equal(t, c.ID, c2.ID, "same address, any case: same person")
	assert.Equal(t, "Sarra", c2.FirstName, "what is known stays")
	assert.Equal(t, "Gharbi", c2.LastName)
	assert.Equal(t, "hr", c2.Department)
	assert.Equal(t, "valid", c2.Verification)
	_, err = store.Conn(ctx).Exec(ctx, `UPDATE contacts SET opted_out = true WHERE id = $1`, c.ID)
	require.NoError(t, err)
	c3, err := repo.SaveContact(ctx, &db.Contact{CompanyID: acme.ID, Email: "sarra@acme.tn", Department: "hr", Source: "website"})
	require.NoError(t, err)
	assert.True(t, c3.OptedOut, "a person who opted out stays out")
	_, err = repo.SaveContact(ctx, &db.Contact{CompanyID: acme.ID, Email: "jobs@acme.tn", Department: "hr", Source: "website", Confidence: 50})
	require.NoError(t, err)

	people, err := repo.CompanyContacts(ctx, acme.ID, now.Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, people, 1, "opted out people are not given")
	assert.Equal(t, "jobs@acme.tn", people[0].Email)
	people, err = repo.CompanyContacts(ctx, acme.ID, now.Add(time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, people, "not updated since")

	asked, err := repo.LookupSince(ctx, "acme.tn", "hunter", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.False(t, asked)
	require.NoError(t, repo.SaveLookup(ctx, "acme.tn", "hunter", 0, 0))
	require.NoError(t, repo.SaveLookup(ctx, "beta.fr", "hunter", 3, 1))
	require.NoError(t, repo.SaveLookup(ctx, "beta.fr", "apollo", 2, 2))
	asked, err = repo.LookupSince(ctx, "acme.tn", "hunter", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.True(t, asked, "0 results is a negative cache")
	asked, err = repo.LookupSince(ctx, "acme.tn", "hunter", now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, asked)
	n, err := repo.CreditsSince(ctx, "hunter", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Deleting a company removes its people; a recruiters row keeps its copy without the link.
	rec := NewRecruiterRepository(store)
	_, saved, err := rec.SaveSearch(ctx, &db.RecruiterSearch{UserID: "00000000-0000-4000-8000-0000000000f1", Query: []byte(`{}`),
		QueryHash: strings.Repeat("a", 64), Provider: "chain"}, []db.Recruiter{{DedupeKey: "jobs@acme.tn", Name: "Service carrières",
		Email: "jobs@acme.tn", Domain: "acme.tn", ContactID: people0(t, repo, acme.ID), CompanySummary: "Data et Python",
		MatchedKeywords: []string{"python"}, Verified: true}})
	require.NoError(t, err)
	assert.NotEmpty(t, saved[0].ContactID)
	assert.Equal(t, []string{"python"}, saved[0].MatchedKeywords)
	assert.True(t, saved[0].Verified)
	assert.Equal(t, "https://acme.tn/logo.png", saved[0].LogoURL, "logo of the contact's company")
	bySearch, err := rec.ListBySearch(ctx, "00000000-0000-4000-8000-0000000000f1", saved[0].SearchID)
	require.NoError(t, err)
	require.Len(t, bySearch, 1)
	assert.Equal(t, "https://acme.tn/logo.png", bySearch[0].LogoURL)
	all, err := rec.List(ctx, "00000000-0000-4000-8000-0000000000f1", 10)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "https://acme.tn/logo.png", all[0].LogoURL)
	_, err = store.Conn(ctx).Exec(ctx, `DELETE FROM companies WHERE id = $1`, acme.ID)
	require.NoError(t, err)
	got, err := rec.Get(ctx, "00000000-0000-4000-8000-0000000000f1", saved[0].ID)
	require.NoError(t, err)
	assert.Empty(t, got.ContactID)
	assert.Empty(t, got.LogoURL, "no contact: no logo")
	assert.Equal(t, "Data et Python", got.CompanySummary)
	domains, err := rec.RecentDomains(ctx, "00000000-0000-4000-8000-0000000000f1", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []string{"acme.tn"}, domains)
	search, err := rec.GetSearch(ctx, "00000000-0000-4000-8000-0000000000f1", saved[0].SearchID)
	require.NoError(t, err)
	assert.Equal(t, "chain", search.Provider)
}

func people0(t *testing.T, repo ProspectRepository, companyID string) string {
	t.Helper()
	people, err := repo.CompanyContacts(context.Background(), companyID, time.Now().Add(-time.Hour), 1)
	require.NoError(t, err)
	require.NotEmpty(t, people)
	return people[0].ID
}
