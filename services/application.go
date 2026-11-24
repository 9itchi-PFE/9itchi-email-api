package services

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

// ApplicationService holds all business logic for application. Handlers stay thin.
type ApplicationService interface {
	// POST /applications: one-click apply: prefilled email + chosen resume
	Apply(ctx context.Context, userID string, req api.ApplyRequest) (*api.ApplicationResponse, *models.ApiError)
	// GET /applications: tracked applications
	ListApplications(ctx context.Context, userID string) (*api.ApplicationsResponse, *models.ApiError)
	// PATCH /applications/:id: what the student says happened (replied, withdrawn, closed, back to sent)
	UpdateApplication(ctx context.Context, userID, id string, req api.UpdateApplicationRequest) (*api.ApplicationResponse, *models.ApiError)
	// POST /applications/:id/send: send a scheduled application now
	SendNow(ctx context.Context, userID, id string) (*api.ApplicationResponse, *models.ApiError)
	// GET /internal/emails/:id/context: the offer an email was sent for (peer services)
	EmailContext(ctx context.Context, emailID string) (*api.EmailContextResponse, *models.ApiError)
}

type applicationService struct {
	repo   repositories.ApplicationRepository
	emails EmailService
	jobs   core.JobSource
}

func NewApplicationService(repo repositories.ApplicationRepository, emails EmailService, jobs core.JobSource) ApplicationService {
	return &applicationService{repo: repo, emails: emails, jobs: jobs}
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// StatusFailed is only a shown status: the application was scheduled and its email could not leave.
const StatusFailed = "failed"

func briefOf(e *db.Email) *api.EmailBrief {
	return &api.EmailBrief{ID: e.ID, Recipient: e.Recipient, Subject: e.Subject, Status: e.Status, ScheduledAt: e.ScheduledAt, SentAt: e.SentAt}
}

// shownStatus is the status of an application as its email stands now: a scheduled application follows its
// email (sent by the dispatcher, failed, or cancelled).
func shownStatus(a *db.Application, e *db.Email) string {
	if a.Status != db.AppScheduled || a.Via != db.ViaGmail {
		return a.Status
	}
	if e == nil {
		return StatusFailed
	}
	switch e.Status {
	case db.EmailSent:
		return db.AppSent
	case db.EmailFailed:
		return StatusFailed
	case db.EmailCancelled:
		return db.AppWithdrawn
	}
	return db.AppScheduled
}

func toResponse(a *db.Application, e *db.Email) *api.ApplicationResponse {
	res := &api.ApplicationResponse{
		ID: a.ID, JobID: a.JobID, EmailID: a.EmailID, Status: shownStatus(a, e), JobTitle: a.JobTitle, Company: a.Company, Via: a.Via,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if e != nil {
		res.Email = briefOf(e)
	}
	return res
}

// emailOf loads the email of an application; nil when it has none or the email is gone.
func (s *applicationService) emailOf(ctx context.Context, a *db.Application) (*db.Email, *models.ApiError) {
	if a.EmailID == nil {
		return nil, nil
	}
	e, apiErr := s.emails.GetEmail(ctx, a.UserID, *a.EmailID)
	if apiErr != nil {
		if apiErr.Code == http.StatusNotFound {
			return nil, nil
		}
		return nil, apiErr
	}
	return e, nil
}

// active says whether the student already has a live application to the offer: waiting to leave, sent, or answered.
func active(status string) bool {
	switch status {
	case db.AppScheduled, db.AppSent, db.AppOpened, db.AppReplied:
		return true
	}
	return false
}

func (s *applicationService) Apply(ctx context.Context, userID string, req api.ApplyRequest) (*api.ApplicationResponse, *models.ApiError) {
	job, err := s.jobs.ApplyInfo(ctx, req.JobID)
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	if !job.Open {
		return nil, apiError(http.StatusConflict, "this offer is closed")
	}
	// One live application per offer: a second email to the same recruiter helps nobody.
	old, err := s.repo.GetByJob(ctx, userID, req.JobID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
	case err != nil:
		return nil, repoError(err, "application not found")
	default:
		e, apiErr := s.emailOf(ctx, old)
		if apiErr != nil {
			return nil, apiErr
		}
		if st := shownStatus(old, e); st == db.AppScheduled {
			return nil, apiError(http.StatusConflict, "an application to this offer is already scheduled")
		} else if active(st) {
			return nil, apiError(http.StatusConflict, "you already applied to this offer")
		}
	}

	app := &db.Application{UserID: userID, JobID: req.JobID, Status: db.AppSent, JobTitle: job.Title, Company: job.Company, Via: db.ViaSelf}
	var email *db.Email
	if req.Via != db.ViaSelf {
		to := job.ApplyEmail
		if to == "" {
			to = req.To
		}
		if to == "" {
			return nil, apiError(http.StatusUnprocessableEntity, "this offer does not take applications by email")
		}
		if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" {
			return nil, apiError(http.StatusBadRequest, "subject and body are required")
		}
		send := api.SendEmailRequest{JobID: req.JobID, ResumeID: req.ResumeID, To: to, Subject: req.Subject, Body: req.Body}
		var apiErr *models.ApiError
		if req.ScheduledAt != nil {
			email, apiErr = s.emails.ScheduleEmail(ctx, userID, api.ScheduleEmailRequest{SendEmailRequest: send, ScheduledAt: *req.ScheduledAt})
		} else {
			email, apiErr = s.emails.SendEmail(ctx, userID, send)
		}
		if email == nil || email.Status == db.EmailFailed {
			if apiErr == nil {
				apiErr = apiError(http.StatusBadGateway, "the email could not be sent")
			}
			return nil, apiErr
		}
		app.Via, app.EmailID = db.ViaGmail, &email.ID
		if email.Status == db.EmailScheduled {
			app.Status = db.AppScheduled
		}
	}
	saved, err := s.repo.Upsert(ctx, app)
	if err != nil {
		return nil, repoError(err, "application not found")
	}
	return toResponse(saved, email), nil
}

func (s *applicationService) ListApplications(ctx context.Context, userID string) (*api.ApplicationsResponse, *models.ApiError) {
	apps, err := s.repo.ListByUser(ctx, userID, listLimit)
	if err != nil {
		return nil, repoError(err, "application not found")
	}
	emails, apiErr := s.emails.ListEmails(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	byID := make(map[string]*db.Email, len(emails))
	for i := range emails {
		byID[emails[i].ID] = &emails[i]
	}
	out := &api.ApplicationsResponse{Items: make([]api.ApplicationResponse, 0, len(apps))}
	for i := range apps {
		var e *db.Email
		if apps[i].EmailID != nil {
			e = byID[*apps[i].EmailID]
		}
		out.Items = append(out.Items, *toResponse(&apps[i], e))
	}
	return out, nil
}

// load returns one application of the user with its email.
func (s *applicationService) load(ctx context.Context, userID, id string) (*db.Application, *db.Email, *models.ApiError) {
	if !uuidRe.MatchString(id) {
		return nil, nil, apiError(http.StatusBadRequest, "invalid application id")
	}
	a, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, nil, repoError(err, "application not found")
	}
	e, apiErr := s.emailOf(ctx, a)
	if apiErr != nil {
		return nil, nil, apiErr
	}
	return a, e, nil
}

func (s *applicationService) UpdateApplication(ctx context.Context, userID, id string, req api.UpdateApplicationRequest) (*api.ApplicationResponse, *models.ApiError) {
	a, e, apiErr := s.load(ctx, userID, id)
	if apiErr != nil {
		return nil, apiErr
	}
	if shownStatus(a, e) == db.AppScheduled {
		// Nothing has left yet: the only thing the student can do is give up, which cancels the email.
		if req.Status != db.AppWithdrawn {
			return nil, apiError(http.StatusConflict, "this application has not been sent yet")
		}
		if _, apiErr := s.emails.CancelEmail(ctx, userID, e.ID); apiErr != nil {
			return nil, apiErr
		}
		e.Status = db.EmailCancelled
	}
	saved, err := s.repo.SetStatus(ctx, userID, id, req.Status)
	if err != nil {
		return nil, repoError(err, "application not found")
	}
	return toResponse(saved, e), nil
}

func (s *applicationService) SendNow(ctx context.Context, userID, id string) (*api.ApplicationResponse, *models.ApiError) {
	a, e, apiErr := s.load(ctx, userID, id)
	if apiErr != nil {
		return nil, apiErr
	}
	if shownStatus(a, e) != db.AppScheduled {
		return nil, apiError(http.StatusConflict, "only a scheduled application can be sent now")
	}
	sent, apiErr := s.emails.SendNow(ctx, userID, e.ID)
	if apiErr != nil {
		return nil, apiErr
	}
	saved, err := s.repo.SetStatus(ctx, userID, id, db.AppSent)
	if err != nil {
		return nil, repoError(err, "application not found")
	}
	return toResponse(saved, sent), nil
}

func (s *applicationService) EmailContext(ctx context.Context, emailID string) (*api.EmailContextResponse, *models.ApiError) {
	if !uuidRe.MatchString(emailID) {
		return nil, apiError(http.StatusBadRequest, "invalid email id")
	}
	a, err := s.repo.GetByEmail(ctx, emailID)
	if err != nil {
		return nil, repoError(err, "no application was sent with this email")
	}
	return &api.EmailContextResponse{EmailID: emailID, UserID: a.UserID, ApplicationID: a.ID, JobID: a.JobID, JobTitle: a.JobTitle, Company: a.Company}, nil
}
