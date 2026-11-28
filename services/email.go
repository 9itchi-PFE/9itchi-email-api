package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

const (
	accessTokenTTL   = 2 * time.Minute
	minScheduleDelay = time.Minute
	maxScheduleAhead = 365 * 24 * time.Hour
	listLimit        = 100
)

// EmailService holds all business logic for email. Handlers stay thin.
type EmailService interface {
	// POST /emails/draft: AI draft for a job + resume
	DraftEmail(ctx context.Context, userID string, req api.DraftEmailRequest) (*api.DraftEmailResponse, *models.ApiError)
	// POST /emails: send immediately via Gmail
	SendEmail(ctx context.Context, userID string, req api.SendEmailRequest) (*db.Email, *models.ApiError)
	// POST /emails/schedule: schedule for date/time
	ScheduleEmail(ctx context.Context, userID string, req api.ScheduleEmailRequest) (*db.Email, *models.ApiError)
	// GET /emails: history: draft, scheduled, sent, opened, failed
	ListEmails(ctx context.Context, userID string) ([]db.Email, *models.ApiError)
	// GET /emails/:id: email detail
	GetEmail(ctx context.Context, userID string, id string) (*db.Email, *models.ApiError)
	// DELETE /emails/:id: cancel a scheduled email
	CancelEmail(ctx context.Context, userID string, id string) (*db.Email, *models.ApiError)
	// SendNow delivers a scheduled email of the user right away and drops its schedule task
	SendNow(ctx context.Context, userID string, id string) (*db.Email, *models.ApiError)
	// GET /applications/allowance: what the user's plan lets them send, and how much of it is used
	Allowance(ctx context.Context, userID string) (*api.AllowanceResponse, *models.ApiError)
	// POST /internal/emails/:id/dispatch: called by schedule-api when due
	DispatchScheduled(ctx context.Context, id string) (*db.Email, *models.ApiError)
	// GET /admin/users/:id/sent-with-cv: how many applications left with the CV built in 9itchi, and the last time
	UserBuiltSends(ctx context.Context, id string) (*api.UserBuiltSendsResponse, *models.ApiError)
	// POST /emails/draft-from-link: AI draft for an offer given by its link (9itchi or elsewhere)
	DraftFromLink(ctx context.Context, userID string, req api.DraftFromLinkRequest) (*api.DraftFromLinkResponse, *models.ApiError)
	// PATCH /emails/:id: edit a scheduled email (recipient, text, CV, time)
	UpdateEmail(ctx context.Context, userID, id string, req api.UpdateEmailRequest) (*db.Email, *models.ApiError)
	// POST /recruiters/:id/draft: AI draft of a spontaneous application to a recruiter the student found
	DraftForRecruiter(ctx context.Context, userID, id string, req api.RecruiterDraftRequest) (*api.DraftEmailResponse, *models.ApiError)
	// DraftFollowUp writes the follow-up of an application (kept and counted like the other AI letters)
	DraftFollowUp(ctx context.Context, userID, applicationID string, fresh bool, in core.DraftInput) (*api.DraftEmailResponse, *models.ApiError)
	// SendFollowUp sends a follow-up from the student's Gmail: part of the subscription, counted in the month's
	// emails, never with a CV
	SendFollowUp(ctx context.Context, userID string, req api.SendEmailRequest) (*db.Email, *models.ApiError)
	// GET /emails/attachment-names: the name of the CV file and the default name of the cover letter
	AttachmentNames(ctx context.Context, userID string, q api.AttachmentNamesQuery) (*api.AttachmentNamesResponse, *models.ApiError)
}

// EmailDeps are the collaborators of the email service (all mockable).
type EmailDeps struct {
	Repo       repositories.EmailRepository
	Gmail      GmailService
	Mailer     core.Mailer
	Drafter    core.Drafter
	Jobs       core.JobSource
	Resumes    core.ResumeSource
	Tracker    core.Tracker
	Scheduler  core.Scheduler
	Quotas     core.QuotaSource
	Drafts     repositories.DraftRepository
	Offers     core.OfferReader
	Recruiters repositories.RecruiterRepository
	Events     core.Publisher
	Config     utils.AppConfig
	Now        func() time.Time
}

