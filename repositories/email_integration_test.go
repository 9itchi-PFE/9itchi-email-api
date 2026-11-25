//go:build integration

package repositories

import (
	"context"
	"testing"
	"time"

	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The emails that carry the imported file are counted over the life of the account, once they left or are about to.
func TestEmailRepositoryCountOriginal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewEmailRepository(store)
	const user, other = "00000000-0000-0000-0000-0000000000a1", "00000000-0000-0000-0000-0000000000a2"

	create := func(userID, status string, original bool) *db.Email {
		e := &db.Email{UserID: userID, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: status, CVOriginal: original}
		if status == db.EmailScheduled {
			at := time.Now().Add(time.Hour)
			e.ScheduledAt = &at
		}
		e, err := repo.Create(ctx, e)
		require.NoError(t, err)
		assert.Equal(t, original, e.CVOriginal)
		return e
	}
	n, err := repo.CountOriginal(ctx, user)
	require.NoError(t, err)
	assert.Zero(t, n)

	sent := create(user, db.EmailDraft, true)
	_, err = repo.Claim(ctx, sent.ID)
	require.NoError(t, err)
	n, err = repo.CountOriginal(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "an email being sent already counts")
	require.NoError(t, repo.Finish(ctx, sent.ID, db.EmailSent, nil))

	create(user, db.EmailScheduled, true)
	failed := create(user, db.EmailDraft, true)
	require.NoError(t, repo.Finish(ctx, failed.ID, db.EmailFailed, nil))
	cancelled := create(user, db.EmailScheduled, true)
	_, err = repo.Cancel(ctx, user, cancelled.ID)
	require.NoError(t, err)
	create(user, db.EmailScheduled, false) // sent with the CV built in 9itchi, while subscribed
	create(other, db.EmailScheduled, true)

	n, err = repo.CountOriginal(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "sent and scheduled count; failed, cancelled, the built CV and other students do not")

	// The first sent email of a student has no other sent email beside it; a scheduled one is not a sent one.
	another, err := repo.SentAnother(ctx, user, sent.ID)
	require.NoError(t, err)
	assert.False(t, another)
	second := create(user, db.EmailDraft, false)
	_, err = repo.Claim(ctx, second.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Finish(ctx, second.ID, db.EmailSent, nil))
	another, err = repo.SentAnother(ctx, user, second.ID)
	require.NoError(t, err)
	assert.True(t, another)
	another, err = repo.SentAnother(ctx, other, second.ID)
	require.NoError(t, err)
	assert.False(t, another, "another student's emails do not count")

	got, err := repo.Get(ctx, user, sent.ID)
	require.NoError(t, err)
	assert.True(t, got.CVOriginal)
}
