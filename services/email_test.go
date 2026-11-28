package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	builtErr error
	trackErr error
	mu       sync.Mutex
	rows     map[string]*db.Email
	n        int
	sent     int
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
	if m.trackErr != nil {
		return m.trackErr
	}
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

func (m *memEmails) SentAnother(_ context.Context, userID, exceptID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.rows {
		if e.UserID == userID && e.ID != exceptID && e.Status == db.EmailSent {
			return true, nil
		}
	}
	return false, nil
}

// CountOriginal counts like the repository: the emails that carry the imported file and left or are about to.
func (m *memEmails) CountOriginal(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.rows {
		if e.UserID == userID && e.CVOriginal && (e.Status == db.EmailSent || e.Status == db.EmailSending || e.Status == db.EmailScheduled) {
			n++
		}
	}
	return n, nil
}

// LastBuiltSend counts like the repository: the sent emails that carry the CV built in 9itchi.
func (m *memEmails) LastBuiltSend(_ context.Context, userID string) (int, *time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.builtErr != nil {
		return 0, nil, m.builtErr
	}
	n := 0
	var last *time.Time
	for _, e := range m.rows {
		if e.UserID != userID || e.CVOriginal || e.Status != db.EmailSent {
			continue
		}
		n++
		if e.SentAt != nil && (last == nil || e.SentAt.After(*last)) {
			last = e.SentAt
		}
	}
	return n, last, nil
}

// UpdateScheduled writes like the repository: only an email of the user that is still scheduled.
func (m *memEmails) UpdateScheduled(_ context.Context, userID, id string, edit db.EmailEdit) (*db.Email, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.UserID != userID || e.Status != db.EmailScheduled {
		return nil, repositories.ErrNotFound
	}
	e.Recipient, e.Subject, e.Body, e.ResumeID = edit.Recipient, edit.Subject, edit.Body, edit.ResumeID
	e.ScheduledAt, e.ScheduleTaskID = edit.ScheduledAt, edit.ScheduleTaskID
	e.LetterFilename, e.AttachLetter, e.LetterLanguage = edit.LetterFilename, edit.AttachLetter, edit.LetterLanguage
	c := *e
	return &c, nil
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
	// pdfName is the name resume-api gives the CV ("": none given).
	pdfName string
	err     error
	// original is the imported file; originalErr fails its read.
	original    *core.Attachment
	originalErr error
	// names is what Names gives (nil: defaultNames); namesErr fails it.
	names    *core.ResumeNames
	namesErr error
	// letterErr fails the cover letter; letters, when set, records what was asked.
	letterErr error
	letters   *[]core.LetterRequest
}

var defaultNames = core.ResumeNames{Title: "Développeuse React", ContactName: "Ines Ben Salah"}

func (f fakeResumes) Names(context.Context, string, string) (*core.ResumeNames, error) {
	if f.namesErr != nil {
		return nil, f.namesErr
	}
	if f.names != nil {
		return f.names, nil
	}
	n := defaultNames
	return &n, nil
}

func (f fakeResumes) LetterPDF(_ context.Context, in core.LetterRequest) ([]byte, error) {
	if f.letters != nil {
		*f.letters = append(*f.letters, in)
	}
	if f.letterErr != nil {
		return nil, f.letterErr
	}
	return []byte("%PDF letter"), nil
}

func (f fakeResumes) Original(context.Context, string) (*core.Attachment, error) {
	return f.original, f.originalErr
}

func (f fakeResumes) Summary(context.Context, string, string) (string, error) {
	return "Ines, ENIS", nil
}
func (f fakeResumes) PDF(context.Context, string) (*core.Attachment, error) {
	if f.err != nil || f.pdf == nil {
		return nil, f.err
	}
	return &core.Attachment{Filename: f.pdfName, ContentType: "application/pdf", Data: f.pdf}, nil
}

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

type fakeDrafter struct {
	in    core.DraftInput
	calls int
	err   error
}

func (f *fakeDrafter) Draft(_ context.Context, bearer string, in core.DraftInput) (string, string, error) {
	f.in = in
	f.calls++
	if bearer == "" {
		return "", "", errors.New("no bearer")
	}
	if f.err != nil {
		return "", "", f.err
	}
	return "Candidature", fmt.Sprintf("Bonjour (%d)", f.calls), nil
}

type memDrafts struct {
	mu   sync.Mutex
	rows []db.AIDraft
	at   time.Time
}

func (m *memDrafts) Latest(_ context.Context, userID, jobID, hash, language, tone string) (*db.AIDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.UserID == userID && r.JobID != nil && *r.JobID == jobID && r.InputHash == hash && r.Language == language && r.Tone == tone {
			return &r, nil
		}
	}
	return nil, repositories.ErrNotFound
}