type emailService struct {
	EmailDeps
}

func NewEmailService(d EmailDeps) EmailService {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Events == nil {
		d.Events = core.LogPublisher{}
	}
	return &emailService{d}
}

func repoError(err error, notFound string) *models.ApiError {
	if errors.Is(err, repositories.ErrNotFound) {
		return apiError(http.StatusNotFound, notFound)
	}
	utils.Logger.Logger.Warnw("email repository error", "error", err)
	return apiError(http.StatusInternalServerError, "internal error")
}

func peerError(err error, notFound string) *models.ApiError {
	if errors.Is(err, core.ErrPeerNotFound) {
		return apiError(http.StatusNotFound, notFound)
	}
	utils.Logger.Logger.Warnw("peer service error", "error", err)
	return apiError(http.StatusBadGateway, "a service is unavailable")
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// bearer is a short-lived token of the user, to call peer routes that need the user's identity.
func (s *emailService) bearer(userID string) (string, error) {
	return utils.IssueToken(s.Config, utils.Claims{ID: userID, Role: "user", Type: utils.TokenAccess}, accessTokenTTL)
}

func (s *emailService) DraftEmail(ctx context.Context, userID string, req api.DraftEmailRequest) (*api.DraftEmailResponse, *models.ApiError) {
	out, _, apiErr := s.draftForJob(ctx, userID, req)
	return out, apiErr
}

// draftForJob is DraftEmail, also giving the offer it read.
func (s *emailService) draftForJob(ctx context.Context, userID string, req api.DraftEmailRequest) (*api.DraftEmailResponse, *core.JobInfo, *models.ApiError) {
	token, err := s.bearer(userID)
	if err != nil {
		return nil, nil, apiError(http.StatusInternalServerError, "internal error")
	}
	job, err := s.Jobs.Job(ctx, token, req.JobID)
	if err != nil {
		return nil, nil, peerError(err, "job not found")
	}
	out, apiErr := s.draftJob(ctx, userID, token, job, req)
	return out, job, apiErr
}

func (s *emailService) draftJob(ctx context.Context, userID, token string, job *core.JobInfo, req api.DraftEmailRequest) (*api.DraftEmailResponse, *models.ApiError) {
	var err error
	summary := ""
	if req.ResumeID != "" {
		if summary, err = s.Resumes.Summary(ctx, token, req.ResumeID); err != nil {
			return nil, peerError(err, "resume not found")
		}
	}
	language, tone := req.Language, req.Tone
	if language == "" {
		language = "fr"
	}
	if tone == "" {
		tone = "formal"
	}
	hash := digest(job.Title, job.Company, job.Description, summary)
	if !req.Fresh {
		d, err := s.Drafts.Latest(ctx, userID, req.JobID, hash, language, tone)
		switch {
		case err == nil:
			return s.draftResponse(ctx, userID, d.Subject, d.Body)
		case !errors.Is(err, repositories.ErrNotFound):
			return nil, repoError(err, "draft not found")
		}
	}
	limit, used, apiErr := s.monthlyUse(ctx, userID, core.QuotaAIDrafts, s.Drafts.CountSince)
	if apiErr != nil {
		return nil, apiErr
	}
	switch {
	case limit != nil && *limit == 0:
		return nil, apiError(http.StatusPaymentRequired, "the AI letter is part of the subscription")
	case limit != nil && used >= *limit:
		return nil, apiError(http.StatusPaymentRequired, "monthly limit of AI letters of your plan reached")
	}
	subject, body, err := s.Drafter.Draft(ctx, token, core.DraftInput{
		JobTitle: job.Title, Company: job.Company, JobDescription: job.Description,
		CandidateSummary: summary, Language: language, Tone: tone,
	})
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	if _, err := s.Drafts.Save(ctx, &db.AIDraft{
		UserID: userID, JobID: &req.JobID, Language: language, Tone: tone, InputHash: hash, Subject: subject, Body: body,
	}); err != nil {
		return nil, repoError(err, "draft not found")
	}
	return s.draftResponse(ctx, userID, subject, body)
}

func (s *emailService) draftResponse(ctx context.Context, userID, subject, body string) (*api.DraftEmailResponse, *models.ApiError) {
	limit, used, apiErr := s.monthlyUse(ctx, userID, core.QuotaAIDrafts, s.Drafts.CountSince)
	if apiErr != nil {
		return nil, apiErr
	}
	return &api.DraftEmailResponse{Subject: subject, Body: body, Limit: limit, Used: used}, nil
}

// monthlyUse is the plan's monthly limit for key and how much of it the user used this calendar month (UTC);
// limit is nil when the plan sets none.
func (s *emailService) monthlyUse(ctx context.Context, userID, key string, count func(context.Context, string, time.Time) (int, error)) (*int, int, *models.ApiError) {
	quotas, err := s.Quotas.Quotas(ctx, userID)
	if err != nil {
		return nil, 0, peerError(err, "plan not found")
	}
	limit, ok := quotas[key]
	if !ok || limit < 0 {
		return nil, 0, nil
	}
	now := s.Now().UTC()
	n, err := count(ctx, userID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, 0, repoError(err, "not found")
	}
	return &limit, n, nil
}

// digest is the SHA-256 of what the AI reads, parts separated so that moving text between them changes it.
func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Allowance modes: how the user's applications leave.
const (
	// AllowPlan: with the CV built in 9itchi, within the monthly limit of the plan.
	AllowPlan = "plan"
	// AllowOriginal: no subscription; a few applications over the life of the account, with the imported file.
	AllowOriginal = "original"
	// AllowNone: the plan sends nothing.
	AllowNone = "none"
)

// allowance reads what the user's plan lets them send and how much of it is used. Limit is nil when unlimited.
func (s *emailService) allowance(ctx context.Context, userID string) (*api.AllowanceResponse, *models.ApiError) {
	quotas, err := s.Quotas.Quotas(ctx, userID)
	if err != nil {
		return nil, peerError(err, "plan not found")
	}
	monthly, ok := quotas[core.QuotaEmailsPerMonth]
	if !ok || monthly < 0 {
		return &api.AllowanceResponse{Mode: AllowPlan}, nil
	}
	if monthly > 0 {
		now := s.Now().UTC()
		n, err := s.Repo.CountSentSince(ctx, userID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			return nil, repoError(err, "email not found")
		}
		return &api.AllowanceResponse{Mode: AllowPlan, Limit: &monthly, Used: n}, nil
	}
	free := quotas[core.QuotaFreeApplications]
	if free <= 0 {
		return &api.AllowanceResponse{Mode: AllowNone}, nil
	}
	n, err := s.Repo.CountOriginal(ctx, userID)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return &api.AllowanceResponse{Mode: AllowOriginal, Limit: &free, Used: n}, nil
}

func (s *emailService) Allowance(ctx context.Context, userID string) (*api.AllowanceResponse, *models.ApiError) {
	return s.allowance(ctx, userID)
}

// checkQuota enforces the plan: the monthly limit of a subscription, or without one the few applications that
// leave with the imported file. original says the email must carry that file.
func (s *emailService) checkQuota(ctx context.Context, userID string) (original bool, apiErr *models.ApiError) {
	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return false, apiErr
	}
	switch {
	case a.Mode == AllowNone:
		return false, apiError(http.StatusPaymentRequired, "sending applications is part of the subscription")
	case a.Limit != nil && a.Used >= *a.Limit && a.Mode == AllowOriginal:
		return false, apiError(http.StatusPaymentRequired, "the applications without a subscription are used up")
	case a.Limit != nil && a.Used >= *a.Limit:
		return false, apiError(http.StatusPaymentRequired, "monthly email limit of your plan reached")
	}
	return a.Mode == AllowOriginal, nil
}

