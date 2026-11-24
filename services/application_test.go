package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const appID = "44444444-4444-4444-4444-444444444444"

// memApps keeps one application per (user, job), like the table.
type memApps struct {
	rows []db.Application
	err  error
}

func (m *memApps) Upsert(_ context.Context, a *db.Application) (*db.Application, error) {
	if m.err != nil {
		return nil, m.err
	}
	c := *a
	c.ID = appID
	for i := range m.rows {
		if m.rows[i].UserID == a.UserID && m.rows[i].JobID == a.JobID {
			m.rows[i] = c
			return &c, nil
		}
	}
	m.rows = append(m.rows, c)
	return &c, nil
}
func (m *memApps) ListByUser(_ context.Context, userID string, _ int) ([]db.Application, error) {
	out := []db.Application{}
	for _, a := range m.rows {
		if a.UserID == userID {
			out = append(out, a)
		}
	}
	return out, m.err
}
func (m *memApps) GetByJob(_ context.Context, userID, job string) (*db.Application, error) {
	for _, a := range m.rows {
		if a.UserID == userID && a.JobID == job {
			c := a
			return &c, nil
		}
	}
	return nil, repositories.ErrNotFound
}
func (m *memApps) Get(_ context.Context, userID, id string) (*db.Application, error) {
	for _, a := range m.rows {
		if a.UserID == userID && a.ID == id {
			c := a
			return &c, nil
		}
	}
	return nil, repositories.ErrNotFound
}
func (m *memApps) SetStatus(_ context.Context, userID, id, status string) (*db.Application, error) {
	for i := range m.rows {
		if m.rows[i].UserID == userID && m.rows[i].ID == id {
			m.rows[i].Status = status
			c := m.rows[i]
			return &c, nil
		}
	}
	return nil, repositories.ErrNotFound
}

type appHarness struct {
	*harness
	apps *memApps
	svc  ApplicationService
}

func newApps(jobs fakeJobs) *appHarness {
	h := newHarness(nil, unlimited)
	apps := &memApps{}
	return &appHarness{harness: h, apps: apps, svc: NewApplicationService(apps, h.svc, jobs)}
}

func gmailReq() api.ApplyRequest {
	return api.ApplyRequest{JobID: jobID, ResumeID: resumID, Subject: "Candidature", Body: "Bonjour"}
}

var tomorrow = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

func TestApplySendsToTheOfferAddress(t *testing.T) {
	h := newApps(fakeJobs{})
	req := gmailReq()
	req.To = "someone@else.tn"
	res, apiErr := h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppSent, res.Status)
	assert.Equal(t, db.ViaGmail, res.Via)
	assert.Equal(t, "Stage React", res.JobTitle)
	assert.Equal(t, "Acme", res.Company)
	require.Len(t, h.mailer.sent, 1)
	assert.Equal(t, "rh@acme.tn", h.mailer.sent[0].To, "the address of the offer wins over the one in the request")
	assert.Equal(t, db.EmailSent, res.Email.Status)
}

func TestApplyUsesTheGivenAddressWhenTheOfferHasNone(t *testing.T) {
	h := newApps(fakeJobs{info: &core.JobInfo{Title: "T", Open: true}})
	_, apiErr := h.svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 422, apiErr.Code)

	req := gmailReq()
	req.To = "rh@autre.tn"
	_, apiErr = h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "rh@autre.tn", h.mailer.sent[0].To)
}

func TestApplyRefusals(t *testing.T) {
	_, apiErr := newApps(fakeJobs{err: core.ErrPeerNotFound}).svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)

	_, apiErr = newApps(fakeJobs{err: core.ErrPeerUnavailable}).svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)

	_, apiErr = newApps(fakeJobs{info: &core.JobInfo{ApplyEmail: "rh@acme.tn"}}).svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code, "closed offer")

	h := newApps(fakeJobs{})
	_, apiErr = h.svc.Apply(context.Background(), uid, api.ApplyRequest{JobID: jobID, Subject: " ", Body: "b"})
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Code)
	assert.Empty(t, h.mailer.sent)

	h = newApps(fakeJobs{})
	h.mailer.err = errors.New("smtp down")
	_, apiErr = h.svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)
	assert.Empty(t, h.apps.rows, "a failed send leaves no application")

	h = newApps(fakeJobs{})
	h.apps.err = errors.New("boom")
	_, apiErr = h.svc.Apply(context.Background(), uid, api.ApplyRequest{JobID: jobID, Via: db.ViaSelf})
	require.NotNil(t, apiErr)
	assert.Equal(t, 500, apiErr.Code)
}

func TestApplySelfRecordsWithoutSending(t *testing.T) {
	h := newApps(fakeJobs{info: &core.JobInfo{Title: "Vision", Company: "Medina Labs", Open: true}})
	res, apiErr := h.svc.Apply(context.Background(), uid, api.ApplyRequest{JobID: jobID, Via: db.ViaSelf, ScheduledAt: &tomorrow})
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppSent, res.Status)
	assert.Equal(t, db.ViaSelf, res.Via)
	assert.Nil(t, res.Email)
	assert.Nil(t, res.EmailID)
	assert.Equal(t, "Medina Labs", res.Company)
	assert.Empty(t, h.mailer.sent)
	assert.Empty(t, h.repo.rows)
}

