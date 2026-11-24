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
}

type applicationRepository struct {
	store *Store
}

func NewApplicationRepository(store *Store) ApplicationRepository {
	return &applicationRepository{store: store}
}

const applicationColumns = `id, user_id, job_id, email_id, status, created_at, updated_at`

func scanApplication(r rowScanner) (*db.Application, error) {
	var a db.Application
	if err := r.Scan(&a.ID, &a.UserID, &a.JobID, &a.EmailID, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, MapError(err)
	}
	return &a, nil
}

func (r *applicationRepository) Upsert(ctx context.Context, a *db.Application) (*db.Application, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanApplication(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO applications (user_id, job_id, email_id, status) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, job_id) DO UPDATE SET email_id = EXCLUDED.email_id, status = EXCLUDED.status, updated_at = now()
		RETURNING `+applicationColumns, a.UserID, a.JobID, a.EmailID, a.Status))
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
