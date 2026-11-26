package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
)

const (
	job1 = "44444444-4444-4444-4444-444444444441"
	job2 = "44444444-4444-4444-4444-444444444442"
	job3 = "44444444-4444-4444-4444-444444444443"
)

// funcJobs answers each offer with JobFn (func-field fake).
type funcJobs struct {
	fakeJobs
	JobFn func(id string) (*core.JobInfo, error)
}

func (f funcJobs) Job(_ context.Context, _, id string) (*core.JobInfo, error) { return f.JobFn(id) }

// funcMatcher answers the AI with MatchFn (func-field fake).
type funcMatcher struct {
	MatchFn func(in core.MatchInput) (*core.MatchResult, error)
}

func (f funcMatcher) Match(_ context.Context, in core.MatchInput) (*core.MatchResult, error) {
	return f.MatchFn(in)
}

type funcQuotas struct {
	QuotasFn func() (map[string]int, error)
}

func (f funcQuotas) Quotas(context.Context, string) (map[string]int, error) { return f.QuotasFn() }

type scoreHarness struct {
	svc   ScoreService
	repo  *memMatches
	calls []string
}

func newScoreHarness(quotas map[string]int, jobErr map[string]error, aiErr map[string]error) *scoreHarness {
	h := &scoreHarness{repo: &memMatches{}}
	jobs := funcJobs{JobFn: func(id string) (*core.JobInfo, error) {
		if err := jobErr[id]; err != nil {
			return nil, err
		}
		return &core.JobInfo{Title: "Stage " + id, Company: "Acme", Description: "desc"}, nil
	}}
	ai := funcMatcher{MatchFn: func(in core.MatchInput) (*core.MatchResult, error) {
		h.calls = append(h.calls, in.JobTitle)
		if err := aiErr[in.JobTitle]; err != nil {
			return nil, err
		}
		return &core.MatchResult{Score: 72, Level: "good", Strengths: []string{"React", "Go"}, Gaps: []string{"Docker", "K8s"},
			Advice: "Conseil."}, nil
	}}
	h.svc = NewScoreService(MatchDeps{
		Repo: h.repo, Jobs: jobs, Resumes: &summaries{text: "Ines, ENIS, React"}, Matcher: ai,
		Quotas: funcQuotas{QuotasFn: func() (map[string]int, error) { return quotas, nil }}, Config: testCfg,
		Now: func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	return h
}

func TestScoreScoresWithinTheDailyLimit(t *testing.T) {
	h := newScoreHarness(map[string]int{core.QuotaAIScores: 2}, nil, nil)
	req := api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1, job2, job3, job1}}

	res, apiErr := h.svc.Score(context.Background(), uid, req)
	require.Nil(t, apiErr)
	require.Len(t, res.Items, 3, "an offer named twice is scored once")
	assert.Equal(t, api.ScoreItem{JobID: job1, Score: 72, Level: "good", Reasons: []string{"React", "Go", "Docker"}, Status: api.ScoreOK},
		res.Items[0], "strengths then gaps, up to 3, never the advice")
	assert.Equal(t, api.ScoreOK, res.Items[1].Status)
	assert.Equal(t, api.ScoreItem{JobID: job3, Reasons: []string{}, Status: api.ScoreQuota}, res.Items[2])
	require.NotNil(t, res.Allowance.Limit)
	assert.Equal(t, 2, *res.Allowance.Limit)
	assert.Equal(t, 2, res.Allowance.Used)
	assert.Len(t, h.calls, 2)
	assert.Equal(t, db.MatchOriginScore, h.repo.rows[0].Origin)

	res, apiErr = h.svc.Score(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, api.ScoreCached, res.Items[0].Status, "a score already made is free")
	assert.Equal(t, api.ScoreCached, res.Items[1].Status)
	assert.Equal(t, api.ScoreQuota, res.Items[2].Status)
	assert.Len(t, h.calls, 2, "nothing asked again")

	found, apiErr := h.svc.Find(context.Background(), uid, api.ScoreQuery{ResumeID: resumID, JobIDs: job1 + ", " + job3})
	require.Nil(t, apiErr)
	require.Len(t, found.Items, 1, "GET gives only what was scored")
	assert.Equal(t, job1, found.Items[0].JobID)
	assert.Equal(t, api.ScoreCached, found.Items[0].Status)
	assert.Nil(t, found.Allowance)
	assert.Len(t, h.calls, 2, "GET never asks the AI")
}

func TestScoreAnalysisIsCachedAndLimitFollowsThePlan(t *testing.T) {
	h := newScoreHarness(map[string]int{core.QuotaAIScores: 0}, nil, nil)
	job := &core.JobInfo{Title: "Stage " + job1, Company: "Acme", Description: "desc"}
	_, err := h.repo.Save(context.Background(), &db.JobMatch{UserID: uid, JobID: job1, ResumeID: resumID, Language: "fr",
		InputHash: matchHash(job, "Ines, ENIS, React"), Score: 80, Level: "strong", Gaps: []string{"Docker"}})
	require.NoError(t, err)

	res, apiErr := h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1, job2}})
	require.Nil(t, apiErr, "no 402 on a list")
	assert.Equal(t, api.ScoreItem{JobID: job1, Score: 80, Level: "strong", Reasons: []string{"Docker"}, Status: api.ScoreCached}, res.Items[0],
		"a full analysis counts as a score")
	assert.Equal(t, api.ScoreQuota, res.Items[1].Status, "0 = none")
	assert.Empty(t, h.calls)

	h = newScoreHarness(map[string]int{}, nil, nil)
	res, apiErr = h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1}})
	require.Nil(t, apiErr)
	assert.Nil(t, res.Allowance.Limit, "no key: no limit")
	assert.Equal(t, api.ScoreOK, res.Items[0].Status)
}

func TestScoreErrorsStayPerOffer(t *testing.T) {
	h := newScoreHarness(nil, map[string]error{job1: core.ErrPeerNotFound}, map[string]error{"Stage " + job2: errors.New("ai down")})
	res, apiErr := h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1, job2, job3}})
	require.Nil(t, apiErr, "never fails the list")
	assert.Equal(t, api.ScoreError, res.Items[0].Status)
	assert.Equal(t, api.ScoreError, res.Items[1].Status)
	assert.Equal(t, api.ScoreOK, res.Items[2].Status)
	assert.Equal(t, 1, res.Allowance.Used, "failures are not counted")

	h = newScoreHarness(nil, nil, nil)
	h.repo.saveErr = errors.New("db down")
	res, apiErr = h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1}})
	require.Nil(t, apiErr)
	assert.Equal(t, api.ScoreError, res.Items[0].Status)
	assert.Zero(t, res.Allowance.Used)
}

func TestScoreRefusesBadRequests(t *testing.T) {
	h := newScoreHarness(nil, nil, nil)
	_, apiErr := h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)

	_, apiErr = h.svc.Find(context.Background(), uid, api.ScoreQuery{ResumeID: resumID, JobIDs: "nope"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)

	h.svc.(*scoreService).Resumes = &summaries{text: " "}
	_, apiErr = h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code, "an empty CV is not sent to the AI")

	h = newScoreHarness(nil, nil, nil)
	h.svc.(*scoreService).Quotas = funcQuotas{QuotasFn: func() (map[string]int, error) { return nil, core.ErrPeerUnavailable }}
	_, apiErr = h.svc.Score(context.Background(), uid, api.ScoreRequest{ResumeID: resumID, JobIDs: []string{job1}})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Empty(t, h.calls)
}
