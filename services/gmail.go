package services

import (
	"context"

	"9itchi-email-api/models"
	"9itchi-email-api/repositories"
)

// GmailService holds all business logic for gmail. Handlers stay thin.
type GmailService interface {
	// GET /gmail/connect: start Gmail OAuth2 (send-only scope)
	ConnectGmail(ctx context.Context, userID string) (any, *models.ApiError)
	// GET /gmail/callback: finish OAuth2, store AES-GCM encrypted token
	GmailCallback(ctx context.Context) (any, *models.ApiError)
	// GET /gmail/status: connection state
	GmailStatus(ctx context.Context, userID string) (any, *models.ApiError)
	// DELETE /gmail/connection: revoke access and delete token
	RevokeGmail(ctx context.Context, userID string) (any, *models.ApiError)
}

type gmailService struct {
	repo repositories.GmailRepository
}

func NewGmailService(repo repositories.GmailRepository) GmailService {
	return &gmailService{repo: repo}
}

func (s *gmailService) ConnectGmail(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *gmailService) GmailCallback(_ context.Context) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *gmailService) GmailStatus(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *gmailService) RevokeGmail(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}
