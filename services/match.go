package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

// MatchService tells a student how well their CV fits an offer, with the AI, before they apply.
type MatchService interface {
	// POST /applications/match: the reading of the CV against the offer, made now unless it was already made
	Match(ctx context.Context, userID string, req api.MatchRequest) (*api.MatchResponse, *models.ApiError)
	// GET /applications/match: the reading already made for this CV and offer (404 when none: nothing is asked)
	Find(ctx context.Context, userID string, req api.MatchRequest) (*api.MatchResponse, *models.ApiError)
}

// MatchDeps are the collaborators of the match service (all mockable).
type MatchDeps struct {
	Repo    repositories.MatchRepository
	Jobs    core.JobSource
	Resumes core.ResumeSource
	Matcher core.Matcher
	Quotas  core.QuotaSource
	Config  utils.AppConfig
	Now     func() time.Time
}

type matchService struct {
	MatchDeps
}

func NewMatchService(d MatchDeps) MatchService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &matchService{d}
}

// matchKey is what the AI would read for this request, and its digest: the same offer and the same CV give the
// same digest, so a reading is reused until one of them changes.
type matchKey struct {
	job      *core.JobInfo
	resume   string
	language string
	hash     string
}

func (s *matchService) key(ctx context.Context, userID string, req api.MatchRequest) (*matchKey, *models.ApiError) {
	token, err := utils.IssueToken(s.Config, utils.Claims{ID: userID, Role: "user", Type: utils.TokenAccess}, accessTokenTTL)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "internal error")
	}
	job, err := s.Jobs.Job(ctx, token, req.JobID)
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	resume, err := s.Resumes.Summary(ctx, token, req.ResumeID)
	if err != nil {
		return nil, peerError(err, "resume not found")
	}
	language := req.Language
	if language == "" {
		language = "fr"
	}
	h := sha256.New()
	for _, part := range []string{job.Title, job.Company, job.Description, strings.Join(job.Skills, "\x1f"), resume} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return &matchKey{job: job, resume: resume, language: language, hash: hex.EncodeToString(h.Sum(nil))}, nil
}

// usage is the plan's monthly limit of analyses and how many were made this month; limit nil means no limit.
func (s *matchService) usage(ctx context.Context, userID string) (limit *int, used int, apiErr *models.ApiError) {
	quotas, err := s.Quotas.Quotas(ctx, userID)
	if err != nil {
		return nil, 0, peerError(err, "plan not found")
	}
	monthly, ok := quotas[core.QuotaAIMatches]
	if !ok || monthly < 0 {
		return nil, 0, nil
	}
	now := s.Now().UTC()
	n, err := s.Repo.CountSince(ctx, userID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, 0, repoError(err, "match not found")
	}
	return &monthly, n, nil
}

func (s *matchService) Find(ctx context.Context, userID string, req api.MatchRequest) (*api.MatchResponse, *models.ApiError) {
	k, apiErr := s.key(ctx, userID, req)
	if apiErr != nil {
		return nil, apiErr
	}
	m, err := s.Repo.Find(ctx, userID, req.JobID, req.ResumeID, k.language, k.hash)
	if err != nil {
		return nil, repoError(err, "no analysis yet")
	}
	return s.respond(ctx, userID, m)
}

// Match reuses the reading of the same offer and CV when there is one: it is not asked again nor counted. Otherwise
// the plan must allow one more this month (402), and the AI is asked.
func (s *matchService) Match(ctx context.Context, userID string, req api.MatchRequest) (*api.MatchResponse, *models.ApiError) {
	k, apiErr := s.key(ctx, userID, req)
	if apiErr != nil {
		return nil, apiErr
	}
	m, err := s.Repo.Find(ctx, userID, req.JobID, req.ResumeID, k.language, k.hash)
	if err == nil {
		return s.respond(ctx, userID, m)
	}
	if !errors.Is(err, repositories.ErrNotFound) {
		return nil, repoError(err, "match not found")
	}
	if strings.TrimSpace(k.resume) == "" {
		return nil, apiError(http.StatusUnprocessableEntity, "the CV is empty")
	}
	limit, used, apiErr := s.usage(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	switch {
	case limit != nil && *limit == 0:
		return nil, apiError(http.StatusPaymentRequired, "the AI analysis of offers is part of the subscription")
	case limit != nil && used >= *limit:
		return nil, apiError(http.StatusPaymentRequired, "monthly limit of AI analyses of your plan reached")
	}
	res, err := s.Matcher.Match(ctx, core.MatchInput{
		JobTitle: k.job.Title, Company: k.job.Company, JobDescription: k.job.Description, JobSkills: k.job.Skills,
		Candidate: k.resume, Language: k.language,
	})
	if err != nil {
		return nil, peerError(err, "job not found")
	}
	m, err = s.Repo.Save(ctx, &db.JobMatch{
		UserID: userID, JobID: req.JobID, ResumeID: req.ResumeID, Language: k.language, InputHash: k.hash,
		Score: res.Score, Level: res.Level, Strengths: res.Strengths, Gaps: res.Gaps, Advice: res.Advice,
		Keywords:        db.MatchKeywords{Matched: res.KeywordsMatched, Missing: res.KeywordsMissing},
		KeywordCoverage: res.KeywordCoverage,
	})
	if err != nil {
		return nil, repoError(err, "match not found")
	}
	return s.respond(ctx, userID, m)
}

func (s *matchService) respond(ctx context.Context, userID string, m *db.JobMatch) (*api.MatchResponse, *models.ApiError) {
	limit, used, apiErr := s.usage(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	strengths, gaps := m.Strengths, m.Gaps
	if strengths == nil {
		strengths = []string{}
	}
	if gaps == nil {
		gaps = []string{}
	}
	matched, missing := m.Keywords.Matched, m.Keywords.Missing
	if matched == nil {
		matched = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	return &api.MatchResponse{
		Score: m.Score, Level: m.Level, Strengths: strengths, Gaps: gaps, Advice: m.Advice, CreatedAt: m.CreatedAt,
		Keywords: api.MatchKeywords{Matched: matched, Missing: missing}, KeywordCoverage: m.KeywordCoverage,
		Limit: limit, Used: used,
	}, nil
}
