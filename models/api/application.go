package api

import "time"

// ApplyRequest is the body of POST /applications: one-click apply (email + CV), now or at scheduled_at.
type ApplyRequest struct {
	JobID       string     `json:"job_id" binding:"required,uuid"`
	ResumeID    string     `json:"resume_id" binding:"omitempty,uuid"`
	To          string     `json:"to" binding:"required,email,max=254"`
	Subject     string     `json:"subject" binding:"required,max=200"`
	Body        string     `json:"body" binding:"required,max=10000"`
	ScheduledAt *time.Time `json:"scheduled_at"`
}

// EmailBrief is the email of an application as shown in the history list.
type EmailBrief struct {
	ID          string     `json:"id"`
	Recipient   string     `json:"recipient"`
	Subject     string     `json:"subject"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	SentAt      *time.Time `json:"sent_at"`
}

// ApplicationResponse is one tracked application.
type ApplicationResponse struct {
	ID        string      `json:"id"`
	JobID     string      `json:"job_id"`
	EmailID   *string     `json:"email_id"`
	Status    string      `json:"status"`
	Email     *EmailBrief `json:"email"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// ApplicationsResponse is the body of GET /applications.
type ApplicationsResponse struct {
	Items []ApplicationResponse `json:"items"`
}
