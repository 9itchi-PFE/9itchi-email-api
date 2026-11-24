package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCfg = utils.AppConfig{
	SecretKey:        "0123456789abcdef0123456789abcdef",
	EncryptionKey:    "abcdef0123456789abcdef0123456789",
	ClientURL:        "http://client",
	TrackerPublicURL: "http://tracker",
}

const (
	uid     = "11111111-1111-1111-1111-111111111111"
	jobID   = "22222222-2222-2222-2222-222222222222"
	resumID = "33333333-3333-3333-3333-333333333333"
)

type memEmails struct {
	mu   sync.Mutex
	rows map[string]*db.Email
	n    int
	sent int
}

func newMemEmails() *memEmails { return &memEmails{rows: map[string]*db.Email{}} }

func (m *memEmails) Create(_ context.Context, e *db.Email) (*db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	c := *e
	c.ID = "email-" + string(rune('a'+m.n))
	c.TrackingID = "local-track"
	m.rows[c.ID] = &c
	out := c
	return &out, nil
}
func (m *memEmails) Get(_ context.Context, userID, id string) (*db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.rows[id]; ok && e.UserID == userID {
		c := *e
		return &c, nil
	}
	return nil, repositories.ErrNotFound
}
func (m *memEmails) GetByID(_ context.Context, id string) (*db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.rows[id]; ok {
		c := *e
		return &c, nil
	}
	return nil, repositories.ErrNotFound
}
func (m *memEmails) ListByUser(_ context.Context, userID string, _ int) ([]db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []db.Email{}
	for _, e := range m.rows {
		if e.UserID == userID {
			out = append(out, *e)
		}
	}
	return out, nil
}
func (m *memEmails) Claim(_ context.Context, id string) (*db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || (e.Status != db.EmailDraft && e.Status != db.EmailScheduled) {
		return nil, repositories.ErrNotFound
	}
	e.Status = db.EmailSending
	c := *e
	return &c, nil
}
func (m *memEmails) SetTracking(_ context.Context, id, t string) error {
	m.rows[id].TrackingID = t
	return nil
}
func (m *memEmails) SetScheduleTask(_ context.Context, id, t string) error {
	m.rows[id].ScheduleTaskID = &t
	return nil
}
func (m *memEmails) Finish(_ context.Context, id, status string, at *time.Time) error {
	m.rows[id].Status, m.rows[id].SentAt = status, at
	return nil
}
func (m *memEmails) Cancel(_ context.Context, userID, id string) (*db.Email, error) {
	e, ok := m.rows[id]
	if !ok || e.UserID != userID || e.Status != db.EmailScheduled {
		return nil, repositories.ErrNotFound
	}
	e.Status = db.EmailCancelled
	c := *e
	return &c, nil
}
func (m *memEmails) CountSentSince(context.Context, string, time.Time) (int, error) {
	return m.sent, nil
}

type fakeGmail struct {
	GmailService
	err *models.ApiError
}

func (f fakeGmail) Credentials(context.Context, string) (string, string, *models.ApiError) {
	return "tok", "me@gmail.com", f.err
}

type fakeMailer struct {
	core.Mailer
	sent []core.MailMessage
	err  error
}

func (f *fakeMailer) Send(_ context.Context, _ string, m core.MailMessage) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeTracker struct{ err error }

func (f fakeTracker) Register(context.Context, string, string) (string, string, error) {
	return "track-1", "/t/track-1", f.err
}

type fakeResumes struct {
	pdf []byte
	err error
}

func (f fakeResumes) Summary(context.Context, string, string) (string, error) {
	return "Ines, ENIS", nil
}
func (f fakeResumes) PDF(context.Context, string) ([]byte, error) { return f.pdf, f.err }

type fakeJobs struct {
	err  error
	info *core.JobInfo
}

