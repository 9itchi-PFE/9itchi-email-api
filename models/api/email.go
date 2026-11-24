package api

import "time"

// DraftEmailRequest is the body of POST /emails/draft: the offer to apply to and the CV to present.
type DraftEmailRequest struct {
	JobID    string `json:"job_id" binding:"required,uuid"`
	ResumeID string `json:"resume_id" binding:"omitempty,uuid"`
	Language string `json:"language" binding:"omitempty,oneof=fr en ar"`
	Tone     string `json:"tone" binding:"omitempty,oneof=formal friendly"`
}

// DraftEmailResponse is the AI draft; nothing is stored until the user sends or schedules it.
type DraftEmailResponse struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// SendEmailRequest is the body of POST /emails.
type SendEmailRequest struct {
	JobID    string `json:"job_id" binding:"omitempty,uuid"`
	ResumeID string `json:"resume_id" binding:"omitempty,uuid"`
	To       string `json:"to" binding:"required,email,max=254"`
	Subject  string `json:"subject" binding:"required,max=200"`
	Body     string `json:"body" binding:"required,max=10000"`
}

// ScheduleEmailRequest is the body of POST /emails/schedule.
type ScheduleEmailRequest struct {
	SendEmailRequest
	ScheduledAt time.Time `json:"scheduled_at" binding:"required"`
}
