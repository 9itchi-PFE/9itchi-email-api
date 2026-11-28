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

func TestRecruiterRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewRecruiterRepository(store)
	const user, other = "00000000-0000-4000-8000-0000000000f1", "00000000-0000-4000-8000-0000000000f2"
	hash := strings.Repeat("f", 64)
	since := time.Now().Add(-time.Hour)

	_, err := repo.LatestSearch(ctx, user, hash, since)
	assert.ErrorIs(t, err, ErrNotFound)

	s, found, err := repo.SaveSearch(ctx, &db.RecruiterSearch{UserID: user, Query: []byte(`{"keywords":["go"]}`), QueryHash: hash, Provider: "fake"}, []db.Recruiter{
		{DedupeKey: "a@x.example", Name: "Amine Sassi", Title: "Responsable RH", Company: "X", Domain: "x.example", Email: "a@x.example", Confidence: 70},
		{DedupeKey: "https://www.linkedin.com/in/b", Name: "Bilel Ayari", LinkedInURL: "https://www.linkedin.com/in/b", Confidence: 90},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, s.ResultCount)
	assert.JSONEq(t, `{"keywords":["go"]}`, string(s.Query))
	require.Len(t, found, 2)
	assert.Equal(t, s.ID, found[0].SearchID)
	assert.Equal(t, "fake", found[0].Provider)

	got, err := repo.LatestSearch(ctx, user, hash, since)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	_, err = repo.LatestSearch(ctx, user, hash, time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, ErrNotFound, "not since that time")
	_, err = repo.LatestSearch(ctx, other, hash, since)
	assert.ErrorIs(t, err, ErrNotFound)

	list, err := repo.ListBySearch(ctx, user, s.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "Bilel Ayari", list[0].Name, "most certain first")
	empty, err := repo.ListBySearch(ctx, other, s.ID)
	require.NoError(t, err)
	assert.Empty(t, empty)

	one, err := repo.Get(ctx, user, found[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "Amine Sassi", one.Name)
	_, err = repo.Get(ctx, other, found[0].ID)
	assert.ErrorIs(t, err, ErrNotFound, "another student's recruiter")

	time.Sleep(10 * time.Millisecond)
	_, _, err = repo.SaveSearch(ctx, &db.RecruiterSearch{UserID: user, Query: []byte(`{}`), QueryHash: strings.Repeat("e", 64), Provider: "fake", Credits: 2}, []db.Recruiter{
		{DedupeKey: "a@x.example", Name: "Amine Sassi", Title: "HR Manager", Email: "a@x.example", Confidence: 75},
	})
	require.NoError(t, err)
	n, err := repo.CountSearchesSince(ctx, user, since)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "credits summed: 1 (not set) + 2")

	all, err := repo.List(ctx, user, 200)
	require.NoError(t, err)
	require.Len(t, all, 2, "each person once")
	assert.Equal(t, "HR Manager", all[0].Title, "as last found, newest first")
	assert.Equal(t, "Bilel Ayari", all[1].Name)

	_, _, err = repo.SaveSearch(ctx, &db.RecruiterSearch{UserID: user, Query: []byte(`{}`), QueryHash: hash, Provider: "fake"}, []db.Recruiter{
		{DedupeKey: "z", Name: "Z", Confidence: 101},
	})
	assert.Error(t, err, "confidence above 100 is refused")
	n, _ = repo.CountSearchesSince(ctx, user, since)
	assert.Equal(t, 3, n, "and the search with it: one transaction")
}
