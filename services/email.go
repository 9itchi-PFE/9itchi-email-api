package services

import (
	"context"
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
	// POST /internal/emails/:id/dispatch: called by schedule-api when due
	DispatchScheduled(ctx context.Context, id string) (*db.Email, *models.ApiError)
}

// EmailDeps are the collaborators of the email service (all mockable).
type EmailDeps struct {
	Repo      repositories.EmailRepository
	Gmail     GmailService
	Mailer    core.Mailer
	Drafter   core.Drafter
	Jobs      core.JobSource
	Resumes   core.ResumeSource
	Tracker   core.Tracker
	Scheduler core.Scheduler
	Quotas    core.QuotaSource
	Events    core.Publisher
	Config    utils.AppConfig
	Now       func() time.Time
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
	token, err := s.bearer(userID)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "internal error")
	}
	job, err := s.Jobs.Job(ctx, token, req.JobID)
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	summary := ""
	if req.ResumeID != "" {
		if summary, err = s.Resumes.Summary(ctx, token, req.ResumeID); err != nil {
			return nil, peerError(err, "resume not found")
		}
	}
	subject, body, err := s.Drafter.Draft(ctx, token, core.DraftInput{
		JobTitle: job.Title, Company: job.Company, JobDescription: job.Description,
		CandidateSummary: summary, Language: req.Language, Tone: req.Tone,
	})
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	return &api.DraftEmailResponse{Subject: subject, Body: body}, nil
}

// checkQuota enforces the plan's monthly email limit (negative = unlimited).
func (s *emailService) checkQuota(ctx context.Context, userID string) *models.ApiError {
	limit, ok, err := s.Quotas.Limit(ctx, userID, core.QuotaEmailsPerMonth)
	if err != nil {
		return peerError(err, "plan not found")
	}
	if !ok || limit < 0 {
		return nil
	}
	now := s.Now().UTC()
	n, err := s.Repo.CountSentSince(ctx, userID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return repoError(err, "email not found")
	}
	if n >= limit {
		return apiError(http.StatusPaymentRequired, "monthly email limit of your plan reached")
	}
	return nil
}

func (s *emailService) create(ctx context.Context, userID string, req api.SendEmailRequest, status string, at *time.Time) (*db.Email, *models.ApiError) {
	if _, _, apiErr := s.Gmail.Credentials(ctx, userID); apiErr != nil {
		return nil, apiErr
	}
	if apiErr := s.checkQuota(ctx, userID); apiErr != nil {
		return nil, apiErr
	}
	e, err := s.Repo.Create(ctx, &db.Email{
		UserID: userID, JobID: optional(req.JobID), ResumeID: optional(req.ResumeID), Recipient: req.To,
		Subject: strings.TrimSpace(req.Subject), Body: req.Body, Status: status, ScheduledAt: at,
	})
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return e, nil
}

func (s *emailService) SendEmail(ctx context.Context, userID string, req api.SendEmailRequest) (*db.Email, *models.ApiError) {
	e, apiErr := s.create(ctx, userID, req, db.EmailDraft, nil)
	if apiErr != nil {
		return nil, apiErr
	}
	return s.deliver(ctx, e.ID)
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
	out, apiErr := s.deliver(ctx, id)
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
	out, apiErr := s.deliver(ctx, id)
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
func (s *emailService) deliver(ctx context.Context, id string) (*db.Email, *models.ApiError) {
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
		_ = s.Events.Publish(ctx, core.EventEmailFailed, map[string]string{"email_id": e.ID, "user_id": e.UserID})
		e.Status = db.EmailFailed
		return e, apiErr
	}

	accessToken, from, apiErr := s.Gmail.Credentials(ctx, e.UserID)
	if apiErr != nil {
		return fail(apiErr, errors.New(apiErr.Error.Error()))
	}
	pixel := ""
	if trackingID, path, err := s.Tracker.Register(ctx, e.ID, e.UserID); err == nil && trackingID != "" {
		if err := s.Repo.SetTracking(ctx, e.ID, trackingID); err == nil {
			e.TrackingID = trackingID
			pixel = strings.TrimRight(s.Config.TrackerPublicURL, "/") + path
		}
	} else if err != nil {
		utils.Logger.Logger.Warnw("email: tracking unavailable, sending without pixel", "error", err)
	}
	msg := core.MailMessage{From: from, To: e.Recipient, Subject: e.Subject, HTML: renderHTML(e.Body, pixel)}
	if e.ResumeID != nil {
		// An application that says « CV attached » never leaves without it: better a failed send the student
		// sees and retries than a recruiter who receives no CV.
		pdf, err := s.Resumes.PDF(ctx, *e.ResumeID)
		if err != nil || len(pdf) == 0 {
			if err == nil {
				err = errors.New("resume has no pdf")
			}
			return fail(apiError(http.StatusBadGateway, "the CV could not be attached"), err)
		}
		msg.Attachment = &core.Attachment{Filename: "CV.pdf", Data: pdf}
	}
	if err := s.Mailer.Send(ctx, accessToken, msg); err != nil {
		if errors.Is(err, core.ErrGmailAuth) {
			return fail(apiError(http.StatusConflict, "gmail access was revoked, reconnect gmail"), err)
		}
		return fail(apiError(http.StatusBadGateway, "gmail could not send the email"), err)
	}
	sentAt := s.Now().UTC()
	if err := s.Repo.Finish(ctx, e.ID, db.EmailSent, &sentAt); err != nil {
		utils.Logger.Logger.Warnw("email: sent but status not stored", "email_id", e.ID, "error", err)
	}
	_ = s.Events.Publish(ctx, core.EventEmailSent, map[string]string{"email_id": e.ID, "user_id": e.UserID})
	e.Status, e.SentAt = db.EmailSent, &sentAt
	return e, nil
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
