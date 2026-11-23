package services

import (
	"context"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/repositories"
)

// ApplicationService holds all business logic for application. Handlers stay thin.
type ApplicationService interface {
	// POST /applications: one-click apply: prefilled email + chosen resume
	Apply(ctx context.Context, userID string, req api.ApplyRequest) (any, *models.ApiError)
	// GET /applications: tracked applications
	ListApplications(ctx context.Context, userID string) (any, *models.ApiError)
}

type applicationService struct {
	repo repositories.ApplicationRepository
}

func NewApplicationService(repo repositories.ApplicationRepository) ApplicationService {
	return &applicationService{repo: repo}
}

func (s *applicationService) Apply(_ context.Context, _ string, _ api.ApplyRequest) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}

func (s *applicationService) ListApplications(_ context.Context, _ string) (any, *models.ApiError) {
	return nil, models.NotImplemented()
}
