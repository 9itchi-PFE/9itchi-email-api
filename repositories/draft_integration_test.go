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

func TestDraftRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewDraftRepository(store)
	const user, other, job = "00000000-0000-4000-8000-0000000000d1", "00000000-0000-4000-8000-0000000000d2", "00000000-0000-4000-8000-0000000000d3"
	hash := strings.Repeat("c", 64)

	_, err := repo.Latest(ctx, user, job, hash, "fr", "formal")
	assert.ErrorIs(t, err, ErrNotFound)

	jobID := job
	first, err := repo.Save(ctx, &db.AIDraft{UserID: user, JobID: &jobID, Language: "fr", Tone: "formal", InputHash: hash, Subject: "Candidature", Body: "Bonjour 1"})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	second, err := repo.Save(ctx, &db.AIDraft{UserID: user, JobID: &jobID, Language: "fr", Tone: "formal", InputHash: hash, Subject: "Candidature", Body: "Bonjour 2"})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)

	got, err := repo.Latest(ctx, user, job, hash, "fr", "formal")
	require.NoError(t, err)
	assert.Equal(t, "Bonjour 2", got.Body, "the newest letter")
	_, err = repo.Latest(ctx, user, job, hash, "fr", "friendly")
	assert.ErrorIs(t, err, ErrNotFound, "another tone is another letter")
	_, err = repo.Latest(ctx, other, job, hash, "fr", "formal")
	assert.ErrorIs(t, err, ErrNotFound, "another student sees nothing")

	n, err := repo.CountSince(ctx, user, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = repo.CountSince(ctx, user, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = repo.Save(ctx, &db.AIDraft{UserID: user, JobID: &jobID, Language: "de", Tone: "formal", InputHash: hash, Subject: "s", Body: "b"})
	assert.Error(t, err, "the table refuses a language it does not know")
}

// A letter for an offer found elsewhere is kept under its link; a row with neither an offer nor a link is refused.
func TestDraftRepositoryByURL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewDraftRepository(store)
	const user = "00000000-0000-4000-8000-0000000000d4"
	link, other := "https://jobs.example/1", "recruiter:00000000-0000-4000-8000-0000000000d5"
	h1, h2 := strings.Repeat("a", 64), strings.Repeat("b", 64)

	_, err := repo.LatestAnyByURL(ctx, user, link, "fr", "formal")
	assert.ErrorIs(t, err, ErrNotFound)
	saved, err := repo.Save(ctx, &db.AIDraft{UserID: user, SourceURL: &link, Language: "fr", Tone: "formal", InputHash: h1, Subject: "s", Body: "one"})
	require.NoError(t, err)
	assert.Nil(t, saved.JobID)
	require.NotNil(t, saved.SourceURL)
	assert.Equal(t, link, *saved.SourceURL)
	time.Sleep(10 * time.Millisecond)
	_, err = repo.Save(ctx, &db.AIDraft{UserID: user, SourceURL: &link, Language: "fr", Tone: "formal", InputHash: h2, Subject: "s", Body: "two"})
	require.NoError(t, err)
	_, err = repo.Save(ctx, &db.AIDraft{UserID: user, SourceURL: &other, Language: "fr", Tone: "formal", InputHash: h1, Subject: "s", Body: "recruiter"})
	require.NoError(t, err)

	got, err := repo.LatestByURL(ctx, user, link, h1, "fr", "formal")
	require.NoError(t, err)
	assert.Equal(t, "one", got.Body)
	got, err = repo.LatestAnyByURL(ctx, user, link, "fr", "formal")
	require.NoError(t, err)
	assert.Equal(t, "two", got.Body, "the newest, whatever its input")
	_, err = repo.LatestAnyByURL(ctx, user, link, "en", "formal")
	assert.ErrorIs(t, err, ErrNotFound)
	n, err := repo.CountSince(ctx, user, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, n, "letters for links count like the others")

	_, err = repo.Save(ctx, &db.AIDraft{UserID: user, Language: "fr", Tone: "formal", InputHash: h1, Subject: "s", Body: "b"})
	assert.Error(t, err, "neither an offer nor a link")
}
