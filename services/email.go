package services

import (
	"context"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/repositories"
)

// EmailService holds all business logic for email. Handlers stay thin.
type EmailService interface {
	// POST /emails/draft: AI draft for a job + resume
	DraftEmail(ctx context.Context, userID string, req api.DraftEmailRequest) (any, *models.ApiError)
	// POST /emails: send immediately via Gmail
	SendEmail(ctx context.Context, userID string, req api.SendEmailRequest) (any, *models.ApiError)
	// POST /emails/schedule: schedule for date/time
	ScheduleEmail(ctx context.Context, userID string, req api.ScheduleEmailRequest) (any, *models.ApiError)
	// GET /emails: history: draft, scheduled, sent, opened, failed
	ListEmails(ctx context.Context, userID string) (any, *models.ApiError)
	// GET /emails/:id: email detail
	GetEmail(ctx context.Context, userID string, id string) (any, *models.ApiError)
	// DELETE /emails/:id: cancel a scheduled email
	CancelEmail(ctx context.Context, userID string, id string) (any, *models.ApiError)
	// POST /internal/emails/:id/dispatch: called by schedule-api when due
	DispatchScheduled(ctx context.Context, id string) (any, *models.ApiError)
}

type emailService struct {
	repo repositories.EmailRepository
}

func NewEmailService(repo repositories.EmailRepository) EmailService {
	return &emailService{repo: repo}
}

func (s *emailService) DraftEmail(_ context.Context, _ string, _ api.DraftEmailRequest) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) SendEmail(_ context.Context, _ string, _ api.SendEmailRequest) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) ScheduleEmail(_ context.Context, _ string, _ api.ScheduleEmailRequest) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) ListEmails(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) GetEmail(_ context.Context, _ string, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) CancelEmail(_ context.Context, _ string, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *emailService) DispatchScheduled(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}