func (s *emailService) create(ctx context.Context, userID string, req api.SendEmailRequest, status string, at *time.Time) (*db.Email, *models.ApiError) {
	if _, _, apiErr := s.Gmail.Credentials(ctx, userID); apiErr != nil {
		return nil, apiErr
	}
	original, apiErr := s.checkQuota(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	if original && req.ResumeID != "" {
		if apiErr := s.checkAttachable(ctx, req.ResumeID); apiErr != nil {
			return nil, apiErr
		}
	}
	attachLetter := req.AttachLetter == nil || *req.AttachLetter
	letterName := optional(cleanStem(req.LetterFilename, ""))
	if attachLetter && letterName != nil && req.ResumeID != "" {
		if apiErr := s.checkLetterName(ctx, userID, req.ResumeID, *letterName); apiErr != nil {
			return nil, apiErr
		}
	}
	language := letterLanguage(req.Language, req.Body)
	e, err := s.Repo.Create(ctx, &db.Email{
		UserID: userID, JobID: optional(req.JobID), ResumeID: optional(req.ResumeID), Recipient: req.To,
		Subject: strings.TrimSpace(req.Subject), Body: req.Body, Status: status, ScheduledAt: at, CVOriginal: original,
		LetterFilename: letterName, AttachLetter: attachLetter, LetterLanguage: &language,
	})
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return e, nil
}

// checkLetterName refuses a cover letter named like the CV it leaves with: the recruiter would get two files with
// the same name. A CV that cannot be read now is not checked (the send says so when it leaves).
func (s *emailService) checkLetterName(ctx context.Context, userID, resumeID, letterStem string) *models.ApiError {
	token, err := s.bearer(userID)
	if err != nil {
		return apiError(http.StatusInternalServerError, "internal error")
	}
	names, err := s.Resumes.Names(ctx, token, resumeID)
	if errors.Is(err, core.ErrPeerNotFound) {
		return apiError(http.StatusNotFound, "resume not found")
	}
	if err != nil || names == nil {
		utils.Logger.Logger.Warnw("email: CV name unavailable, letter name not checked", "error", err)
		return nil
	}
	if strings.EqualFold(letterStem, cvStem(names.Title, names.ContactName)) {
		return apiError(http.StatusBadRequest, "letter_filename must differ from the CV name")
	}
	return nil
}

// AttachmentNames gives the composer the names the files will have: the CV's (resume-api names it after its title)
// and the default name of the cover letter in that language. A CV that cannot be read now gives generic names.
func (s *emailService) AttachmentNames(ctx context.Context, userID string, q api.AttachmentNamesQuery) (*api.AttachmentNamesResponse, *models.ApiError) {
	language := letterLanguage(q.Language, "")
	if q.ResumeID == "" {
		return &api.AttachmentNamesResponse{LetterFilename: defaultLetterStem(language, "")}, nil
	}
	token, err := s.bearer(userID)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "internal error")
	}
	names, err := s.Resumes.Names(ctx, token, q.ResumeID)
	if errors.Is(err, core.ErrPeerNotFound) {
		return nil, apiError(http.StatusNotFound, "resume not found")
	}
	if err != nil || names == nil {
		utils.Logger.Logger.Warnw("email: CV names unavailable, generic attachment names", "error", err)
		return &api.AttachmentNamesResponse{CVFilename: "CV.pdf", LetterFilename: defaultLetterStem(language, "")}, nil
	}
	return &api.AttachmentNamesResponse{
		CVFilename:     cvStem(names.Title, names.ContactName) + ".pdf",
		LetterFilename: defaultLetterStem(language, names.ContactName),
	}, nil
}

