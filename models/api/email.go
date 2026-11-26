package api

import "time"

// DraftEmailRequest is the body of POST /emails/draft: the offer to apply to and the CV to present.
type DraftEmailRequest struct {
	JobID    string `json:"job_id" binding:"required,uuid"`
	ResumeID string `json:"resume_id" binding:"omitempty,uuid"`
	Language string `json:"language" binding:"omitempty,oneof=fr en ar"`
	Tone     string `json:"tone" binding:"omitempty,oneof=formal friendly"`
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

// DraftFromLinkRequest is the body of POST /emails/draft-from-link: an offer the student found elsewhere, by its
// link. OfferText is the offer as they pasted it, when its site cannot be read.
type DraftFromLinkRequest struct {
	URL       string `json:"url" binding:"required,max=2048"`
	OfferText string `json:"offer_text" binding:"max=40000"`
	ResumeID  string `json:"resume_id" binding:"omitempty,uuid"`
	Language  string `json:"language" binding:"omitempty,oneof=fr en ar"`
	Tone      string `json:"tone" binding:"omitempty,oneof=formal friendly"`
	Fresh     bool   `json:"fresh"`
}

// LinkOffer is the offer as it was read: where it comes from ("9itchi", "linkedin" or the host of the link).
type LinkOffer struct {
	Source  string `json:"source"`
	Title   string `json:"title"`
	Company string `json:"company"`
}

// DraftFromLinkResponse is the AI's letter for the offer behind a link. JobID is set when the link was a 9itchi
// offer.
type DraftFromLinkResponse struct {
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	Limit   *int      `json:"limit"`
	Used    int       `json:"used"`
	JobID   *string   `json:"job_id"`
	Offer   LinkOffer `json:"offer"`
}

// UpdateEmailRequest is the body of PATCH /emails/:id: what changes in a scheduled email (absent: unchanged).
// AttachCV false sends it without a CV; true attaches ResumeID, or the CV it already carries.
type UpdateEmailRequest struct {
	To          *string    `json:"to" binding:"omitempty,email,max=254"`
	Subject     *string    `json:"subject" binding:"omitempty,max=200"`
	Body        *string    `json:"body" binding:"omitempty,max=10000"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	AttachCV    *bool      `json:"attach_cv"`
	ResumeID    *string    `json:"resume_id" binding:"omitempty,uuid"`
}
