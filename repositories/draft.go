package repositories

import (
	"context"
	"time"

	"9itchi-email-api/models/db"
)

// DraftRepository keeps the letters the AI wrote.
type DraftRepository interface {
	// Latest returns the newest letter for exactly this offer, input, language and tone (ErrNotFound when none).
	Latest(ctx context.Context, userID, jobID, inputHash, language, tone string) (*db.AIDraft, error)
	// LatestByURL is Latest for an offer given by its link.
	LatestByURL(ctx context.Context, userID, sourceURL, inputHash, language, tone string) (*db.AIDraft, error)
	// LatestAnyByURL returns the newest letter for the link, language and tone whatever the input it was written
	// from (ErrNotFound when none).
	LatestAnyByURL(ctx context.Context, userID, sourceURL, language, tone string) (*db.AIDraft, error)
	Save(ctx context.Context, d *db.AIDraft) (*db.AIDraft, error)
	// CountSince counts the letters written for the user since a time: the plan's monthly limit counts these.
	CountSince(ctx context.Context, userID string, since time.Time) (int, error)
}

type draftRepository struct {
	store *Store
}

func NewDraftRepository(store *Store) DraftRepository {
	return &draftRepository{store: store}
}

const draftColumns = `id, user_id, job_id, source_url, language, tone, input_hash, subject, body, created_at`

func scanDraft(r rowScanner) (*db.AIDraft, error) {
	var d db.AIDraft
	if err := r.Scan(&d.ID, &d.UserID, &d.JobID, &d.SourceURL, &d.Language, &d.Tone, &d.InputHash, &d.Subject, &d.Body, &d.CreatedAt); err != nil {
		return nil, MapError(err)
	}
	return &d, nil
}

func (r *draftRepository) Latest(ctx context.Context, userID, jobID, inputHash, language, tone string) (*db.AIDraft, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanDraft(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+draftColumns+` FROM ai_drafts
		WHERE user_id = $1 AND job_id = $2 AND input_hash = $3 AND language = $4 AND tone = $5
		ORDER BY created_at DESC LIMIT 1`, userID, jobID, inputHash, language, tone))
}

func (r *draftRepository) LatestByURL(ctx context.Context, userID, sourceURL, inputHash, language, tone string) (*db.AIDraft, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanDraft(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+draftColumns+` FROM ai_drafts
		WHERE user_id = $1 AND source_url = $2 AND input_hash = $3 AND language = $4 AND tone = $5
		ORDER BY created_at DESC LIMIT 1`, userID, sourceURL, inputHash, language, tone))
}

func (r *draftRepository) LatestAnyByURL(ctx context.Context, userID, sourceURL, language, tone string) (*db.AIDraft, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanDraft(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+draftColumns+` FROM ai_drafts
		WHERE user_id = $1 AND source_url = $2 AND language = $3 AND tone = $4
		ORDER BY created_at DESC LIMIT 1`, userID, sourceURL, language, tone))
}

func (r *draftRepository) Save(ctx context.Context, d *db.AIDraft) (*db.AIDraft, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanDraft(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO ai_drafts (user_id, job_id, source_url, language, tone, input_hash, subject, body) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+draftColumns, d.UserID, d.JobID, d.SourceURL, d.Language, d.Tone, d.InputHash, d.Subject, d.Body))
}

func (r *draftRepository) CountSince(ctx context.Context, userID string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT count(*) FROM ai_drafts WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n)
	return n, MapError(err)
}
