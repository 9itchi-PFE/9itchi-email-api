package repositories

import (
	"context"

	"9itchi-email-api/models/db"
)

// ApplicationRepository is the only layer that touches PostgreSQL for application data.
type ApplicationRepository interface {
	// Upsert creates the application of (user, job) or relinks it to the new email.
	Upsert(ctx context.Context, a *db.Application) (*db.Application, error)
	ListByUser(ctx context.Context, userID string, limit int) ([]db.Application, error)
	// GetByJob returns the application of userID to jobID (ErrNotFound when there is none).
	GetByJob(ctx context.Context, userID, jobID string) (*db.Application, error)
	// GetByEmail returns the application an email was sent for (ErrNotFound when it belongs to none).
	GetByEmail(ctx context.Context, emailID string) (*db.Application, error)
	// Get returns one application of userID (ErrNotFound otherwise).
	Get(ctx context.Context, userID, id string) (*db.Application, error)
	// SetStatus changes the status of one application of userID (ErrNotFound otherwise).
	SetStatus(ctx context.Context, userID, id, status string) (*db.Application, error)
}

type applicationRepository struct {
	store *Store
}

func NewApplicationRepository(store *Store) ApplicationRepository {
	return &applicationRepository{store: store}
}

const applicationColumns = `id, user_id, job_id, email_id, status, job_title, company, via, created_at, updated_at`

func scanApplication(r rowScanner) (*db.Application, error) {
	var a db.Application
	if err := r.Scan(&a.ID, &a.UserID, &a.JobID, &a.EmailID, &a.Status, &a.JobTitle, &a.Company, &a.Via, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, MapError(err)
	}
	return &a, nil
}

func (r *applicationRepository) Upsert(ctx context.Context, a *db.Application) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO applications (user_id, job_id, email_id, status, job_title, company, via) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, job_id) DO UPDATE SET email_id = EXCLUDED.email_id, status = EXCLUDED.status,
			job_title = EXCLUDED.job_title, company = EXCLUDED.company, via = EXCLUDED.via, updated_at = now()
		RETURNING `+applicationColumns, a.UserID, a.JobID, a.EmailID, a.Status, a.JobTitle, a.Company, a.Via))
}

func (r *applicationRepository) GetByJob(ctx context.Context, userID, jobID string) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE user_id = $1 AND job_id = $2`, userID, jobID))
}

func (r *applicationRepository) GetByEmail(ctx context.Context, emailID string) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE email_id = $1`, emailID))
}

func (r *applicationRepository) Get(ctx context.Context, userID, id string) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE user_id = $1 AND id = $2`, userID, id))
}

func (r *applicationRepository) SetStatus(ctx context.Context, userID, id, status string) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx, `
		UPDATE applications SET status = $3, updated_at = now() WHERE user_id = $1 AND id = $2
		RETURNING `+applicationColumns, userID, id, status))
}

func (r *applicationRepository) ListByUser(ctx context.Context, userID string, limit int) ([]db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Application{}
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, MapError(rows.Err())
}