func TestApplyOncePerOffer(t *testing.T) {
	h := newApps(fakeJobs{})
	_, apiErr := h.svc.Apply(context.Background(), uid, gmailReq())
	require.Nil(t, apiErr)
	_, apiErr = h.svc.Apply(context.Background(), uid, gmailReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	_, apiErr = h.svc.Apply(context.Background(), uid, api.ApplyRequest{JobID: jobID, Via: db.ViaSelf})
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	assert.Len(t, h.mailer.sent, 1)

	// Another student is not concerned.
	_, apiErr = h.svc.Apply(context.Background(), "55555555-5555-5555-5555-555555555555", gmailReq())
	assert.Nil(t, apiErr)

	// Once withdrawn, the student may apply again.
	_, apiErr = h.svc.UpdateApplication(context.Background(), uid, appID, api.UpdateApplicationRequest{Status: db.AppWithdrawn})
	require.Nil(t, apiErr)
	_, apiErr = h.svc.Apply(context.Background(), uid, gmailReq())
	assert.Nil(t, apiErr)
}

func TestScheduledApplication(t *testing.T) {
	h := newApps(fakeJobs{})
	req := gmailReq()
	req.ScheduledAt = &tomorrow
	res, apiErr := h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppScheduled, res.Status)
	assert.Empty(t, h.mailer.sent)

	_, apiErr = h.svc.Apply(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code, "already scheduled")

	_, apiErr = h.svc.UpdateApplication(context.Background(), uid, appID, api.UpdateApplicationRequest{Status: db.AppReplied})
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code, "nothing was sent yet")

	// The dispatcher sends it: the list follows the email.
	_, apiErr = h.harness.svc.DispatchScheduled(context.Background(), res.Email.ID)
	require.Nil(t, apiErr)
	list, apiErr := h.svc.ListApplications(context.Background(), uid)
	require.Nil(t, apiErr)
	require.Len(t, list.Items, 1)
	assert.Equal(t, db.AppSent, list.Items[0].Status)
	assert.Equal(t, db.EmailSent, list.Items[0].Email.Status)
}

func TestScheduledApplicationSendNowAndCancel(t *testing.T) {
	h := newApps(fakeJobs{})
	req := gmailReq()
	req.ScheduledAt = &tomorrow
	_, apiErr := h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)

	res, apiErr := h.svc.SendNow(context.Background(), uid, appID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppSent, res.Status)
	assert.Len(t, h.mailer.sent, 1)
	assert.Equal(t, db.AppSent, h.apps.rows[0].Status)

	_, apiErr = h.svc.SendNow(context.Background(), uid, appID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	_, apiErr = h.svc.SendNow(context.Background(), uid, "nope")
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Code)
	_, apiErr = h.svc.SendNow(context.Background(), "55555555-5555-5555-5555-555555555555", appID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)

	// Giving up a scheduled application cancels its email, and the student can apply again.
	h = newApps(fakeJobs{})
	first, apiErr := h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)
	res, apiErr = h.svc.UpdateApplication(context.Background(), uid, appID, api.UpdateApplicationRequest{Status: db.AppWithdrawn})
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppWithdrawn, res.Status)
	assert.Equal(t, db.EmailCancelled, h.repo.rows[first.Email.ID].Status)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled)
	_, apiErr = h.svc.Apply(context.Background(), uid, gmailReq())
	assert.Nil(t, apiErr)
}

func TestScheduledApplicationWhoseEmailFailed(t *testing.T) {
	h := newApps(fakeJobs{})
	req := gmailReq()
	req.ScheduledAt = &tomorrow
	res, apiErr := h.svc.Apply(context.Background(), uid, req)
	require.Nil(t, apiErr)
	h.mailer.err = core.ErrGmailAuth
	_, _ = h.harness.svc.DispatchScheduled(context.Background(), res.Email.ID)

	list, apiErr := h.svc.ListApplications(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Equal(t, StatusFailed, list.Items[0].Status)

	// A failed application does not block a new try.
	h.mailer.err = nil
	again, apiErr := h.svc.Apply(context.Background(), uid, gmailReq())
	require.Nil(t, apiErr)
	assert.Equal(t, db.AppSent, again.Status)
}

func TestUpdateApplication(t *testing.T) {
	h := newApps(fakeJobs{})
	_, apiErr := h.svc.Apply(context.Background(), uid, api.ApplyRequest{JobID: jobID, Via: db.ViaSelf})
	require.Nil(t, apiErr)

	for _, st := range []string{db.AppReplied, db.AppClosed, db.AppSent, db.AppWithdrawn} {
		res, apiErr := h.svc.UpdateApplication(context.Background(), uid, appID, api.UpdateApplicationRequest{Status: st})
		require.Nil(t, apiErr, st)
		assert.Equal(t, st, res.Status)
	}
	_, apiErr = h.svc.UpdateApplication(context.Background(), uid, "nope", api.UpdateApplicationRequest{Status: db.AppReplied})
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Code)
	_, apiErr = h.svc.UpdateApplication(context.Background(), "55555555-5555-5555-5555-555555555555", appID, api.UpdateApplicationRequest{Status: db.AppReplied})
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)
}

func TestShownStatus(t *testing.T) {
	sched := &db.Application{Status: db.AppScheduled, Via: db.ViaGmail}
	assert.Equal(t, StatusFailed, shownStatus(sched, nil))
	assert.Equal(t, db.AppScheduled, shownStatus(sched, &db.Email{Status: db.EmailSending}))
	assert.Equal(t, db.AppWithdrawn, shownStatus(sched, &db.Email{Status: db.EmailCancelled}))
	assert.Equal(t, db.AppReplied, shownStatus(&db.Application{Status: db.AppReplied, Via: db.ViaGmail}, &db.Email{Status: db.EmailSent}))
	assert.Equal(t, db.AppSent, shownStatus(&db.Application{Status: db.AppSent, Via: db.ViaSelf}, nil))
}