// checkAttachable makes sure the CV has an imported file, for an email that must carry it. Said now, while the
// student can still do something about it, and not when the email fails to leave.
func (s *emailService) checkAttachable(ctx context.Context, resumeID string) *models.ApiError {
	f, err := s.Resumes.Original(ctx, resumeID)
	if err != nil {
		return peerError(err, "resume not found")
	}
	if f == nil {
		return apiError(http.StatusUnprocessableEntity, "this CV has no imported file to send")
	}
	return nil
}

func (s *emailService) SendEmail(ctx context.Context, userID string, req api.SendEmailRequest) (*db.Email, *models.ApiError) {
	e, apiErr := s.create(ctx, userID, req, db.EmailDraft, nil)
	if apiErr != nil {
		return nil, apiErr
	}
	return s.deliver(ctx, e.ID, false)
}

func (s *emailService) ScheduleEmail(ctx context.Context, userID string, req api.ScheduleEmailRequest) (*db.Email, *models.ApiError) {
	at := req.ScheduledAt.UTC()
	now := s.Now()
	if at.Before(now.Add(minScheduleDelay)) || at.After(now.Add(maxScheduleAhead)) {
		return nil, apiError(http.StatusBadRequest, "scheduled_at must be between one minute and one year from now")
	}
	e, apiErr := s.create(ctx, userID, req.SendEmailRequest, db.EmailScheduled, &at)
	if apiErr != nil {
		return nil, apiErr
	}
	taskID, err := s.Scheduler.ScheduleEmail(ctx, e.ID, at)
	if err != nil {
		_ = s.Repo.Finish(ctx, e.ID, db.EmailFailed, nil)
		return nil, peerError(err, "schedule not found")
	}
	if err := s.Repo.SetScheduleTask(ctx, e.ID, taskID); err != nil {
		utils.Logger.Logger.Warnw("email: could not store schedule task", "error", err)
	}
	e.ScheduleTaskID = &taskID
	return e, nil
}

