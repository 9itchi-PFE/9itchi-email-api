package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

type memMatches struct {
	mu      sync.Mutex
	rows    []db.JobMatch
	err     error
	saveErr error
}

func (m *memMatches) Find(_ context.Context, userID, jobID, resumeID, language, hash string) (*db.JobMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	for i := range m.rows {
		r := m.rows[i]
		if r.UserID == userID && r.JobID == jobID && r.ResumeID == resumeID && r.Language == language && r.InputHash == hash {
			return &r, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memMatches) Save(_ context.Context, r *db.JobMatch) (*db.JobMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	c := *r
	c.ID = fmt.Sprintf("m%d", len(m.rows)+1)
	if c.Origin == "" {
		c.Origin = db.MatchOriginAnalysis
	}
	c.CreatedAt = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	m.rows = append(m.rows, c)
	return &c, nil
}

func (m *memMatches) Upgrade(_ context.Context, r *db.JobMatch) (*db.JobMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].ID == r.ID && m.rows[i].UserID == r.UserID && m.rows[i].Origin == db.MatchOriginScore {
			row := &m.rows[i]
			row.Score, row.Level, row.Strengths, row.Gaps, row.Advice = r.Score, r.Level, r.Strengths, r.Gaps, r.Advice
			row.Keywords, row.KeywordCoverage, row.Origin = r.Keywords, r.KeywordCoverage, db.MatchOriginAnalysis
			c := *row
			return &c, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memMatches) CountSince(_ context.Context, userID, origin string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.rows {
		if r.UserID == userID && r.Origin == origin && !r.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

type fakeMatcher struct {
	noKeywords bool
	calls      int
	in         core.MatchInput
	err        error
}

func (f *fakeMatcher) Match(_ context.Context, in core.MatchInput) (*core.MatchResult, error) {
	f.calls++
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	if f.noKeywords {
		return &core.MatchResult{Score: 68, Level: "good", Strengths: []string{"a"}}, nil
	}
	return &core.MatchResult{Score: 68, Level: "good", Strengths: []string{"React en stage"}, Gaps: nil, Advice: "Mets en avant React.",
		KeywordsMatched: []string{"React"}, KeywordsMissing: []string{"Docker"}, KeywordCoverage: 50}, nil
}

// summaries answers a CV text per call: a CV that changes gives a new reading.
type summaries struct {
	fakeResumes
	text string
}

func (s *summaries) Summary(context.Context, string, string) (string, error) { return s.text, nil }

type matchHarness struct {
	svc     MatchService
	repo    *memMatches
	ai      *fakeMatcher
	resumes *summaries
}

func newMatchHarness(quotas map[string]int) *matchHarness {
	h := &matchHarness{repo: &memMatches{}, ai: &fakeMatcher{}, resumes: &summaries{text: "Ines, ENIS, React"}}
	h.svc = NewMatchService(MatchDeps{
		Repo: h.repo, Jobs: fakeJobs{}, Resumes: h.resumes, Matcher: h.ai, Quotas: mapQuota(quotas), Config: testCfg,
		Now: func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	return h
}

type mapQuota map[string]int

func (m mapQuota) Quotas(context.Context, string) (map[string]int, error) { return m, nil }

func TestMatchIsAskedOnceAndCounted(t *testing.T) {
	h := newMatchHarness(map[string]int{core.QuotaAIMatches: 2})
	req := api.MatchRequest{JobID: jobID, ResumeID: resumID}

	_, apiErr := h.svc.Find(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code, "nothing asked yet")

	res, apiErr := h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 68, res.Score)
	assert.Equal(t, "good", res.Level)
	assert.Equal(t, []string{}, res.Gaps, "an empty list, never null")
	assert.Equal(t, api.MatchKeywords{Matched: []string{"React"}, Missing: []string{"Docker"}}, res.Keywords)
	assert.Equal(t, 50, res.KeywordCoverage)
	require.NotNil(t, res.Limit)
	assert.Equal(t, 2, *res.Limit)
	assert.Equal(t, 1, res.Used)
	assert.Equal(t, "fr", h.ai.in.Language, "French by default")
	assert.Equal(t, "Ines, ENIS, React", h.ai.in.Candidate)
	assert.Equal(t, "Acme", h.ai.in.Company)

	res, apiErr = h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 1, h.ai.calls, "the same offer and CV are not asked again")
	assert.Equal(t, 1, res.Used, "nor counted again")
	found, apiErr := h.svc.Find(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 68, found.Score)
	assert.Equal(t, []string{"Docker"}, found.Keywords.Missing, "GET returns the stored keywords")
	assert.Equal(t, 50, found.KeywordCoverage)

	h.resumes.text = "Ines, ENIS, React, Docker"
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 2, h.ai.calls, "a changed CV is read again")

	h.resumes.text = "Ines, ENIS, React, Docker, Go"
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code, "two this month, the plan allows two")
	assert.Equal(t, 2, h.ai.calls)

	h.resumes.text = "Ines, ENIS, React"
	res, apiErr = h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr, "a reading already made is still shown at the limit")
	assert.Equal(t, 2, res.Used)
}

func TestMatchFollowsThePlan(t *testing.T) {
	req := api.MatchRequest{JobID: jobID, ResumeID: resumID, Language: "en"}

	h := newMatchHarness(map[string]int{})
	res, apiErr := h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr, "no key: no limit")
	assert.Nil(t, res.Limit)
	assert.Equal(t, "en", h.ai.in.Language)

	h = newMatchHarness(map[string]int{core.QuotaAIMatches: 0})
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Zero(t, h.ai.calls, "nothing is asked when the plan allows none")

	h = newMatchHarness(nil)
	h.resumes.text = "  "
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code, "an empty CV is not sent to the AI")

	h = newMatchHarness(nil)
	h.ai.err = errors.New("ai down")
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Empty(t, h.repo.rows, "nothing is kept when the AI fails")

	h = newMatchHarness(nil)
	h.svc = NewMatchService(MatchDeps{Repo: h.repo, Jobs: fakeJobs{err: core.ErrPeerNotFound}, Resumes: h.resumes, Matcher: h.ai, Quotas: mapQuota(nil), Config: testCfg})
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)

	h = newMatchHarness(nil)
	h.repo.err = errors.New("db down")
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
	assert.Zero(t, h.ai.calls)
}

