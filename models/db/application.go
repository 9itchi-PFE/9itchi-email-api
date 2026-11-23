package db

import "time"

// Application maps table applications.
type Application struct {
	ID        string    `db:"id" json:"id"`
	UserID    string    `db:"user_id" json:"user_id"`
	JobID     string    `db:"job_id" json:"job_id"`
	EmailID   *string   `db:"email_id" json:"email_id"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