func (s *emailService) ListEmails(ctx context.Context, userID string) ([]db.Email, *models.ApiError) {
	out, err := s.Repo.ListByUser(ctx, userID, listLimit)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return out, nil
}

func (s *emailService) GetEmail(ctx context.Context, userID, id string) (*db.Email, *models.ApiError) {
	e, err := s.Repo.Get(ctx, userID, id)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return e, nil
}

func (s *emailService) CancelEmail(ctx context.Context, userID, id string) (*db.Email, *models.ApiError) {
	e, err := s.Repo.Cancel(ctx, userID, id)
	if errors.Is(err, repositories.ErrNotFound) {
		if _, getErr := s.Repo.Get(ctx, userID, id); getErr == nil {
			return nil, apiError(http.StatusConflict, "only a scheduled email can be cancelled")
		}
	}
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	if e.ScheduleTaskID != nil {
		// The dispatch of a cancelled email is a no-op anyway, so a failed cancel is only logged.
		if err := s.Scheduler.Cancel(ctx, *e.ScheduleTaskID); err != nil {
			utils.Logger.Logger.Warnw("email: could not cancel schedule task", "error", err)
		}
	}
	return e, nil
}

func (s *emailService) SendNow(ctx context.Context, userID, id string) (*db.Email, *models.ApiError) {
	e, err := s.Repo.Get(ctx, userID, id)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	if e.Status != db.EmailScheduled {
		return nil, apiError(http.StatusConflict, "only a scheduled email can be sent now")
	}
	// Checked before the email is claimed: with Gmail disconnected it stays scheduled instead of failing.
	if _, _, apiErr := s.Gmail.Credentials(ctx, userID); apiErr != nil {
		return nil, apiErr
	}
	out, apiErr := s.deliver(ctx, id, false)
	if apiErr != nil {
		return nil, apiErr
	}
	if e.ScheduleTaskID != nil {
		// The dispatch of an email already sent is a no-op, so a failed cancel is only logged.
		if err := s.Scheduler.Cancel(ctx, *e.ScheduleTaskID); err != nil {
			utils.Logger.Logger.Warnw("email: could not cancel schedule task", "error", err)
		}
	}
	return out, nil
}

func (s *emailService) DispatchScheduled(ctx context.Context, id string) (*db.Email, *models.ApiError) {
	e, err := s.Repo.GetByID(ctx, id)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	if e.Status != db.EmailScheduled {
		return e, nil
	}
	if e.ScheduledAt != nil && e.ScheduledAt.After(s.Now().Add(minScheduleDelay)) {
		// The task of a time the student since moved: the email waits for its new task.
		return e, nil
	}
	out, apiErr := s.deliver(ctx, id, true)
	if out == nil {
		if apiErr.Code == http.StatusConflict {
			// Claimed by a concurrent dispatch: nothing left to do.
			return e, nil
		}
		return nil, apiErr
	}
	// A failed delivery is recorded in the email status; the dispatch call itself succeeded.
	return out, nil
}