func (f fakeJobs) ApplyInfo(context.Context, string) (*core.JobInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.info != nil {
		return f.info, nil
	}
	return &core.JobInfo{Title: "Stage React", Company: "Acme", ApplyEmail: "rh@acme.tn", Open: true}, nil
}

func (f fakeJobs) Job(context.Context, string, string) (*core.JobInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &core.JobInfo{Title: "Stage React", Company: "Acme", Description: "desc"}, nil
}

type fakeDrafter struct{ in core.DraftInput }

func (f *fakeDrafter) Draft(_ context.Context, bearer string, in core.DraftInput) (string, string, error) {
	f.in = in
	if bearer == "" {
		return "", "", errors.New("no bearer")
	}
	return "Candidature", "Bonjour", nil
}

type fakeScheduler struct {
	err       error
	scheduled []string
	cancelled []string
}

func (f *fakeScheduler) ScheduleEmail(_ context.Context, id string, _ time.Time) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.scheduled = append(f.scheduled, id)
	return "task-1", nil
}
func (f *fakeScheduler) Cancel(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

type fakeQuota struct {
	limit int
	ok    bool
}

func (f fakeQuota) Limit(context.Context, string, string) (int, bool, error) {
	return f.limit, f.ok, nil
}

type harness struct {
	svc    EmailService
	repo   *memEmails
	mailer *fakeMailer
	sched  *fakeScheduler
	draft  *fakeDrafter
	events *recordedEvents
}

// recordedEvents keeps what the service published, and can fail like a broker that is down.
type recordedEvents struct {
	got []publishedEvent
	err error
}

type publishedEvent struct {
	name    string
	payload map[string]any
}

func (r *recordedEvents) Publish(_ context.Context, name string, payload any) error {
	p, _ := payload.(map[string]any)
	r.got = append(r.got, publishedEvent{name, p})
	return r.err
}

func newHarness(gmailErr *models.ApiError, quota fakeQuota) *harness {
	h := &harness{repo: newMemEmails(), mailer: &fakeMailer{}, sched: &fakeScheduler{}, draft: &fakeDrafter{}, events: &recordedEvents{}}
	h.svc = NewEmailService(EmailDeps{
		Repo: h.repo, Gmail: fakeGmail{err: gmailErr}, Mailer: h.mailer, Drafter: h.draft, Jobs: fakeJobs{},
		Resumes: fakeResumes{pdf: []byte("%PDF")}, Tracker: fakeTracker{}, Scheduler: h.sched, Quotas: quota,
		Events: h.events,
		Config: testCfg, Now: func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	return h
}

var unlimited = fakeQuota{limit: -1, ok: true}

func sendReq() api.SendEmailRequest {
	return api.SendEmailRequest{JobID: jobID, ResumeID: resumID, To: "hr@acme.tn", Subject: "Candidature", Body: "Salut <b>\nBonjour"}
}

func TestSendEmail(t *testing.T) {
	h := newHarness(nil, unlimited)
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, e.Status)
	assert.Equal(t, "track-1", e.TrackingID)
	require.Len(t, h.mailer.sent, 1)
	m := h.mailer.sent[0]
	assert.Equal(t, "hr@acme.tn", m.To)
	assert.NotNil(t, m.Attachment)
	assert.Contains(t, m.HTML, "&lt;b&gt;<br>Bonjour")
	assert.Contains(t, m.HTML, "http://tracker/t/track-1")
	assert.Equal(t, db.EmailSent, h.repo.rows[e.ID].Status)
}

func TestSendEmailGmailNotConnected(t *testing.T) {
	h := newHarness(apiError(409, "gmail is not connected"), unlimited)
	_, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	assert.Empty(t, h.repo.rows)
}

func TestSendEmailQuota(t *testing.T) {
	h := newHarness(nil, fakeQuota{limit: 0, ok: true})
	_, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 402, apiErr.Code)

	h = newHarness(nil, fakeQuota{limit: 5, ok: true})
	h.repo.sent = 5
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 402, apiErr.Code)

	h = newHarness(nil, fakeQuota{limit: 5, ok: true})
	h.repo.sent = 4
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	assert.Nil(t, apiErr)
}