// LatestByURL finds like the repository: the newest letter for the link, input, language and tone.
func (m *memDrafts) LatestByURL(_ context.Context, userID, link, hash, language, tone string) (*db.AIDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.UserID == userID && r.SourceURL != nil && *r.SourceURL == link && r.InputHash == hash && r.Language == language && r.Tone == tone {
			return &r, nil
		}
	}
	return nil, repositories.ErrNotFound
}

// LatestAnyByURL finds like the repository: the newest letter for the link, language and tone, whatever its input.
func (m *memDrafts) LatestAnyByURL(_ context.Context, userID, link, language, tone string) (*db.AIDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.UserID == userID && r.SourceURL != nil && *r.SourceURL == link && r.Language == language && r.Tone == tone {
			return &r, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memDrafts) Save(_ context.Context, d *db.AIDraft) (*db.AIDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *d
	c.CreatedAt = m.at
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	}
	m.rows = append(m.rows, c)
	return &c, nil
}

func (m *memDrafts) CountSince(_ context.Context, userID string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.rows {
		if r.UserID == userID && !r.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
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

// fakeQuota is a plan: its monthly limit (when ok), and the applications it gives without a subscription.
type fakeQuota struct {
	limit int
	ok    bool
	free  int
	err   error
}

func (f fakeQuota) Quotas(context.Context, string) (map[string]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	q := map[string]int{}
	if f.ok {
		q[core.QuotaEmailsPerMonth] = f.limit
	}
	if f.free != 0 {
		q[core.QuotaFreeApplications] = f.free
	}
	return q, nil
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
	return newHarnessWith(gmailErr, quota, fakeResumes{pdf: []byte("%PDF")})
}

func newHarnessWith(gmailErr *models.ApiError, quota fakeQuota, resumes fakeResumes) *harness {
	h := &harness{repo: newMemEmails(), mailer: &fakeMailer{}, sched: &fakeScheduler{}, draft: &fakeDrafter{}, events: &recordedEvents{}}
	h.svc = NewEmailService(EmailDeps{
		Repo: h.repo, Gmail: fakeGmail{err: gmailErr}, Mailer: h.mailer, Drafter: h.draft, Jobs: fakeJobs{},
		Resumes: resumes, Tracker: fakeTracker{}, Scheduler: h.sched, Quotas: quota,
		Events: h.events, Drafts: &memDrafts{},
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
	assert.Len(t, m.Attachments, 2, "the CV and the cover letter")
	assert.Contains(t, m.HTML, "&lt;b&gt;<br>Bonjour")
	assert.Contains(t, m.HTML, "http://tracker/t/track-1")
	assert.Equal(t, db.EmailSent, h.repo.rows[e.ID].Status)
}

// A tracking id that cannot be stored here still sends the pixel: the tracker counts the opens on its own.
func TestSendEmailTrackingNotStored(t *testing.T) {
	h := newHarness(nil, unlimited)
	h.repo.trackErr = errors.New("invalid input syntax for type uuid")
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, e.Status)
	require.Len(t, h.mailer.sent, 1)
	assert.Contains(t, h.mailer.sent[0].HTML, "http://tracker/t/track-1")
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

	out, apiErr := dispatchAt(h.svc, at, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	// A second dispatch (scheduler retry) must not send again.
	out, apiErr = dispatchAt(h.svc, at, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	assert.Len(t, h.mailer.sent, 1)

	e2, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	c, apiErr := h.svc.CancelEmail(context.Background(), uid, e2.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailCancelled, c.Status)
	assert.Equal(t, []string{"task-1"}, h.sched.cancelled)
	out, apiErr = dispatchAt(h.svc, at, e2.ID)
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
	assert.Nil(t, out.Limit, "no key in the plan: no limit")
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
		assert.Len(t, h.mailer.sent[0].Attachments, 1, name+": the letter only")
	}
}

// The events say who sent the email: only one sent by the scheduler becomes a notification downstream.
func TestEmailEventsSayWhetherTheSchedulerSentIt(t *testing.T) {
	h := newHarness(nil, unlimited)
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	// The very first email also announces the student's first application; it is set aside here.
	require.Len(t, h.events.got, 2)
	assert.Equal(t, core.EventApplicationFirstSent, h.events.got[1].name)
	h.events.got = h.events.got[:1]
	assert.Equal(t, core.EventEmailSent, h.events.got[0].name)
	assert.Equal(t, map[string]any{"email_id": e.ID, "user_id": uid, "scheduled": false}, h.events.got[0].payload)

	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	later, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	assert.Len(t, h.events.got, 1, "scheduling sends nothing yet")
	_, apiErr = dispatchAt(h.svc, at, later.ID)
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
	_, _ = dispatchAt(h.svc, at, e.ID)
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

// noPlan is a student without a subscription: nothing per month, two applications with the imported file.
var noPlan = fakeQuota{limit: 0, ok: true, free: 2}

func imported() fakeResumes {
	return fakeResumes{pdf: []byte("%PDF built in 9itchi"), original: &core.Attachment{
		Filename: "cv-ines.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: []byte("as imported"),
	}}
}

func TestSendWithoutSubscriptionCarriesTheImportedFile(t *testing.T) {
	h := newHarnessWith(nil, noPlan, imported())
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, e.Status)
	assert.True(t, e.CVOriginal)
	require.Len(t, h.mailer.sent, 1)
	require.Len(t, h.mailer.sent[0].Attachments, 2)
	att := h.mailer.sent[0].Attachments[0]
	assert.Equal(t, "cv-ines.docx", att.Filename)
	assert.Equal(t, []byte("as imported"), att.Data, "the CV built in 9itchi is not sent without a subscription")
	assert.Contains(t, att.ContentType, "wordprocessingml")

	a, apiErr := h.svc.Allowance(context.Background(), uid)
	require.Nil(t, apiErr)
	require.NotNil(t, a.Limit)
	assert.Equal(t, api.AllowanceResponse{Mode: AllowOriginal, Limit: a.Limit, Used: 1}, *a)
	assert.Equal(t, 2, *a.Limit)

	// The second one may be scheduled: it counts before it leaves.
	at := time.Date(2026, 5, 12, 8, 30, 0, 0, time.UTC)
	s, apiErr := h.svc.ScheduleEmail(context.Background(), uid, api.ScheduleEmailRequest{SendEmailRequest: sendReq(), ScheduledAt: at})
	require.Nil(t, apiErr)
	assert.True(t, s.CVOriginal)

	// The third is refused, and nothing is stored or sent for it.
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 402, apiErr.Code)
	assert.Len(t, h.repo.rows, 2)
	assert.Len(t, h.mailer.sent, 1)

	// The scheduled one leaves with the imported file too.
	out, apiErr := dispatchAt(h.svc, at, s.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	assert.Equal(t, []byte("as imported"), h.mailer.sent[1].Attachments[0].Data)
}

func TestSendWithoutSubscriptionNeedsAnImportedFile(t *testing.T) {
	// A CV that was not started from a file: said before anything is stored.
	h := newHarnessWith(nil, noPlan, fakeResumes{pdf: []byte("%PDF")})
	_, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 422, apiErr.Code)
	assert.Empty(t, h.repo.rows)

	h = newHarnessWith(nil, noPlan, fakeResumes{originalErr: core.ErrPeerUnavailable})
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)
	assert.Empty(t, h.repo.rows)
}

func TestSubscriberStillSendsTheBuiltCV(t *testing.T) {
	h := newHarnessWith(nil, fakeQuota{limit: 5, ok: true, free: 2}, imported())
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.False(t, e.CVOriginal)
	assert.Equal(t, "CV.pdf", h.mailer.sent[0].Attachments[0].Filename)
	assert.Equal(t, []byte("%PDF built in 9itchi"), h.mailer.sent[0].Attachments[0].Data)
}

func TestAllowance(t *testing.T) {
	five := 5
	for name, tc := range map[string]struct {
		quota fakeQuota
		sent  int
		want  api.AllowanceResponse
	}{
		"no limit in the plan":     {fakeQuota{}, 0, api.AllowanceResponse{Mode: AllowPlan}},
		"unlimited":                {unlimited, 9, api.AllowanceResponse{Mode: AllowPlan}},
		"monthly limit":            {fakeQuota{limit: 5, ok: true}, 3, api.AllowanceResponse{Mode: AllowPlan, Limit: &five, Used: 3}},
		"no subscription, nothing": {fakeQuota{limit: 0, ok: true}, 0, api.AllowanceResponse{Mode: AllowNone}},
		"no subscription, five":    {fakeQuota{limit: 0, ok: true, free: 5}, 3, api.AllowanceResponse{Mode: AllowOriginal, Limit: &five}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(nil, tc.quota)
			h.repo.sent = tc.sent
			got, apiErr := h.svc.Allowance(context.Background(), uid)
			require.Nil(t, apiErr)
			assert.Equal(t, tc.want, *got)
		})
	}
	h := newHarness(nil, fakeQuota{err: core.ErrPeerUnavailable})
	_, apiErr := h.svc.Allowance(context.Background(), uid)
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.Code)
}