// deliver sends one draft/scheduled email through Gmail and records the outcome.
// scheduled says the scheduler is sending it: the events carry it, so that only an email sent while the student was
// away becomes a notification.
func (s *emailService) deliver(ctx context.Context, id string, scheduled bool) (*db.Email, *models.ApiError) {
	e, err := s.Repo.Claim(ctx, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return nil, apiError(http.StatusConflict, "email is not pending")
	}
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	fail := func(apiErr *models.ApiError, cause error) (*db.Email, *models.ApiError) {
		utils.Logger.Logger.Warnw("email delivery failed", "email_id", e.ID, "error", cause)
		_ = s.Repo.Finish(ctx, e.ID, db.EmailFailed, nil)
		s.publish(ctx, core.EventEmailFailed, e, scheduled)
		e.Status = db.EmailFailed
		return e, apiErr
	}

	accessToken, from, apiErr := s.Gmail.Credentials(ctx, e.UserID)
	if apiErr != nil {
		return fail(apiErr, errors.New(apiErr.Error.Error()))
	}
	pixel := ""
	if trackingID, path, err := s.Tracker.Register(ctx, e.ID, e.UserID); err == nil && trackingID != "" {
		// The pixel goes in even when the id cannot be stored here: the tracker counts the opens on its own.
		pixel = strings.TrimRight(s.Config.TrackerPublicURL, "/") + path
		if err := s.Repo.SetTracking(ctx, e.ID, trackingID); err == nil {
			e.TrackingID = trackingID
		} else {
			utils.Logger.Logger.Warnw("email: tracking id not stored", "email_id", e.ID, "error", err)
		}
	} else if err != nil {
		utils.Logger.Logger.Warnw("email: tracking unavailable, sending without pixel", "error", err)
	}
	msg := core.MailMessage{From: from, To: e.Recipient, Subject: e.Subject, HTML: renderHTML(e.Body, pixel)}
	if e.ResumeID != nil {
		// An application that says « CV attached » never leaves without it: better a failed send the student
		// sees and retries than a recruiter who receives no CV.
		att, err := s.attachment(ctx, e)
		if err != nil || att == nil {
			if err == nil {
				err = errors.New("resume has no file to attach")
			}
			return fail(apiError(http.StatusBadGateway, "the CV could not be attached"), err)
		}
		msg.Attachments = append(msg.Attachments, *att)
	}
	if e.AttachLetter {
		// Same rule for the letter the student asked to attach.
		letter, err := s.letter(ctx, e, from)
		if err != nil {
			return fail(apiError(http.StatusBadGateway, "the cover letter could not be attached"), err)
		}
		msg.Attachments = append(msg.Attachments, *letter)
	}
	if err := s.Mailer.Send(ctx, accessToken, msg); err != nil {
		if errors.Is(err, core.ErrGmailAuth) {
			return fail(apiError(http.StatusConflict, "gmail access was revoked, reconnect gmail"), err)
		}
		if errors.Is(err, core.ErrMailTooLarge) {
			return fail(apiError(http.StatusUnprocessableEntity, "the attachments are larger than Gmail allows (25 MB)"), err)
		}
		return fail(apiError(http.StatusBadGateway, "gmail could not send the email"), err)
	}
	sentAt := s.Now().UTC()
	if err := s.Repo.Finish(ctx, e.ID, db.EmailSent, &sentAt); err != nil {
		utils.Logger.Logger.Warnw("email: sent but status not stored", "email_id", e.ID, "error", err)
	}
	s.publish(ctx, core.EventEmailSent, e, scheduled)
	// The first email a student ever sends is also the step « first application » of the statistics. A failed
	// lookup only loses that figure, never the send.
	if other, err := s.Repo.SentAnother(ctx, e.UserID, e.ID); err == nil && !other {
		if err := s.Events.Publish(ctx, core.EventApplicationFirstSent, map[string]any{"user_id": e.UserID}); err != nil {
			utils.Logger.Logger.Warnw("publish event", "event", core.EventApplicationFirstSent, "error", err)
		}
	}
	e.Status, e.SentAt = db.EmailSent, &sentAt
	return e, nil
}

// attachment is the CV an email carries: the file the student imported for an application without a
// subscription, else the CV as it is now in 9itchi. Nil when there is none.
func (s *emailService) attachment(ctx context.Context, e *db.Email) (*core.Attachment, error) {
	if e.CVOriginal {
		return s.Resumes.Original(ctx, *e.ResumeID)
	}
	pdf, err := s.Resumes.PDF(ctx, *e.ResumeID)
	if err != nil || pdf == nil || len(pdf.Data) == 0 {
		return nil, err
	}
	if pdf.Filename == "" {
		pdf.Filename = "CV.pdf"
	}
	return pdf, nil
}

