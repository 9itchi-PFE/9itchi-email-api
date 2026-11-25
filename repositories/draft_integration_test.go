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

	first, err := repo.Save(ctx, &db.AIDraft{UserID: user, JobID: job, Language: "fr", Tone: "formal", InputHash: hash, Subject: "Candidature", Body: "Bonjour 1"})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	second, err := repo.Save(ctx, &db.AIDraft{UserID: user, JobID: job, Language: "fr", Tone: "formal", InputHash: hash, Subject: "Candidature", Body: "Bonjour 2"})
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

	_, err = repo.Save(ctx, &db.AIDraft{UserID: user, JobID: job, Language: "de", Tone: "formal", InputHash: hash, Subject: "s", Body: "b"})
	assert.Error(t, err, "the table refuses a language it does not know")
}
