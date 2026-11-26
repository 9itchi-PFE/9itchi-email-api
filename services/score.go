package services

import (
	"context"
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

// maxScoreJobs is how many offers one score request may name.
const maxScoreJobs = 10

// maxScoreReasons is how many strengths and gaps a score shows.
const maxScoreReasons = 3

// ScoreService gives quick AI scores of a list of offers for one CV, within the plan's daily limit of scores.
type ScoreService interface {
	// POST /applications/scores: scores the offers not scored yet while today's limit allows; one offer that fails
	// never fails the others.
	Score(ctx context.Context, userID string, req api.ScoreRequest) (*api.ScoreResponse, *models.ApiError)
	// GET /applications/scores: the offers already scored or analysed for this CV; nothing is asked nor counted.
	Find(ctx context.Context, userID string, q api.ScoreQuery) (*api.ScoreResponse, *models.ApiError)
}

type scoreService struct {
	MatchDeps
}

// NewScoreService builds the score service on the collaborators of the match service: a score is a reading kept in
// job_matches with origin 'score'.
func NewScoreService(d MatchDeps) ScoreService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &scoreService{d}
}

// allowance is the plan's daily limit of scores (nil: no limit) and how many were made today (Tunis day).
func (s *scoreService) allowance(ctx context.Context, userID string) (limit *int, used int, apiErr *models.ApiError) {
	quotas, err := s.Quotas.Quotas(ctx, userID)
	if err != nil {
		return nil, 0, peerError(err, "plan not found")
	}
	start, _ := tunisDayOf(s.Now())
	n, err := s.Repo.CountSince(ctx, userID, db.MatchOriginScore, start)
	if err != nil {
		return nil, 0, repoError(err, "match not found")
	}
	if daily, ok := quotas[core.QuotaAIScores]; ok && daily >= 0 {
		return &daily, n, nil
	}
	return nil, n, nil
}

// resume is the CV as the AI reads it, read once for the whole list, and the user's token for the offers.
func (s *scoreService) resume(ctx context.Context, userID, resumeID string) (token, text string, apiErr *models.ApiError) {
	token, err := utils.IssueToken(s.Config, utils.Claims{ID: userID, Role: "user", Type: utils.TokenAccess}, accessTokenTTL)
	if err != nil {
		return "", "", apiError(http.StatusInternalServerError, "internal error")
	}
	text, err = s.Resumes.Summary(ctx, token, resumeID)
	if err != nil {
		return "", "", peerError(err, "resume not found")
	}
	return token, text, nil
}

func (s *scoreService) Score(ctx context.Context, userID string, req api.ScoreRequest) (*api.ScoreResponse, *models.ApiError) {
	if len(req.JobIDs) == 0 || len(req.JobIDs) > maxScoreJobs {
		return nil, apiError(http.StatusBadRequest, "give 1 to 10 offers")
	}
	token, resume, apiErr := s.resume(ctx, userID, req.ResumeID)
	if apiErr != nil {
		return nil, apiErr
	}
	if strings.TrimSpace(resume) == "" {
		return nil, apiError(http.StatusUnprocessableEntity, "the CV is empty")
	}
	limit, used, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	language := scoreLanguage(req.Language)
	items := make([]api.ScoreItem, 0, len(req.JobIDs))
	for _, jobID := range uniqueIDs(req.JobIDs) {
		job, err := s.Jobs.Job(ctx, token, jobID)
		if err != nil {
			items = append(items, scoreFailed(jobID, "job", err))
			continue
		}
		hash := matchHash(job, resume)
		m, err := s.Repo.Find(ctx, userID, jobID, req.ResumeID, language, hash)
		switch {
		case err == nil:
			items = append(items, scoreItem(jobID, m, api.ScoreCached))
			continue
		case !errors.Is(err, repositories.ErrNotFound):
			items = append(items, scoreFailed(jobID, "repository", err))
			continue
		case limit != nil && used >= *limit:
			items = append(items, api.ScoreItem{JobID: jobID, Reasons: []string{}, Status: api.ScoreQuota})
			continue
		}
		res, err := s.Matcher.Match(ctx, matchInput(job, resume, language))
		if err != nil {
			items = append(items, scoreFailed(jobID, "ai", err))
			continue
		}
		m, err = s.Repo.Save(ctx, &db.JobMatch{
			UserID: userID, JobID: jobID, ResumeID: req.ResumeID, Language: language, InputHash: hash,
			Score: res.Score, Level: res.Level, Strengths: res.Strengths, Gaps: res.Gaps, Advice: res.Advice,
			Keywords:        db.MatchKeywords{Matched: res.KeywordsMatched, Missing: res.KeywordsMissing},
			KeywordCoverage: res.KeywordCoverage, Origin: db.MatchOriginScore,
		})
		if err != nil {
			items = append(items, scoreFailed(jobID, "repository", err))
			continue
		}
		used++
		items = append(items, scoreItem(jobID, m, api.ScoreOK))
	}
	return &api.ScoreResponse{Items: items, Allowance: &api.ScoreAllowance{Limit: limit, Used: used}}, nil
}

func (s *scoreService) Find(ctx context.Context, userID string, q api.ScoreQuery) (*api.ScoreResponse, *models.ApiError) {
	var ids []string
	for _, id := range strings.Split(q.JobIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > maxScoreJobs {
		return nil, apiError(http.StatusBadRequest, "give 1 to 10 offers")
	}
	for _, id := range ids {
		if !uuidRe.MatchString(id) {
			return nil, apiError(http.StatusBadRequest, "invalid job id")
		}
	}
	token, resume, apiErr := s.resume(ctx, userID, q.ResumeID)
	if apiErr != nil {
		return nil, apiErr
	}
	language := scoreLanguage(q.Language)
	items := []api.ScoreItem{}
	for _, jobID := range uniqueIDs(ids) {
		job, err := s.Jobs.Job(ctx, token, jobID)
		if err != nil {
			continue
		}
		m, err := s.Repo.Find(ctx, userID, jobID, q.ResumeID, language, matchHash(job, resume))
		if err != nil {
			continue
		}
		items = append(items, scoreItem(jobID, m, api.ScoreCached))
	}
	return &api.ScoreResponse{Items: items}, nil
}

func scoreLanguage(language string) string {
	if language == "" {
		return "fr"
	}
	return language
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// scoreItem is a reading as a score: its strengths then its gaps, up to 3 (never its advice).
func scoreItem(jobID string, m *db.JobMatch, status string) api.ScoreItem {
	reasons := make([]string, 0, maxScoreReasons)
	for _, r := range append(append([]string{}, m.Strengths...), m.Gaps...) {
		if len(reasons) == maxScoreReasons {
			break
		}
		reasons = append(reasons, r)
	}
	return api.ScoreItem{JobID: jobID, Score: m.Score, Level: m.Level, Reasons: reasons, Status: status}
}

func scoreFailed(jobID, step string, err error) api.ScoreItem {
	utils.Logger.Logger.Warnw("score failed", "job_id", jobID, "step", step, "error", err)
	return api.ScoreItem{JobID: jobID, Reasons: []string{}, Status: api.ScoreError}
}