// letter is the cover letter of an email as a PDF, rendered now by resume-api from the email as it leaves, signed
// with the CV's contact block and the Gmail address it leaves from. Its name is the one the student gave, else the
// default one of its language.
func (s *emailService) letter(ctx context.Context, e *db.Email, from string) (*core.Attachment, error) {
	asked := ""
	if e.LetterLanguage != nil {
		asked = *e.LetterLanguage
	}
	language := letterLanguage(asked, e.Body)
	in := core.LetterRequest{Language: language, Subject: e.Subject, Body: e.Body, SenderEmail: from}
	if e.ResumeID != nil {
		in.ResumeID = *e.ResumeID
	}
	data, err := s.Resumes.LetterPDF(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty letter")
	}
	stem := ""
	if e.LetterFilename != nil {
		stem = *e.LetterFilename
	}
	if stem == "" {
		stem = s.defaultLetterName(ctx, e, language)
	}
	return &core.Attachment{Filename: stem + ".pdf", ContentType: "application/pdf", Data: data}, nil
}

// defaultLetterName is the default name of the letter with the name on the CV; without a CV, or when it cannot be
// read, the prefix of the language alone.
func (s *emailService) defaultLetterName(ctx context.Context, e *db.Email, language string) string {
	if e.ResumeID == nil {
		return defaultLetterStem(language, "")
	}
	token, err := s.bearer(e.UserID)
	if err != nil {
		return defaultLetterStem(language, "")
	}
	names, err := s.Resumes.Names(ctx, token, *e.ResumeID)
	if err != nil || names == nil {
		utils.Logger.Logger.Warnw("email: CV name unavailable, generic letter name", "email_id", e.ID, "error", err)
		return defaultLetterStem(language, "")
	}
	return defaultLetterStem(language, names.ContactName)
}

// renderHTML turns the plain-text body into HTML (escaped) and appends the tracking pixel.
func renderHTML(body, pixel string) string {
	var b strings.Builder
	b.WriteString("<div style=\"font-family:Arial,sans-serif;font-size:14px;line-height:1.5\">")
	b.WriteString(strings.ReplaceAll(html.EscapeString(strings.ReplaceAll(body, "\r\n", "\n")), "\n", "<br>"))
	b.WriteString("</div>")
	if pixel != "" {
		if u, err := url.Parse(pixel); err == nil {
			fmt.Fprintf(&b, "<img src=\"%s\" width=\"1\" height=\"1\" alt=\"\" style=\"display:none\">", html.EscapeString(u.String()))
		}
	}
	return b.String()
}

// publish raises an event about an email. A failure is logged and never changes what happened to the email.
func (s *emailService) publish(ctx context.Context, event string, e *db.Email, scheduled bool) {
	payload := map[string]any{"email_id": e.ID, "user_id": e.UserID, "scheduled": scheduled}
	if err := s.Events.Publish(ctx, event, payload); err != nil {
		utils.Logger.Logger.Warnw("publish event", "event", event, "email_id", e.ID, "error", err)
	}
}

