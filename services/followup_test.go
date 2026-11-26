package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sent is the day the harness sends; a follow-up is possible a week later.
var sentDay = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

func appsAt(h *appHarness, now time.Time) {
	h.svc.(*applicationService).now = func() time.Time { return now }
}

func TestFollowUpOfAGmailApplication(t *testing.T) {
	ctx := context.Background()
	h := newApps(fakeJobs{})
	app, apiErr := h.svc.Apply(ctx, uid, gmailReq())
	require.Nil(t, apiErr)
	require.Len(t, h.mailer.sent, 1)

	appsAt(h, sentDay.Add(6*24*time.Hour))
	_, apiErr = h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code, "not before a week")

	appsAt(h, sentDay.Add(8*24*time.Hour))
	d, apiErr := h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{Language: "fr"})
	require.Nil(t, apiErr)
	assert.Equal(t, "Re: Candidature", d.Subject, "it answers the first email")
	assert.Equal(t, "follow_up", h.draft.in.Kind)
	assert.Equal(t, 8, h.draft.in.DaysSince)
	assert.Contains(t, h.draft.in.Previous, "Subject: Candidature")
	assert.Equal(t, "Acme", h.draft.in.Company)
	assert.Empty(t, h.draft.in.CandidateSummary, "the CV is not read again for a follow-up")
	calls := h.draft.calls
	_, apiErr = h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{Language: "fr"})
	require.Nil(t, apiErr)
	assert.Equal(t, calls, h.draft.calls, "asked again, the same letter comes back")

	out, apiErr := h.svc.FollowUp(ctx, uid, app.ID, api.FollowUpRequest{Subject: d.Subject, Body: "Je me permets de revenir vers vous."})
	require.Nil(t, apiErr)
	require.NotNil(t, out.FollowUpSentAt)
	require.Len(t, h.mailer.sent, 2)
	m := h.mailer.sent[1]
	assert.Equal(t, "rh@acme.tn", m.To, "the recipient of the first email")
	assert.Equal(t, "Re: Candidature", m.Subject)
	assert.Nil(t, m.Attachment, "never a CV")

	_, apiErr = h.svc.FollowUp(ctx, uid, app.ID, api.FollowUpRequest{Subject: "Re: x", Body: "y"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code, "one follow-up per application")
	_, apiErr = h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Code)

	list, apiErr := h.svc.ListApplications(ctx, uid)
	require.Nil(t, apiErr)
	require.NotNil(t, list.Items[0].FollowUpSentAt)
}

func TestFollowUpIsRefused(t *testing.T) {
	ctx := context.Background()
	week := sentDay.Add(8 * 24 * time.Hour)

	t.Run("an application that got an answer", func(t *testing.T) {
		h := newApps(fakeJobs{})
		app, _ := h.svc.Apply(ctx, uid, gmailReq())
		_, apiErr := h.svc.UpdateApplication(ctx, uid, app.ID, api.UpdateApplicationRequest{Status: db.AppReplied})
		require.Nil(t, apiErr)
		appsAt(h, week)
		_, apiErr = h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{})
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusConflict, apiErr.Code)
	})

	t.Run("an application sent by the student: a draft, but no send", func(t *testing.T) {
		h := newApps(fakeJobs{})
		app, apiErr := h.svc.Apply(ctx, uid, api.ApplyRequest{JobID: jobID, Via: db.ViaSelf})
		require.Nil(t, apiErr)
		h.apps.rows[0].CreatedAt = sentDay
		appsAt(h, week)
		d, apiErr := h.svc.DraftFollowUp(ctx, uid, app.ID, api.FollowUpDraftRequest{})
		require.Nil(t, apiErr)
		assert.Equal(t, "Candidature", d.Subject, "no first email: the subject of the AI")
		assert.Empty(t, h.draft.in.Previous)
		_, apiErr = h.svc.FollowUp(ctx, uid, app.ID, api.FollowUpRequest{Subject: "s", Body: "b"})
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code)
	})

	t.Run("without a subscription the follow-up is not sent", func(t *testing.T) {
		h := newApps(fakeJobs{})
		app, apiErr := h.svc.Apply(ctx, uid, gmailReq())
		require.Nil(t, apiErr)
		// The same emails, read with a plan that has no subscription.
		free := NewEmailService(EmailDeps{
			Repo: h.repo, Gmail: fakeGmail{}, Mailer: h.mailer, Drafter: h.draft, Jobs: fakeJobs{}, Resumes: fakeResumes{pdf: []byte("%PDF")},
			Tracker: fakeTracker{}, Scheduler: h.sched, Quotas: fakeQuota{limit: 0, ok: true, free: 3}, Events: h.events, Drafts: &memDrafts{},
			Config: testCfg, Now: func() time.Time { return week },
		})
		svc := NewApplicationService(h.apps, free, fakeJobs{}).(*applicationService)
		svc.now = func() time.Time { return week }
		_, apiErr = svc.FollowUp(ctx, uid, app.ID, api.FollowUpRequest{Subject: "Re: Candidature", Body: "b"})
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
		assert.Len(t, h.mailer.sent, 1, "nothing more left")
	})

	t.Run("an application of another student", func(t *testing.T) {
		h := newApps(fakeJobs{})
		app, _ := h.svc.Apply(ctx, uid, gmailReq())
		appsAt(h, week)
		_, apiErr := h.svc.FollowUp(ctx, "99999999-9999-9999-9999-999999999999", app.ID, api.FollowUpRequest{Subject: "s", Body: "b"})
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.Code)
	})
}
