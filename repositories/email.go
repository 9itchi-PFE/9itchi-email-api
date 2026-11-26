package repositories

import (
	"context"
	"time"

	"9itchi-email-api/models/db"
)

// EmailRepository is the only layer that touches PostgreSQL for email data.
type EmailRepository interface {
	Create(ctx context.Context, e *db.Email) (*db.Email, error)
	// Get returns the email only when it belongs to userID.
	Get(ctx context.Context, userID, id string) (*db.Email, error)
	GetByID(ctx context.Context, id string) (*db.Email, error)
	ListByUser(ctx context.Context, userID string, limit int) ([]db.Email, error)
	// Claim moves a draft or scheduled email to "sending" (ErrNotFound when it is in another state).
	Claim(ctx context.Context, id string) (*db.Email, error)
	SetTracking(ctx context.Context, id, trackingID string) error
	SetScheduleTask(ctx context.Context, id, taskID string) error
	Finish(ctx context.Context, id, status string, sentAt *time.Time) error
	// Cancel moves a scheduled email of userID to "cancelled" (ErrNotFound otherwise).
	Cancel(ctx context.Context, userID, id string) (*db.Email, error)
	CountSentSince(ctx context.Context, userID string, since time.Time) (int, error)
	// SentAnother says whether the user has a sent email other than this one: false means this one is their first.
	SentAnother(ctx context.Context, userID, exceptID string) (bool, error)
	// CountOriginal counts the user's emails that carry the imported file and left or are about to (sent,
	// sending, scheduled), over the whole life of the account.
	CountOriginal(ctx context.Context, userID string) (int, error)
	// LastBuiltSend returns how many of the user's emails left with the CV built in 9itchi (not the imported
	// file) and when the last one did (nil when none; the last change of the row when the time it left is missing).
	LastBuiltSend(ctx context.Context, userID string) (int, *time.Time, error)
	// UpdateScheduled writes the editable part of a scheduled email of userID (ErrNotFound when it is not, or no
	// longer, scheduled).
	UpdateScheduled(ctx context.Context, userID, id string, edit db.EmailEdit) (*db.Email, error)
}

type emailRepository struct {
	store *Store
}

func NewEmailRepository(store *Store) EmailRepository {
	return &emailRepository{store: store}
}

const emailColumns = `id, user_id, job_id, resume_id, recipient, subject, body, status, scheduled_at, sent_at, tracking_id, schedule_task_id, created_at, updated_at, cv_original`

type rowScanner interface{ Scan(dest ...any) error }

func scanEmail(r rowScanner) (*db.Email, error) {
	var e db.Email
	err := r.Scan(&e.ID, &e.UserID, &e.JobID, &e.ResumeID, &e.Recipient, &e.Subject, &e.Body, &e.Status,
		&e.ScheduledAt, &e.SentAt, &e.TrackingID, &e.ScheduleTaskID, &e.CreatedAt, &e.UpdatedAt, &e.CVOriginal)
	if err != nil {
		return nil, MapError(err)
	}
	return &e, nil
}

func (r *emailRepository) Create(ctx context.Context, e *db.Email) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO emails (user_id, job_id, resume_id, recipient, subject, body, status, scheduled_at, tracking_id, cv_original)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, gen_random_uuid(), $9)
		RETURNING `+emailColumns,
		e.UserID, e.JobID, e.ResumeID, e.Recipient, e.Subject, e.Body, e.Status, e.ScheduledAt, e.CVOriginal))
}

func (r *emailRepository) Get(ctx context.Context, userID, id string) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+emailColumns+` FROM emails WHERE id = $1 AND user_id = $2`, id, userID))
}

func (r *emailRepository) GetByID(ctx context.Context, id string) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+emailColumns+` FROM emails WHERE id = $1`, id))
}

func (r *emailRepository) ListByUser(ctx context.Context, userID string, limit int) ([]db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx,
		`SELECT `+emailColumns+` FROM emails WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Email{}
	for rows.Next() {
		e, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, MapError(rows.Err())
}

func (r *emailRepository) Claim(ctx context.Context, id string) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx, `
		UPDATE emails SET status = 'sending', updated_at = now()
		WHERE id = $1 AND status IN ('draft', 'scheduled')
		RETURNING `+emailColumns, id))
}

func (r *emailRepository) SetTracking(ctx context.Context, id, trackingID string) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `UPDATE emails SET tracking_id = $2, updated_at = now() WHERE id = $1`, id, trackingID)
	return MapError(err)
}

func (r *emailRepository) SetScheduleTask(ctx context.Context, id, taskID string) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `UPDATE emails SET schedule_task_id = $2, updated_at = now() WHERE id = $1`, id, taskID)
	return MapError(err)
}

func (r *emailRepository) Finish(ctx context.Context, id, status string, sentAt *time.Time) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `UPDATE emails SET status = $2, sent_at = $3, updated_at = now() WHERE id = $1`, id, status, sentAt)
	return MapError(err)
}

func (r *emailRepository) Cancel(ctx context.Context, userID, id string) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx, `
		UPDATE emails SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'scheduled'
		RETURNING `+emailColumns, id, userID))
}

func (r *emailRepository) CountSentSince(ctx context.Context, userID string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT count(*) FROM emails WHERE user_id = $1 AND status = 'sent' AND sent_at >= $2`, userID, since).Scan(&n)
	return n, MapError(err)
}

func (r *emailRepository) SentAnother(ctx context.Context, userID, exceptID string) (bool, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var other bool
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM emails WHERE user_id = $1 AND status = 'sent' AND id <> $2)`, userID, exceptID).Scan(&other)
	return other, MapError(err)
}

func (r *emailRepository) CountOriginal(ctx context.Context, userID string) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT count(*) FROM emails WHERE user_id = $1 AND cv_original AND status IN ('sent', 'sending', 'scheduled')`, userID).Scan(&n)
	return n, MapError(err)
}

func (r *emailRepository) LastBuiltSend(ctx context.Context, userID string) (int, *time.Time, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	var last *time.Time
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT count(*), max(coalesce(sent_at, updated_at)) FROM emails WHERE user_id = $1 AND status = 'sent' AND NOT cv_original`, userID).Scan(&n, &last)
	return n, last, MapError(err)
}

func (r *emailRepository) UpdateScheduled(ctx context.Context, userID, id string, edit db.EmailEdit) (*db.Email, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanEmail(r.store.Conn(ctx).QueryRow(ctx, `
		UPDATE emails SET recipient = $3, subject = $4, body = $5, resume_id = $6, scheduled_at = $7, schedule_task_id = $8, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'scheduled'
		RETURNING `+emailColumns, id, userID, edit.Recipient, edit.Subject, edit.Body, edit.ResumeID, edit.ScheduledAt, edit.ScheduleTaskID))
}