// UpdateEmail edits a scheduled email. The recipient of an offer that takes applications at its own address stays
// that address. A new time gets its schedule task first; the email changes only once it has one, and the task of
// the old time is then dropped (were it to run anyway, the dispatch finds the email not due and does nothing).
func (s *emailService) UpdateEmail(ctx context.Context, userID, id string, req api.UpdateEmailRequest) (*db.Email, *models.ApiError) {
	e, err := s.Repo.Get(ctx, userID, id)
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	if e.Status != db.EmailScheduled {
		return nil, apiError(http.StatusConflict, "only a scheduled email can be edited")
	}
	edit := db.EmailEdit{
		Recipient: e.Recipient, Subject: e.Subject, Body: e.Body, ResumeID: e.ResumeID,
		ScheduledAt: e.ScheduledAt, ScheduleTaskID: e.ScheduleTaskID,
		LetterFilename: e.LetterFilename, AttachLetter: e.AttachLetter, LetterLanguage: e.LetterLanguage,
	}

	if req.To != nil && !strings.EqualFold(strings.TrimSpace(*req.To), e.Recipient) {
		if e.JobID != nil {
			job, err := s.Jobs.ApplyInfo(ctx, *e.JobID)
			if err != nil && !errors.Is(err, core.ErrPeerNotFound) {
				return nil, peerError(err, "job not found")
			}
			if err == nil && job.ApplyEmail != "" {
				return nil, apiError(http.StatusConflict, "this offer takes applications at its own address")
			}
		}
		edit.Recipient = strings.TrimSpace(*req.To)
	}
	if req.Subject != nil {
		if edit.Subject = strings.TrimSpace(*req.Subject); edit.Subject == "" {
			return nil, apiError(http.StatusBadRequest, "subject cannot be empty")
		}
	}
	if req.Body != nil {
		if strings.TrimSpace(*req.Body) == "" {
			return nil, apiError(http.StatusBadRequest, "body cannot be empty")
		}
		edit.Body = *req.Body
	}

	attach := req.AttachCV
	if attach == nil && req.ResumeID != nil {
		yes := true
		attach = &yes
	}
	if attach != nil {
		if !*attach {
			edit.ResumeID = nil
		} else {
			resumeID := e.ResumeID
			if req.ResumeID != nil {
				resumeID = req.ResumeID
			}
			if resumeID == nil {
				return nil, apiError(http.StatusBadRequest, "attach_cv needs a resume_id")
			}
			changed := e.ResumeID == nil || !strings.EqualFold(*e.ResumeID, *resumeID)
			if e.CVOriginal && changed {
				if apiErr := s.checkAttachable(ctx, *resumeID); apiErr != nil {
					return nil, apiErr
				}
			}
			edit.ResumeID = resumeID
		}
	}

	if req.LetterFilename != nil {
		edit.LetterFilename = optional(cleanStem(*req.LetterFilename, ""))
	}
	if req.AttachLetter != nil {
		edit.AttachLetter = *req.AttachLetter
	}
	if req.Language != nil {
		edit.LetterLanguage = optional(*req.Language)
	}
	letterChanged := req.LetterFilename != nil || req.AttachLetter != nil || attach != nil
	if letterChanged && edit.AttachLetter && edit.LetterFilename != nil && edit.ResumeID != nil {
		if apiErr := s.checkLetterName(ctx, userID, *edit.ResumeID, *edit.LetterFilename); apiErr != nil {
			return nil, apiErr
		}
	}

	var newTask *string
	if req.ScheduledAt != nil && (e.ScheduledAt == nil || !req.ScheduledAt.Equal(*e.ScheduledAt)) {
		at := req.ScheduledAt.UTC()
		now := s.Now()
		if at.Before(now.Add(minScheduleDelay)) || at.After(now.Add(maxScheduleAhead)) {
			return nil, apiError(http.StatusBadRequest, "scheduled_at must be between one minute and one year from now")
		}
		taskID, err := s.Scheduler.ScheduleEmail(ctx, e.ID, at)
		if err != nil {
			return nil, peerError(err, "schedule not found")
		}
		newTask = &taskID
		edit.ScheduledAt, edit.ScheduleTaskID = &at, newTask
	}

	out, err := s.Repo.UpdateScheduled(ctx, userID, id, edit)
	if err != nil {
		if newTask != nil {
			if err := s.Scheduler.Cancel(ctx, *newTask); err != nil {
				utils.Logger.Logger.Warnw("email: could not cancel schedule task", "error", err)
			}
		}
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, apiError(http.StatusConflict, "only a scheduled email can be edited")
		}
		return nil, repoError(err, "email not found")
	}
	if newTask != nil && e.ScheduleTaskID != nil {
		// The dispatch of the old time finds the email not due, so a failed cancel is only logged.
		if err := s.Scheduler.Cancel(ctx, *e.ScheduleTaskID); err != nil {
			utils.Logger.Logger.Warnw("email: could not cancel schedule task", "error", err)
		}
	}
	return out, nil
}