func TestSendEmailFailureIsRecorded(t *testing.T) {
	h := newHarness(nil, unlimited)
	h.mailer.err = core.ErrGmailAuth
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	assert.Equal(t, db.EmailFailed, h.repo.rows[e.ID].Status)

	h.mailer.err = errors.New("boom")
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)
}

func TestScheduleDispatchAndCancel(t *testing.T) {
	h := newHarness(nil, unlimited)
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailScheduled, e.Status)
	assert.Equal(t, []string{e.ID}, h.sched.scheduled)

	out, apiErr := h.svc.DispatchScheduled(context.Background(), e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	// A second dispatch (scheduler retry) must not send again.
	out, apiErr = h.svc.DispatchScheduled(context.Background(), e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	assert.Len(t, h.mailer.sent, 1)

	e2, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	c, apiErr := h.svc.CancelEmail(context.Background(), uid, e2.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailCancelled, c.Status)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled)
	out, apiErr = h.svc.DispatchScheduled(context.Background(), e2.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailCancelled, out.Status)
	assert.Len(t, h.mailer.sent, 1)

	_, apiErr = h.svc.CancelEmail(context.Background(), uid, e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	_, apiErr = h.svc.CancelEmail(context.Background(), "other", e2.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)
}

func TestScheduleValidatesDate(t *testing.T) {
	h := newHarness(nil, unlimited)
	for _, at := range []time.Time{
		time.Date(2026, 5, 10, 12, 0, 30, 0, time.UTC),
		time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		_, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
		require.NotNil(t, apiErr)
		assert.Equal(t, 400, apiErr.Code)
	}
}

func TestScheduleFailureMarksEmailFailed(t *testing.T) {
	h := newHarness(nil, unlimited)
	h.sched.err = core.ErrPeerUnavailable
	_, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)})
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)
	for _, e := range h.repo.rows {
		assert.Equal(t, db.EmailFailed, e.Status)
	}
}

func TestDraftEmail(t *testing.T) {
	h := newHarness(nil, unlimited)
	out, apiErr := h.svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID, ResumeID: resumID, Language: "fr"})
	require.Nil(t, apiErr)
	assert.Equal(t, "Candidature", out.Subject)
	assert.Equal(t, "Acme", h.draft.in.Company)
	assert.Equal(t, "Ines, ENIS", h.draft.in.CandidateSummary)

	h.svc.(*emailService).Jobs = fakeJobs{err: core.ErrPeerNotFound}
	_, apiErr = h.svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID})
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)
}

func TestGetAndListAreScopedToUser(t *testing.T) {
	h := newHarness(nil, unlimited)
	e, _ := h.svc.SendEmail(context.Background(), uid, sendReq())
	_, apiErr := h.svc.GetEmail(context.Background(), "other", e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)
	list, apiErr := h.svc.ListEmails(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Len(t, list, 1)
	list, _ = h.svc.ListEmails(context.Background(), "other")
	assert.Empty(t, list)
}

func TestSendNow(t *testing.T) {
	h := newHarness(nil, unlimited)
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)

	_, apiErr = h.svc.SendNow(context.Background(), "other", e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Code)

	out, apiErr := h.svc.SendNow(context.Background(), uid, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	assert.Len(t, h.mailer.sent, 1)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled)

	_, apiErr = h.svc.SendNow(context.Background(), uid, e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code, "already sent")
}

