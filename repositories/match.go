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
	// CountSince counts the readings made for the user since a time: the plan's monthly limit counts these.
	CountSince(ctx context.Context, userID string, since time.Time) (int, error)
}

type matchRepository struct {
	store *Store
}

func NewMatchRepository(store *Store) MatchRepository {
	return &matchRepository{store: store}
}

const matchColumns = `id, user_id, job_id, resume_id, language, input_hash, score, level, strengths, gaps, advice, keywords, keyword_coverage, created_at`

func scanMatch(r rowScanner) (*db.JobMatch, error) {
	var m db.JobMatch
	if err := r.Scan(&m.ID, &m.UserID, &m.JobID, &m.ResumeID, &m.Language, &m.InputHash, &m.Score, &m.Level,
		&m.Strengths, &m.Gaps, &m.Advice, &m.Keywords, &m.KeywordCoverage, &m.CreatedAt); err != nil {
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

func (r *matchRepository) Save(ctx context.Context, m *db.JobMatch) (*db.JobMatch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	strengths, gaps := m.Strengths, m.Gaps
	if strengths == nil {
		strengths = []string{}
	}
	if gaps == nil {
		gaps = []string{}
	}
	keywords := db.MatchKeywords{Matched: m.Keywords.Matched, Missing: m.Keywords.Missing}
	if keywords.Matched == nil {
		keywords.Matched = []string{}
	}
	if keywords.Missing == nil {
		keywords.Missing = []string{}
	}
	// DO UPDATE with no change, so the row that was there first comes back.
	return scanMatch(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO job_matches (user_id, job_id, resume_id, language, input_hash, score, level, strengths, gaps, advice, keywords, keyword_coverage)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, job_id, resume_id, language, input_hash) DO UPDATE SET score = job_matches.score
		RETURNING `+matchColumns,
		m.UserID, m.JobID, m.ResumeID, m.Language, m.InputHash, m.Score, m.Level, strengths, gaps, m.Advice, keywords, m.KeywordCoverage))
}

func (r *matchRepository) CountSince(ctx context.Context, userID string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT count(*) FROM job_matches WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n)
	return n, MapError(err)
}
