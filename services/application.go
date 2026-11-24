package services

import (
	"context"
	"net/http"

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
}

type applicationService struct {
	repo   repositories.ApplicationRepository
	emails EmailService
}

func NewApplicationService(repo repositories.ApplicationRepository, emails EmailService) ApplicationService {
	return &applicationService{repo: repo, emails: emails}
}

func briefOf(e *db.Email) *api.EmailBrief {
	return &api.EmailBrief{ID: e.ID, Recipient: e.Recipient, Subject: e.Subject, Status: e.Status, ScheduledAt: e.ScheduledAt, SentAt: e.SentAt}
}

func (s *applicationService) Apply(ctx context.Context, userID string, req api.ApplyRequest) (*api.ApplicationResponse, *models.ApiError) {
	send := api.SendEmailRequest{JobID: req.JobID, ResumeID: req.ResumeID, To: req.To, Subject: req.Subject, Body: req.Body}
	var (
		e      *db.Email
		apiErr *models.ApiError
	)
	if req.ScheduledAt != nil {
		e, apiErr = s.emails.ScheduleEmail(ctx, userID, api.ScheduleEmailRequest{SendEmailRequest: send, ScheduledAt: *req.ScheduledAt})
	} else {
		e, apiErr = s.emails.SendEmail(ctx, userID, send)
	}
	if apiErr != nil {
		return nil, apiErr
	}
	status := "sent"
	if e.Status == db.EmailScheduled {
		status = "scheduled"
	}
	if e.Status == db.EmailFailed {
		return nil, apiError(http.StatusBadGateway, "the email could not be sent")
	}
	app, err := s.repo.Upsert(ctx, &db.Application{UserID: userID, JobID: req.JobID, EmailID: &e.ID, Status: status})
	if err != nil {
		return nil, repoError(err, "application not found")
	}
	return &api.ApplicationResponse{
		ID: app.ID, JobID: app.JobID, EmailID: app.EmailID, Status: app.Status, Email: briefOf(e),
		CreatedAt: app.CreatedAt, UpdatedAt: app.UpdatedAt,
	}, nil
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
	for _, a := range apps {
		item := api.ApplicationResponse{ID: a.ID, JobID: a.JobID, EmailID: a.EmailID, Status: a.Status, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
		if a.EmailID != nil {
			if e, ok := byID[*a.EmailID]; ok {
				item.Email = briefOf(e)
				if e.Status == db.EmailSent && a.Status == "scheduled" {
					item.Status = "sent"
				}
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
