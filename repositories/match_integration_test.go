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

func TestMatchRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewMatchRepository(store)
	const user, other = "00000000-0000-4000-8000-0000000000f1", "00000000-0000-4000-8000-0000000000f2"
	const job, cv = "00000000-0000-4000-8000-0000000000f3", "00000000-0000-4000-8000-0000000000f4"
	hash := strings.Repeat("a", 64)

	_, err := repo.Find(ctx, user, job, cv, "fr", hash)
	assert.ErrorIs(t, err, ErrNotFound)

	m, err := repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: hash,
		Score: 68, Level: "good", Strengths: []string{"React en stage"}, Advice: "Mets en avant React."})
	require.NoError(t, err)
	assert.Equal(t, []string{"React en stage"}, m.Strengths)
	assert.Equal(t, []string{}, m.Gaps, "no gaps is an empty list")
	assert.Equal(t, db.MatchKeywords{Matched: []string{}, Missing: []string{}}, m.Keywords, "no keywords is two empty lists")
	assert.Zero(t, m.KeywordCoverage)

	kw := strings.Repeat("c", 64)
	withKw, err := repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: kw, Score: 50, Level: "partial",
		Keywords: db.MatchKeywords{Matched: []string{"React", "C++"}, Missing: []string{"Docker"}}, KeywordCoverage: 67})
	require.NoError(t, err)
	found, err := repo.Find(ctx, user, job, cv, "fr", kw)
	require.NoError(t, err)
	assert.Equal(t, withKw.Keywords, found.Keywords)
	assert.Equal(t, []string{"React", "C++"}, found.Keywords.Matched, "order kept")
	assert.Equal(t, []string{"Docker"}, found.Keywords.Missing)
	assert.Equal(t, 67, found.KeywordCoverage)
	_, err = repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("d", 64), Score: 50, Level: "partial", KeywordCoverage: 101})
	assert.Error(t, err, "the table refuses a coverage above 100")

	again, err := repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: hash, Score: 10, Level: "weak"})
	require.NoError(t, err)
	assert.Equal(t, m.ID, again.ID, "the same key keeps the first reading")
	assert.Equal(t, 68, again.Score)

	got, err := repo.Find(ctx, user, job, cv, "fr", hash)
	require.NoError(t, err)
	assert.Equal(t, m.ID, got.ID)
	_, err = repo.Find(ctx, user, job, cv, "en", hash)
	assert.ErrorIs(t, err, ErrNotFound, "another language is another reading")
	_, err = repo.Find(ctx, other, job, cv, "fr", hash)
	assert.ErrorIs(t, err, ErrNotFound, "another student sees nothing")

	_, err = repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "en", InputHash: hash, Score: 68, Level: "good"})
	require.NoError(t, err)
	n, err := repo.CountSince(ctx, user, db.MatchOriginAnalysis, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	n, err = repo.CountSince(ctx, user, db.MatchOriginAnalysis, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("b", 64), Score: 120, Level: "good"})
	assert.Error(t, err, "the table refuses a score above 100")
}

func TestMatchRepositoryOrigin(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewMatchRepository(store)
	const user, job, cv = "00000000-0000-4000-8000-0000000000e1", "00000000-0000-4000-8000-0000000000e3", "00000000-0000-4000-8000-0000000000e4"
	hour := time.Now().Add(-time.Hour)

	a, err := repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("a", 64), Score: 50, Level: "partial"})
	require.NoError(t, err)
	assert.Equal(t, db.MatchOriginAnalysis, a.Origin, "no origin is an analysis")
	s, err := repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("b", 64),
		Score: 61, Level: "good", Advice: "old", Origin: db.MatchOriginScore})
	require.NoError(t, err)
	assert.Equal(t, db.MatchOriginScore, s.Origin)
	_, err = repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("c", 64), Score: 1, Level: "weak", Origin: "other"})
	assert.Error(t, err, "the table refuses an unknown origin")

	n, err := repo.CountSince(ctx, user, db.MatchOriginScore, hour)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = repo.CountSince(ctx, user, db.MatchOriginAnalysis, hour)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	up, err := repo.Upgrade(ctx, &db.JobMatch{ID: s.ID, UserID: user, Score: 70, Level: "good", Advice: "new", Gaps: []string{"Docker"}})
	require.NoError(t, err)
	assert.Equal(t, s.ID, up.ID)
	assert.Equal(t, db.MatchOriginAnalysis, up.Origin)
	assert.Equal(t, 70, up.Score)
	assert.Equal(t, "new", up.Advice)
	assert.Equal(t, []string{"Docker"}, up.Gaps)
	assert.Equal(t, []string{}, up.Strengths)
	assert.False(t, up.CreatedAt.Before(s.CreatedAt), "counted when upgraded")
	n, err = repo.CountSince(ctx, user, db.MatchOriginAnalysis, hour)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = repo.Upgrade(ctx, &db.JobMatch{ID: s.ID, UserID: user, Score: 1, Level: "weak"})
	assert.ErrorIs(t, err, ErrNotFound, "an analysis is not upgraded again")
	_, err = repo.Upgrade(ctx, &db.JobMatch{ID: a.ID, UserID: "00000000-0000-4000-8000-0000000000e2", Score: 1, Level: "weak"})
	assert.ErrorIs(t, err, ErrNotFound, "another student's row is not touched")
}
