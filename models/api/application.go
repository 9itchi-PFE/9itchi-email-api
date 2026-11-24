package api

import "time"

// ApplyRequest is the body of POST /applications.
//
// via "gmail" (default) sends the email with the CV from the student's Gmail, now or at scheduled_at; the
// recipient is the address the offer takes applications at, and to is only used when the offer has none.
// via "self" only records that the student applied on their own (another site, their own mailbox).
type ApplyRequest struct {
	JobID       string     `json:"job_id" binding:"required,uuid"`
	ResumeID    string     `json:"resume_id" binding:"omitempty,uuid"`
	Via         string     `json:"via" binding:"omitempty,oneof=gmail self"`
	To          string     `json:"to" binding:"omitempty,email,max=254"`
	Subject     string     `json:"subject" binding:"max=200"`
	Body        string     `json:"body" binding:"max=10000"`
	ScheduledAt *time.Time `json:"scheduled_at"`
}

// UpdateApplicationRequest is the body of PATCH /applications/:id: what the student tells us happened.
// "replied": the company answered; "withdrawn": no longer interested; "closed": the company said no;
// "sent": back to waiting.
type UpdateApplicationRequest struct {
	Status string `json:"status" binding:"required,oneof=sent replied withdrawn closed"`
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
	ID      string  `json:"id"`
	JobID   string  `json:"job_id"`
	EmailID *string `json:"email_id"`
	// Status is scheduled, sent, opened, replied, withdrawn, closed, or failed when the email could not leave.
	Status    string      `json:"status"`
	JobTitle  string      `json:"job_title"`
	Company   string      `json:"company"`
	Via       string      `json:"via"`
	Email     *EmailBrief `json:"email"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// ApplicationsResponse is the body of GET /applications.
type ApplicationsResponse struct {
	Items []ApplicationResponse `json:"items"`
}

// EmailContextResponse says what an email was sent for, so that a notification about it can name the offer.
type EmailContextResponse struct {
	EmailID       string `json:"email_id"`
	UserID        string `json:"user_id"`
	ApplicationID string `json:"application_id"`
	JobID         string `json:"job_id"`
	JobTitle      string `json:"job_title"`
	Company       string `json:"company"`
}
