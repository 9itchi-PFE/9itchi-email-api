package db

import "time"

// Application maps table applications.
type Application struct {
	ID      string  `db:"id" json:"id"`
	UserID  string  `db:"user_id" json:"user_id"`
	JobID   string  `db:"job_id" json:"job_id"`
	EmailID *string `db:"email_id" json:"email_id"`
	Status  string  `db:"status" json:"status"`
	// JobTitle and Company are copied from the offer when the student applies.
	JobTitle string `db:"job_title" json:"job_title"`
	Company  string `db:"company" json:"company"`
	// Via is "gmail" (sent by 9itchi from the student's Gmail) or "self" (sent by the student elsewhere).
	Via       string    `db:"via" json:"via"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Application statuses and channels.
const (
	AppScheduled = "scheduled"
	AppSent      = "sent"
	AppOpened    = "opened"
	AppReplied   = "replied"
	AppWithdrawn = "withdrawn"
	AppClosed    = "closed"

	ViaGmail = "gmail"
	ViaSelf  = "self"
)