// The first email a student ever sends is announced once: the second one is not a first.
func TestFirstApplicationIsAnnouncedOnce(t *testing.T) {
	h := newHarness(nil, unlimited)
	firsts := func() int {
		n := 0
		for _, e := range h.events.got {
			if e.name == core.EventApplicationFirstSent {
				assert.Equal(t, map[string]any{"user_id": uid}, e.payload, "nothing but who")
				n++
			}
		}
		return n
	}
	_, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, 1, firsts())
	_, apiErr = h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.Equal(t, 1, firsts())

	// An email that does not leave announces nothing.
	h = newHarness(nil, unlimited)
	h.mailer.err = assert.AnError
	_, _ = h.svc.SendEmail(context.Background(), uid, sendReq())
	assert.Zero(t, firsts())
}

func TestAILettersAreKeptAndCounted(t *testing.T) {
	h := newHarness(nil, unlimited)
	drafts := &memDrafts{}
	svc := h.svc.(*emailService)
	svc.Drafts = drafts
	svc.Quotas = mapQuota{core.QuotaAIDrafts: 1}
	req := api.DraftEmailRequest{JobID: jobID, ResumeID: resumID, Language: "fr"}

	out, apiErr := svc.DraftEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (1)", out.Body)
	require.NotNil(t, out.Limit)
	assert.Equal(t, 1, *out.Limit)
	assert.Equal(t, 1, out.Used)
	assert.Equal(t, "formal", h.draft.in.Tone, "formal by default")

	out, apiErr = svc.DraftEmail(context.Background(), uid, req)
	require.Nil(t, apiErr, "the same offer and CV give the letter back, even at the limit")
	assert.Equal(t, "Bonjour (1)", out.Body)
	assert.Equal(t, 1, h.draft.calls, "without asking the AI again")
	assert.Equal(t, 1, out.Used, "nor counting it")

	for name, r := range map[string]api.DraftEmailRequest{
		"« Réécrire »":     {JobID: jobID, ResumeID: resumID, Language: "fr", Fresh: true},
		"another tone":     {JobID: jobID, ResumeID: resumID, Language: "fr", Tone: "friendly"},
		"another language": {JobID: jobID, ResumeID: resumID, Language: "en"},
	} {
		_, apiErr = svc.DraftEmail(context.Background(), uid, r)
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusPaymentRequired, apiErr.Code, name)
		assert.Contains(t, apiErr.Error.Error(), "monthly limit", name)
	}
	assert.Equal(t, 1, h.draft.calls)

	svc.Quotas = mapQuota{core.QuotaAIDrafts: 5}
	out, apiErr = svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID, ResumeID: resumID, Language: "fr", Fresh: true})
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (2)", out.Body, "« Réécrire » asks for a new letter")
	out, apiErr = svc.DraftEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (2)", out.Body, "the newest letter comes back")

	drafts.at = time.Date(2026, 4, 30, 23, 0, 0, 0, time.UTC)
	other := &memDrafts{at: drafts.at}
	svc.Drafts = other
	svc.Quotas = mapQuota{core.QuotaAIDrafts: 1}
	_, apiErr = svc.DraftEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	other.at = time.Time{}
	_, apiErr = svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID, ResumeID: resumID, Language: "fr", Fresh: true})
	require.Nil(t, apiErr, "last month's letter does not count this month")
}

func TestAILetterNotInThePlan(t *testing.T) {
	h := newHarness(nil, unlimited)
	svc := h.svc.(*emailService)
	svc.Quotas = mapQuota{core.QuotaAIDrafts: 0}
	_, apiErr := svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID, ResumeID: resumID})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Contains(t, apiErr.Error.Error(), "part of the subscription")
	assert.Zero(t, h.draft.calls)

	svc.Quotas = mapQuota{}
	h.draft.err = errors.New("ai down")
	_, apiErr = svc.DraftEmail(context.Background(), uid, api.DraftEmailRequest{JobID: jobID, ResumeID: resumID})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	n, _ := svc.Drafts.CountSince(context.Background(), uid, time.Time{})
	assert.Zero(t, n, "a letter the AI did not write is not counted")
}

// dispatchAt is schedule-api dispatching an email at the time it was scheduled for (the service's clock moved
// there for the call only).
func dispatchAt(svc EmailService, at time.Time, id string) (*db.Email, *models.ApiError) {
	s := svc.(*emailService)
	prev := s.Now
	s.Now = func() time.Time { return at }
	defer func() { s.Now = prev }()
	return s.DispatchScheduled(context.Background(), id)
}
