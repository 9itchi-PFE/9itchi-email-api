package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

// scheduled is one email scheduled for tomorrow 9:00, with the CV attached.
func scheduled(t *testing.T, h *harness, req api.SendEmailRequest) *db.Email {
	t.Helper()
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: req, ScheduledAt: tomorrow})
	require.Nil(t, apiErr)
	return e
}

func TestUpdateEmailText(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())

	out, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{Subject: ptr(" Nouvel objet "), Body: ptr("Nouveau texte")})
	require.Nil(t, apiErr)
	assert.Equal(t, "Nouvel objet", out.Subject)
	assert.Equal(t, "Nouveau texte", out.Body)
	assert.Equal(t, e.Recipient, out.Recipient)
	assert.Equal(t, e.ScheduledAt, out.ScheduledAt)
	assert.Equal(t, []string{e.ID}, h.sched.scheduled, "the time did not change: no new task")

	for name, req := range map[string]api.UpdateEmailRequest{
		"empty subject": {Subject: ptr("  ")},
		"empty body":    {Body: ptr("")},
		"too soon":      {ScheduledAt: ptr(time.Date(2026, 5, 10, 12, 0, 30, 0, time.UTC))},
		"too late":      {ScheduledAt: ptr(time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC))},
	} {
		_, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, req)
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusBadRequest, apiErr.Code, name)
	}
}

func TestUpdateEmailOnlyWhileScheduled(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())
	_, apiErr := h.svc.UpdateEmail(context.Background(), "55555555-5555-5555-5555-555555555555", e.ID, api.UpdateEmailRequest{Body: ptr("x")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)
	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, "nope", api.UpdateEmailRequest{Body: ptr("x")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)

	_, _ = h.svc.CancelEmail(context.Background(), uid, e.ID)
	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{Body: ptr("x")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code)
	assert.Equal(t, "only a scheduled email can be edited", apiErr.Error.Error())
}

func TestUpdateEmailRecipient(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())
	_, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{To: ptr("other@acme.tn")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code, "the offer takes applications at its own address")
	out, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{To: ptr("HR@acme.tn")})
	require.Nil(t, apiErr, "the same address, however written, is no change")
	assert.Equal(t, "hr@acme.tn", out.Recipient)

	svc := h.svc.(*emailService)
	svc.Jobs = fakeJobs{info: &core.JobInfo{Title: "Stage", Open: true}}
	out, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{To: ptr("other@acme.tn")})
	require.Nil(t, apiErr)
	assert.Equal(t, "other@acme.tn", out.Recipient)

	svc.Jobs = fakeJobs{err: core.ErrPeerUnavailable}
	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{To: ptr("third@acme.tn")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)

	noJob := sendReq()
	noJob.JobID = ""
	free := scheduled(t, h, noJob)
	out, apiErr = h.svc.UpdateEmail(context.Background(), uid, free.ID, api.UpdateEmailRequest{To: ptr("third@acme.tn")})
	require.Nil(t, apiErr, "an email without an offer goes where the student says")
	assert.Equal(t, "third@acme.tn", out.Recipient)
}

func TestUpdateEmailCV(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())
	out, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{AttachCV: ptr(false)})
	require.Nil(t, apiErr)
	assert.Nil(t, out.ResumeID)

	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{AttachCV: ptr(true)})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code, "no CV to attach")

	other := "66666666-6666-6666-6666-666666666666"
	out, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{AttachCV: ptr(true), ResumeID: &other})
	require.Nil(t, apiErr)
	require.NotNil(t, out.ResumeID)
	assert.Equal(t, other, *out.ResumeID)

	// Without a subscription the email carries the imported file: a CV without one is refused.
	h = newHarnessWith(nil, noPlan, imported())
	e = scheduled(t, h, sendReq())
	require.True(t, e.CVOriginal)
	h.svc.(*emailService).Resumes = fakeResumes{pdf: []byte("%PDF")}
	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{ResumeID: &other})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code)
	assert.Equal(t, resumID, *h.repo.rows[e.ID].ResumeID, "nothing changed")
}

func TestUpdateEmailTime(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())
	require.NotNil(t, e.ScheduleTaskID)
	later := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)

	h.sched.err = core.ErrPeerUnavailable
	_, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{ScheduledAt: &later, Body: ptr("x")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Equal(t, tomorrow, *h.repo.rows[e.ID].ScheduledAt, "no new task: nothing changed")
	assert.Equal(t, sendReq().Body, h.repo.rows[e.ID].Body)
	assert.Empty(t, h.sched.cancelled)

	h.sched.err = nil
	out, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{ScheduledAt: &later})
	require.Nil(t, apiErr)
	assert.Equal(t, later, *out.ScheduledAt)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled, "the old task is dropped")
	assert.Len(t, h.sched.scheduled, 2)

	// The old task runs anyway at the old time: the email is not due, nothing leaves.
	got, apiErr := dispatchAt(h.svc, tomorrow, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailScheduled, got.Status)
	assert.Empty(t, h.mailer.sent)
	got, apiErr = dispatchAt(h.svc, later, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, got.Status)
	assert.Len(t, h.mailer.sent, 1)
}

// The email stops being scheduled between the read and the write: the new task is dropped, 409.
func TestUpdateEmailRaceCancelsTheNewTask(t *testing.T) {
	h := newHarness(nil, unlimited)
	e := scheduled(t, h, sendReq())
	svc := h.svc.(*emailService)
	svc.Repo = racingEmails{h.repo}
	later := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	_, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{ScheduledAt: &later})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled, "the new task, not the email's own")
}

// racingEmails is a repository where the email was sent just before the edit is written.
type racingEmails struct{ *memEmails }

func (r racingEmails) UpdateScheduled(ctx context.Context, userID, id string, edit db.EmailEdit) (*db.Email, error) {
	r.rows[id].Status = db.EmailSent
	return r.memEmails.UpdateScheduled(ctx, userID, id, edit)
}

func TestSendWithoutCVHasNoAttachment(t *testing.T) {
	h := newHarness(nil, unlimited)
	req := sendReq()
	req.ResumeID = ""
	_, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	require.Len(t, h.mailer.sent, 1)
	assert.Len(t, h.mailer.sent[0].Attachments, 1, "the letter only")
}
