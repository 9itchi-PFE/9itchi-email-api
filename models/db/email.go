package db

import "time"

// Email maps table emails.
type Email struct {
	ID          string     `db:"id" json:"id"`
	UserID      string     `db:"user_id" json:"user_id"`
	JobID       *string    `db:"job_id" json:"job_id"`
	ResumeID    *string    `db:"resume_id" json:"resume_id"`
	Recipient   string     `db:"recipient" json:"recipient"`
	Subject     string     `db:"subject" json:"subject"`
	Body        string     `db:"body" json:"body"`
	Status      string     `db:"status" json:"status"`
	ScheduledAt *time.Time `db:"scheduled_at" json:"scheduled_at"`
	SentAt      *time.Time `db:"sent_at" json:"sent_at"`
	TrackingID  string     `db:"tracking_id" json:"tracking_id"`
	// CVOriginal: the attachment is the file the student imported (an application without a subscription).
	CVOriginal bool `db:"cv_original" json:"cv_original"`
	// LetterFilename is the name of the cover letter PDF without extension (nil: the default name, read when the
	// email leaves). AttachLetter says the letter leaves as a PDF too; LetterLanguage is its language (nil: read
	// from the body).
	LetterFilename *string `db:"letter_filename" json:"letter_filename"`
	AttachLetter   bool    `db:"attach_letter" json:"attach_letter"`
	LetterLanguage *string `db:"letter_language" json:"letter_language"`
	// ScheduleTaskID is the schedule-api task to cancel with a scheduled email.
	ScheduleTaskID *string   `db:"schedule_task_id" json:"-"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

const (
	EmailDraft     = "draft"
	EmailScheduled = "scheduled"
	EmailSending   = "sending"
	EmailSent      = "sent"
	EmailFailed    = "failed"
	EmailCancelled = "cancelled"
)

// EmailEdit is the whole editable part of a scheduled email, written at once.
type EmailEdit struct {
	Recipient      string
	Subject        string
	Body           string
	ResumeID       *string
	ScheduledAt    *time.Time
	ScheduleTaskID *string
	LetterFilename *string
	AttachLetter   bool
	LetterLanguage *string
}
