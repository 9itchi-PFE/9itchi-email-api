package db

import "time"

// Email maps table emails.
type Email struct {
	ID          string     `db:"id" json:"id"`
	UserID      string     `db:"user_id" json:"user_id"`
	JobID       *string    `db:"job_id" json:"job_id"`
	ResumeID    *string    `db:"resume_id" json:"resume_id"`
	Subject     string     `db:"subject" json:"subject"`
	Body        string     `db:"body" json:"body"`
	Status      string     `db:"status" json:"status"`
	ScheduledAt *time.Time `db:"scheduled_at" json:"scheduled_at"`
	TrackingID  string     `db:"tracking_id" json:"tracking_id"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}
