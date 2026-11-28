package services

import (
	"context"
	"net/http"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

// UsageService tells the team how much of the plan a student used here. Handlers stay thin.
type UsageService interface {
	// GET /admin/users/:id/usage: applications, AI letters, analyses and scores, recruiter searches
	UserUsage(ctx context.Context, id string) (*api.UserUsageResponse, *models.ApiError)
}

// UsageDeps are the collaborators of the usage service (all mockable). Applications and recruiter searches go
// through the services that enforce them, so the team sees what the student is held to.
type UsageDeps struct {
	Email     EmailService
	Recruiter RecruiterService
	Drafts    repositories.DraftRepository
	Matches   repositories.MatchRepository
	Quotas    core.QuotaSource
	Now       func() time.Time
}

type usageService struct {
	UsageDeps
}

func NewUsageService(d UsageDeps) UsageService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &usageService{d}
}

// usageItem is the plan's limit for key (nil: none, absent or negative) next to what count says was used.
func usageItem(quotas map[string]int, key string, count func() (int, error)) (api.UsageItem, error) {
	n, err := count()
	if err != nil {
		return api.UsageItem{}, err
	}
	item := api.UsageItem{Used: n}
	if limit, ok := quotas[key]; ok && limit >= 0 {
		item.Limit = &limit
	}
	return item, nil
}

func (s *usageService) UserUsage(ctx context.Context, id string) (*api.UserUsageResponse, *models.ApiError) {
	if !uuidRe.MatchString(id) {
		return nil, apiError(http.StatusBadRequest, "invalid user id")
	}
	id = strings.ToLower(id)
	quotas, err := s.Quotas.Quotas(ctx, id)
	if err != nil {
		return nil, peerError(err, "plan not found")
	}
	apps, apiErr := s.Email.Allowance(ctx, id)
	if apiErr != nil {
		return nil, apiErr
	}
	searches, apiErr := s.Recruiter.Allowance(ctx, id)
	if apiErr != nil {
		return nil, apiErr
	}
	now := s.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	day, _ := tunisDayOf(s.Now())
	out := &api.UserUsageResponse{
		Applications:      *apps,
		RecruiterSearches: api.UsageItem{Limit: searches.Limit, Used: searches.Used},
	}
	if out.AIDrafts, err = usageItem(quotas, core.QuotaAIDrafts, func() (int, error) { return s.Drafts.CountSince(ctx, id, month) }); err != nil {
		return nil, repoError(err, "not found")
	}
	if out.AIMatches, err = usageItem(quotas, core.QuotaAIMatches, func() (int, error) { return s.Matches.CountSince(ctx, id, db.MatchOriginAnalysis, month) }); err != nil {
		return nil, repoError(err, "not found")
	}
	if out.AIScores, err = usageItem(quotas, core.QuotaAIScores, func() (int, error) { return s.Matches.CountSince(ctx, id, db.MatchOriginScore, day) }); err != nil {
		return nil, repoError(err, "not found")
	}
	return out, nil
}