func TestSendNowGmailFails(t *testing.T) {
	h := newHarness(nil, unlimited)
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	h.mailer.err = core.ErrGmailAuth
	_, apiErr = h.svc.SendNow(context.Background(), uid, e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	assert.Equal(t, db.EmailFailed, h.repo.rows[e.ID].Status)
	assert.Empty(t, h.sched.cancelled)
}

func TestSendNowGmailNotConnected(t *testing.T) {
	h := newHarness(nil, unlimited)
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)

	// Same store, Gmail now disconnected.
	off := NewEmailService(EmailDeps{
		Repo: h.repo, Gmail: fakeGmail{err: apiError(409, "gmail is not connected")}, Mailer: h.mailer, Drafter: h.draft, Jobs: fakeJobs{},
		Resumes: fakeResumes{}, Tracker: fakeTracker{}, Scheduler: h.sched, Quotas: unlimited, Config: testCfg,
	})
	_, apiErr = off.SendNow(context.Background(), uid, e.ID)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
	assert.Equal(t, db.EmailScheduled, h.repo.rows[e.ID].Status, "the email stays scheduled")
}

func TestSendEmailNeverLeavesWithoutItsCV(t *testing.T) {
	for name, resumes := range map[string]fakeResumes{"no pdf": {}, "resume-api down": {err: core.ErrPeerUnavailable}} {
		h := newHarness(nil, unlimited)
		svc := NewEmailService(EmailDeps{
			Repo: h.repo, Gmail: fakeGmail{}, Mailer: h.mailer, Drafter: h.draft, Jobs: fakeJobs{},
			Resumes: resumes, Tracker: fakeTracker{}, Scheduler: h.sched, Quotas: unlimited, Config: testCfg,
		})
		e, apiErr := svc.SendEmail(context.Background(), uid, sendReq())
		require.NotNil(t, apiErr, name)
		assert.Equal(t, 502, apiErr.Code, name)
		assert.Equal(t, db.EmailFailed, e.Status, name)
		assert.Empty(t, h.mailer.sent, name)

		// Without a chosen CV the email is a plain email and leaves.
		req := sendReq()
		req.ResumeID = ""
		_, apiErr = svc.SendEmail(context.Background(), uid, req)
		require.Nil(t, apiErr, name)
		require.Len(t, h.mailer.sent, 1, name)
		assert.Nil(t, h.mailer.sent[0].Attachment, name)
	}
}

// The events say who sent the email: only one sent by the scheduler becomes a notification downstream.
func TestEmailEventsSayWhetherTheSchedulerSentIt(t *testing.T) {
	h := newHarness(nil, unlimited)
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	require.Len(t, h.events.got, 1)
	assert.Equal(t, core.EventEmailSent, h.events.got[0].name)
	assert.Equal(t, map[string]any{"email_id": e.ID, "user_id": uid, "scheduled": false}, h.events.got[0].payload)

	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	later, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	assert.Len(t, h.events.got, 1, "scheduling sends nothing yet")
	_, apiErr = h.svc.DispatchScheduled(context.Background(), later.ID)
	require.Nil(t, apiErr)
	require.Len(t, h.events.got, 2)
	assert.Equal(t, map[string]any{"email_id": later.ID, "user_id": uid, "scheduled": true}, h.events.got[1].payload)

	clicked, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	_, apiErr = h.svc.SendNow(context.Background(), uid, clicked.ID)
	require.Nil(t, apiErr)
	require.Len(t, h.events.got, 3)
	assert.Equal(t, false, h.events.got[2].payload["scheduled"], "« send now » is the student's own click")
}

func TestEmailFailureEventAndBrokerDown(t *testing.T) {
	h := newHarness(nil, unlimited)
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	e, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	h.mailer.err = errors.New("boom")
	_, _ = h.svc.DispatchScheduled(context.Background(), e.ID)
	assert.Equal(t, db.EmailFailed, h.repo.rows[e.ID].Status)
	require.Len(t, h.events.got, 1)
	assert.Equal(t, core.EventEmailFailed, h.events.got[0].name)
	assert.Equal(t, true, h.events.got[0].payload["scheduled"])

	// A broker that is down never changes what happened to the email.
	h = newHarness(nil, unlimited)
	h.events.err = errors.New("rabbitmq: broker unreachable")
	sent, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, sent.Status)
}
