package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

type usageQuotas struct {
	limits map[string]int
	err    error
}

func (q usageQuotas) Quotas(context.Context, string) (map[string]int, error) { return q.limits, q.err }

type usageEmail struct {
	EmailService
	got   string
	allow *api.AllowanceResponse
}

func (e *usageEmail) Allowance(_ context.Context, id string) (*api.AllowanceResponse, *models.ApiError) {
	e.got = id
	return e.allow, nil
}

type usageRecruiter struct {
	RecruiterService
	allow *api.RecruiterAllowance
}

func (r usageRecruiter) Allowance(context.Context, string) (*api.RecruiterAllowance, *models.ApiError) {
	return r.allow, nil
}

type usageDrafts struct {
	repositories.DraftRepository
	since time.Time
	err   error
}

func (d *usageDrafts) CountSince(_ context.Context, _ string, since time.Time) (int, error) {
	d.since = since
	return 4, d.err
}

type usageMatches struct {
	repositories.MatchRepository
	since map[string]time.Time
}

func (m *usageMatches) CountSince(_ context.Context, _, origin string, since time.Time) (int, error) {
	m.since[origin] = since
	return map[string]int{db.MatchOriginAnalysis: 2, db.MatchOriginScore: 7}[origin], nil
}

func TestUserUsageForTheTeam(t *testing.T) {
	const student = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	ten, six := 10, 6
	email := &usageEmail{allow: &api.AllowanceResponse{Mode: AllowPlan, Limit: &ten, Used: 3}}
	drafts, matches := &usageDrafts{}, &usageMatches{since: map[string]time.Time{}}
	// 23:30 UTC on 30 Nov. is already 1 Dec. in Tunis (UTC+1): the month is UTC, the day is Tunis.
	now := time.Date(2026, 11, 30, 23, 30, 0, 0, time.UTC)
	svc := NewUsageService(UsageDeps{
		Email: email, Recruiter: usageRecruiter{allow: &api.RecruiterAllowance{Limit: &six, Used: 5}},
		Drafts: drafts, Matches: matches, Now: func() time.Time { return now },
		Quotas: usageQuotas{limits: map[string]int{"ai_drafts_per_month": 5, "ai_matches_per_month": -1}},
	})

	res, apiErr := svc.UserUsage(context.Background(), strings.ToUpper(student))
	require.Nil(t, apiErr)
	five := 5
	assert.Equal(t, student, email.got, "the id is lower-cased")
	assert.Equal(t, &api.UserUsageResponse{
		Applications:      api.AllowanceResponse{Mode: AllowPlan, Limit: &ten, Used: 3},
		AIDrafts:          api.UsageItem{Limit: &five, Used: 4},
		AIMatches:         api.UsageItem{Used: 2},
		AIScores:          api.UsageItem{Used: 7},
		RecruiterSearches: api.UsageItem{Limit: &six, Used: 5},
	}, res, "a negative or absent limit is no limit")
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), drafts.since)
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), matches.since[db.MatchOriginAnalysis])
	day := matches.since[db.MatchOriginScore]
	assert.True(t, day.Equal(time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)), "scores count from the start of the Tunis day, got %s", day)

	_, apiErr = svc.UserUsage(context.Background(), "nope")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)

	drafts.err = errors.New("db down")
	_, apiErr = svc.UserUsage(context.Background(), student)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
}
