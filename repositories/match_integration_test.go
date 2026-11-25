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
	n, err := repo.CountSince(ctx, user, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = repo.CountSince(ctx, user, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = repo.Save(ctx, &db.JobMatch{UserID: user, JobID: job, ResumeID: cv, Language: "fr", InputHash: strings.Repeat("b", 64), Score: 120, Level: "good"})
	assert.Error(t, err, "the table refuses a score above 100")
}
