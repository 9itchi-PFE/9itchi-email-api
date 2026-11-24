package repositories

import (
	"context"

	"9itchi-email-api/models/db"
)

// GmailRepository is the only layer that touches PostgreSQL for gmail data.
type GmailRepository interface {
	Upsert(ctx context.Context, c *db.GmailConnection) error
	Get(ctx context.Context, userID string) (*db.GmailConnection, error)
	Delete(ctx context.Context, userID string) error
}

type gmailRepository struct {
	store *Store
}

func NewGmailRepository(store *Store) GmailRepository {
	return &gmailRepository{store: store}
}

func (r *gmailRepository) Upsert(ctx context.Context, c *db.GmailConnection) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `
		INSERT INTO gmail_connections (user_id, email, encrypted_token, scopes) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email, encrypted_token = EXCLUDED.encrypted_token,
			scopes = EXCLUDED.scopes, connected_at = now(), updated_at = now()`,
		c.UserID, c.Email, c.EncryptedToken, c.Scopes)
	return MapError(err)
}

func (r *gmailRepository) Get(ctx context.Context, userID string) (*db.GmailConnection, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var c db.GmailConnection
	err := r.store.Conn(ctx).QueryRow(ctx, `
		SELECT id, user_id, email, encrypted_token, scopes, connected_at, created_at, updated_at
		FROM gmail_connections WHERE user_id = $1`, userID).
		Scan(&c.ID, &c.UserID, &c.Email, &c.EncryptedToken, &c.Scopes, &c.ConnectedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, MapError(err)
	}
	return &c, nil
}

func (r *gmailRepository) Delete(ctx context.Context, userID string) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `DELETE FROM gmail_connections WHERE user_id = $1`, userID)
	return MapError(err)
}
