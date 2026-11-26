package api

import "time"

// DraftEmailRequest is the body of POST /emails/draft: the offer to apply to and the CV to present.
type DraftEmailRequest struct {
	JobID    string `json:"job_id" binding:"required,uuid"`
	ResumeID string `json:"resume_id" binding:"omitempty,uuid"`
	Language string `json:"language" binding:"omitempty,oneof=fr en ar"`
	Tone     string `json:"tone" binding:"omitempty,oneof=formal friendly"`
	// Goal is what the student wants from the email (« Objectif » on the outreach page); the AI writes the letter
	// around it. A letter written for another goal is not given back.
	Goal string `json:"goal" binding:"max=300"`
	// Fresh asks the AI for a new letter (« Réécrire »); otherwise the last one written for the same offer, CV,
	// language and tone comes back without being counted.
	Fresh bool `json:"fresh"`
}

// DraftEmailResponse is the AI's letter, with what is left of the plan's AI letters this month (Limit nil: no
// limit). The letter is kept for the student only; it leaves only when they send it.
type DraftEmailResponse struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Limit   *int   `json:"limit"`
	Used    int    `json:"used"`
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

// MatchRequest asks how well one CV fits one offer (POST /applications/match), or whether that was already asked
// (GET, same fields as query parameters).
type MatchRequest struct {
	JobID    string `json:"job_id" form:"job_id" binding:"required,uuid"`
	ResumeID string `json:"resume_id" form:"resume_id" binding:"required,uuid"`
	Language string `json:"language" form:"language" binding:"omitempty,oneof=fr en ar"`
}

// MatchResponse is the AI's reading of the CV against the offer, with what is left of the plan's analyses this
// month (Limit nil: no limit).
type MatchResponse struct {
	Score           int           `json:"score"`
	Level           string        `json:"level"`
	Strengths       []string      `json:"strengths"`
	Gaps            []string      `json:"gaps"`
	Advice          string        `json:"advice"`
	Keywords        MatchKeywords `json:"keywords"`
	KeywordCoverage int           `json:"keyword_coverage"`
	CreatedAt       time.Time     `json:"created_at"`
	Limit           *int          `json:"limit"`
	Used            int           `json:"used"`
}

// MatchKeywords are the offer's keywords, in the order of the offer, split by whether the CV mentions them.
type MatchKeywords struct {
	Matched []string `json:"matched"`
	Missing []string `json:"missing"`
}
