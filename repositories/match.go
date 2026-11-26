package repositories

import (
	"context"
	"time"

	"9itchi-email-api/models/db"
)

// MatchRepository keeps the AI's readings of how well a CV fits an offer.
type MatchRepository interface {
	// Find returns the reading of exactly this offer, CV, language and input (ErrNotFound when there is none).
	Find(ctx context.Context, userID, jobID, resumeID, language, inputHash string) (*db.JobMatch, error)
	// Save stores a reading; saving the same key again keeps the first one.
	Save(ctx context.Context, m *db.JobMatch) (*db.JobMatch, error)
	// Upgrade turns a score (m.ID) into an analysis with the new reading, counted now (ErrNotFound when the row is
	// not a score of the user any more).
	Upgrade(ctx context.Context, m *db.JobMatch) (*db.JobMatch, error)
	// CountSince counts the readings of one origin made for the user since a time: the plan's monthly limit counts
	// the analyses, its daily limit the scores.
	CountSince(ctx context.Context, userID, origin string, since time.Time) (int, error)
}

type matchRepository struct {
	store *Store
}

func NewMatchRepository(store *Store) MatchRepository {
	return &matchRepository{store: store}
}

const matchColumns = `id, user_id, job_id, resume_id, language, input_hash, score, level, strengths, gaps, advice, keywords, keyword_coverage, origin, created_at`

func scanMatch(r rowScanner) (*db.JobMatch, error) {
	var m db.JobMatch
	if err := r.Scan(&m.ID, &m.UserID, &m.JobID, &m.ResumeID, &m.Language, &m.InputHash, &m.Score, &m.Level,
		&m.Strengths, &m.Gaps, &m.Advice, &m.Keywords, &m.KeywordCoverage, &m.Origin, &m.CreatedAt); err != nil {
		return nil, MapError(err)
	}
	return &m, nil
}

func (r *matchRepository) Find(ctx context.Context, userID, jobID, resumeID, language, inputHash string) (*db.JobMatch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanMatch(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+matchColumns+` FROM job_matches
		WHERE user_id = $1 AND job_id = $2 AND resume_id = $3 AND language = $4 AND input_hash = $5`,
		userID, jobID, resumeID, language, inputHash))
}

// matchLists gives the lists of a reading, empty rather than nil (the columns are NOT NULL).
func matchLists(m *db.JobMatch) (strengths, gaps []string, keywords db.MatchKeywords) {
	strengths, gaps = m.Strengths, m.Gaps
	if strengths == nil {
		strengths = []string{}
	}
	if gaps == nil {
		gaps = []string{}
	}
	keywords = db.MatchKeywords{Matched: m.Keywords.Matched, Missing: m.Keywords.Missing}
	if keywords.Matched == nil {
		keywords.Matched = []string{}
	}
	if keywords.Missing == nil {
		keywords.Missing = []string{}
	}
	return strengths, gaps, keywords
}

func (r *matchRepository) Save(ctx context.Context, m *db.JobMatch) (*db.JobMatch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	strengths, gaps, keywords := matchLists(m)
	origin := m.Origin
	if origin == "" {
		origin = db.MatchOriginAnalysis
	}
	// DO UPDATE with no change, so the row that was there first comes back.
	return scanMatch(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO job_matches (user_id, job_id, resume_id, language, input_hash, score, level, strengths, gaps, advice, keywords, keyword_coverage, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (user_id, job_id, resume_id, language, input_hash) DO UPDATE SET score = job_matches.score
		RETURNING `+matchColumns,
		m.UserID, m.JobID, m.ResumeID, m.Language, m.InputHash, m.Score, m.Level, strengths, gaps, m.Advice, keywords, m.KeywordCoverage, origin))
}

func (r *matchRepository) Upgrade(ctx context.Context, m *db.JobMatch) (*db.JobMatch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	strengths, gaps, keywords := matchLists(m)
	return scanMatch(r.store.Conn(ctx).QueryRow(ctx, `
		UPDATE job_matches SET score = $3, level = $4, strengths = $5, gaps = $6, advice = $7, keywords = $8,
			keyword_coverage = $9, origin = 'analysis', created_at = now()
		WHERE id = $1 AND user_id = $2 AND origin = 'score'
		RETURNING `+matchColumns,
		m.ID, m.UserID, m.Score, m.Level, strengths, gaps, m.Advice, keywords, m.KeywordCoverage))
}

func (r *matchRepository) CountSince(ctx context.Context, userID, origin string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT count(*) FROM job_matches WHERE user_id = $1 AND origin = $2 AND created_at >= $3`, userID, origin, since).Scan(&n)
	return n, MapError(err)
}