func TestMatchRespondsEmptyKeywordsForOldRows(t *testing.T) {
	h := newMatchHarness(nil)
	h.ai.noKeywords = true
	res, apiErr := h.svc.Match(context.Background(), uid, api.MatchRequest{JobID: jobID, ResumeID: resumID})
	require.Nil(t, apiErr)
	assert.Equal(t, api.MatchKeywords{Matched: []string{}, Missing: []string{}}, res.Keywords, "empty lists, never null")
	assert.Zero(t, res.KeywordCoverage)
}

// seedScore keeps a quick score of the test offer and CV, as POST /applications/scores would.
func (h *matchHarness) seedScore(t *testing.T) {
	t.Helper()
	job := &core.JobInfo{Title: "Stage React", Company: "Acme", Description: "desc"}
	_, err := h.repo.Save(context.Background(), &db.JobMatch{
		UserID: uid, JobID: jobID, ResumeID: resumID, Language: "fr", InputHash: matchHash(job, h.resumes.text),
		Score: 61, Level: "good", Strengths: []string{"React"}, Advice: "Conseil du score.", Origin: db.MatchOriginScore,
	})
	require.NoError(t, err)
}

func TestMatchHidesTheAdviceOfAScoreThePlanDoesNotCover(t *testing.T) {
	req := api.MatchRequest{JobID: jobID, ResumeID: resumID}
	for name, quotas := range map[string]map[string]int{
		"no analyses": {core.QuotaAIMatches: 0},
		"month used":  {core.QuotaAIMatches: 1},
	} {
		t.Run(name, func(t *testing.T) {
			h := newMatchHarness(quotas)
			if quotas[core.QuotaAIMatches] == 1 {
				_, err := h.repo.Save(context.Background(), &db.JobMatch{UserID: uid, JobID: "other", ResumeID: resumID, Language: "fr"})
				require.NoError(t, err)
			}
			h.seedScore(t)

			res, apiErr := h.svc.Match(context.Background(), uid, req)
			require.Nil(t, apiErr, "a score is shown, never a 402")
			assert.Equal(t, 61, res.Score)
			assert.Equal(t, db.MatchOriginScore, res.Origin)
			assert.Empty(t, res.Advice, "the advice is hidden")
			assert.Zero(t, h.ai.calls)

			found, apiErr := h.svc.Find(context.Background(), uid, req)
			require.Nil(t, apiErr)
			assert.Empty(t, found.Advice)
			assert.Equal(t, res.Used, found.Used, "not counted")
		})
	}
}

func TestMatchUpgradesAScoreWhenThePlanAllows(t *testing.T) {
	h := newMatchHarness(map[string]int{core.QuotaAIMatches: 2})
	h.seedScore(t)
	req := api.MatchRequest{JobID: jobID, ResumeID: resumID}

	found, apiErr := h.svc.Find(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Empty(t, found.Advice, "GET never asks: the score keeps its advice hidden")
	assert.Zero(t, found.Used)

	res, apiErr := h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 1, h.ai.calls, "the full analysis is asked")
	assert.Equal(t, db.MatchOriginAnalysis, res.Origin)
	assert.Equal(t, 68, res.Score)
	assert.Equal(t, "Mets en avant React.", res.Advice)
	assert.Equal(t, 1, res.Used, "and counted")
	assert.Len(t, h.repo.rows, 1, "the score row becomes the analysis")

	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, 1, h.ai.calls, "not asked again")

	h = newMatchHarness(map[string]int{core.QuotaAIMatches: 2})
	h.seedScore(t)
	h.ai.err = errors.New("ai down")
	_, apiErr = h.svc.Match(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Equal(t, db.MatchOriginScore, h.repo.rows[0].Origin, "the score stays a score")
}
