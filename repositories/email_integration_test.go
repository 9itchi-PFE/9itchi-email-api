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

func TestEmailRepositoryLastBuiltSend(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewEmailRepository(store)
	const user, other = "00000000-0000-0000-0000-0000000000b1", "00000000-0000-0000-0000-0000000000b2"
	send := func(userID string, original bool, at *time.Time) {
		e, err := repo.Create(ctx, &db.Email{UserID: userID, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailDraft, CVOriginal: original})
		require.NoError(t, err)
		if at != nil {
			require.NoError(t, repo.Finish(ctx, e.ID, db.EmailSent, at))
		}
	}
	n, last, err := repo.LastBuiltSend(ctx, user)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Nil(t, last)

	first, latest := time.Now().Add(-48*time.Hour).Truncate(time.Second), time.Now().Add(-time.Hour).Truncate(time.Second)
	send(user, false, &latest)
	send(user, false, &first)
	send(user, true, &latest) // the imported file
	send(user, false, nil)    // a draft
	send(other, false, &latest)

	n, last, err = repo.LastBuiltSend(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only what left with the CV built in 9itchi")
	require.NotNil(t, last)
	assert.True(t, last.Equal(latest))

	// A sent row without the time it left still says when: the last change of the row.
	e, err := repo.Create(ctx, &db.Email{UserID: other, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailDraft})
	require.NoError(t, err)
	_, err = store.Conn(ctx).Exec(ctx, `UPDATE emails SET sent_at = NULL WHERE user_id = $1`, other)
	require.NoError(t, err)
	require.NoError(t, repo.Finish(ctx, e.ID, db.EmailSent, nil))
	n, last, err = repo.LastBuiltSend(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.NotNil(t, last)
	assert.WithinDuration(t, time.Now(), *last, time.Minute)
}

// Only an email of the user that is still scheduled is edited.
func TestEmailRepositoryUpdateScheduled(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewEmailRepository(store)
	const user, other = "00000000-0000-0000-0000-0000000000b1", "00000000-0000-0000-0000-0000000000b2"
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	resume, task := "00000000-0000-0000-0000-0000000000b3", "task-2"
	e, err := repo.Create(ctx, &db.Email{UserID: user, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailScheduled, ScheduledAt: &at, ResumeID: &resume})
	require.NoError(t, err)

	later := at.Add(24 * time.Hour)
	edit := db.EmailEdit{Recipient: "rh@acme.tn", Subject: "s2", Body: "b2", ScheduledAt: &later, ScheduleTaskID: &task}
	_, err = repo.UpdateScheduled(ctx, other, e.ID, edit)
	assert.ErrorIs(t, err, ErrNotFound, "another student's email")

	out, err := repo.UpdateScheduled(ctx, user, e.ID, edit)
	require.NoError(t, err)
	assert.Equal(t, "rh@acme.tn", out.Recipient)
	assert.Equal(t, "s2", out.Subject)
	assert.Equal(t, "b2", out.Body)
	assert.Nil(t, out.ResumeID, "sent without a CV")
	assert.True(t, later.Equal(*out.ScheduledAt))
	require.NotNil(t, out.ScheduleTaskID)
	assert.Equal(t, task, *out.ScheduleTaskID)

	_, err = repo.Cancel(ctx, user, e.ID)
	require.NoError(t, err)
	_, err = repo.UpdateScheduled(ctx, user, e.ID, edit)
	assert.ErrorIs(t, err, ErrNotFound, "no longer scheduled")
}

// The cover letter fields are stored, edited and read back; a row made without them sends no letter.
func TestEmailRepositoryLetterFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewEmailRepository(store)
	const user = "00000000-0000-0000-0000-0000000000b1"
	name, lang := "رسالة_تحفيزية_Ines", "ar"
	at := time.Now().Add(time.Hour)

	e, err := repo.Create(ctx, &db.Email{UserID: user, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailScheduled,
		ScheduledAt: &at, LetterFilename: &name, AttachLetter: true, LetterLanguage: &lang})
	require.NoError(t, err)
	require.NotNil(t, e.LetterFilename)
	assert.Equal(t, name, *e.LetterFilename)
	assert.True(t, e.AttachLetter)
	assert.Equal(t, "ar", *e.LetterLanguage)

	en := "en"
	out, err := repo.UpdateScheduled(ctx, user, e.ID, db.EmailEdit{Recipient: "hr@acme.tn", Subject: "s", Body: "b", ScheduledAt: &at,
		LetterFilename: nil, AttachLetter: false, LetterLanguage: &en})
	require.NoError(t, err)
	assert.Nil(t, out.LetterFilename)
	assert.False(t, out.AttachLetter)
	got, err := repo.Get(ctx, user, e.ID)
	require.NoError(t, err)
	assert.Equal(t, "en", *got.LetterLanguage)

	old, err := repo.Create(ctx, &db.Email{UserID: user, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailDraft})
	require.NoError(t, err)
	assert.False(t, old.AttachLetter)
	assert.Nil(t, old.LetterFilename)
	assert.Nil(t, old.LetterLanguage)

	bad := "de"
	_, err = repo.Create(ctx, &db.Email{UserID: user, Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailDraft, LetterLanguage: &bad})
	assert.Error(t, err, "language checked")
}
