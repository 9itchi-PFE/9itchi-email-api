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

func TestApplicationRepository(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewApplicationRepository(store)
	const user, other, job = "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000009", "00000000-0000-0000-0000-000000000002"

	_, err := repo.GetByJob(ctx, user, job)
	assert.ErrorIs(t, err, ErrNotFound)

	a, err := repo.Upsert(ctx, &db.Application{UserID: user, JobID: job, Status: db.AppSent, JobTitle: "Portail", Company: "Acme", Via: db.ViaSelf})
	require.NoError(t, err)
	assert.Equal(t, db.ViaSelf, a.Via)
	assert.Equal(t, "Portail", a.JobTitle)

	// Applying again to the same offer replaces the row instead of adding one.
	var emailID string
	require.NoError(t, store.Conn(ctx).QueryRow(ctx,
		`INSERT INTO emails (user_id, tracking_id) VALUES ($1, gen_random_uuid()) RETURNING id`, user).Scan(&emailID))
	b, err := repo.Upsert(ctx, &db.Application{UserID: user, JobID: job, EmailID: &emailID, Status: db.AppScheduled, JobTitle: "Portail v2", Company: "Acme", Via: db.ViaGmail})
	require.NoError(t, err)
	assert.Equal(t, a.ID, b.ID)
	assert.Equal(t, db.ViaGmail, b.Via)
	assert.Equal(t, "Portail v2", b.JobTitle)

	byEmail, err := repo.GetByEmail(ctx, emailID)
	require.NoError(t, err)
	assert.Equal(t, b.ID, byEmail.ID)
	assert.Equal(t, "Acme", byEmail.Company)
	_, err = repo.GetByEmail(ctx, "00000000-0000-0000-0000-0000000000ff")
	assert.ErrorIs(t, err, ErrNotFound)

	got, err := repo.GetByJob(ctx, user, job)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
	require.NotNil(t, got.EmailID)

	_, err = repo.Get(ctx, other, a.ID)
	assert.ErrorIs(t, err, ErrNotFound, "another student's application")
	_, err = repo.SetStatus(ctx, other, a.ID, db.AppReplied)
	assert.ErrorIs(t, err, ErrNotFound)

	set, err := repo.SetStatus(ctx, user, a.ID, db.AppReplied)
	require.NoError(t, err)
	assert.Equal(t, db.AppReplied, set.Status)
	_, err = repo.SetStatus(ctx, user, a.ID, "bogus")
	assert.Error(t, err)

	list, err := repo.ListByUser(ctx, user, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Acme", list[0].Company)

	// The follow-up is recorded once; applying again to the offer starts over.
	var followID string
	require.NoError(t, store.Conn(ctx).QueryRow(ctx,
		`INSERT INTO emails (user_id, tracking_id) VALUES ($1, gen_random_uuid()) RETURNING id`, user).Scan(&followID))
	at := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	_, err = repo.SetFollowUp(ctx, other, a.ID, followID, at)
	assert.ErrorIs(t, err, ErrNotFound)
	f, err := repo.SetFollowUp(ctx, user, a.ID, followID, at)
	require.NoError(t, err)
	require.NotNil(t, f.FollowUpEmailID)
	assert.Equal(t, followID, *f.FollowUpEmailID)
	assert.True(t, at.Equal(*f.FollowUpSentAt))
	_, err = repo.SetFollowUp(ctx, user, a.ID, followID, at)
	assert.ErrorIs(t, err, ErrConflict, "one follow-up per application")
	again, err := repo.Upsert(ctx, &db.Application{UserID: user, JobID: job, Status: db.AppSent, JobTitle: "Portail", Company: "Acme", Via: db.ViaSelf})
	require.NoError(t, err)
	assert.Nil(t, again.FollowUpEmailID)
	assert.Nil(t, again.FollowUpSentAt)

	_, err = store.Conn(ctx).Exec(ctx, `INSERT INTO applications (user_id, job_id, via) VALUES (gen_random_uuid(), gen_random_uuid(), 'pigeon')`)
	assert.Error(t, err, "unknown channel")
}
