package db

import "time"

// GmailConnection maps table gmail_connections.
type GmailConnection struct {
	ID             string    `db:"id" json:"id"`
	UserID         string    `db:"user_id" json:"user_id"`
	Email          string    `db:"email" json:"email"`
	EncryptedToken string    `db:"encrypted_token" json:"encrypted_token"`
	Scopes         []string  `db:"scopes" json:"scopes"`
	ConnectedAt    time.Time `db:"connected_at" json:"connected_at"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}
