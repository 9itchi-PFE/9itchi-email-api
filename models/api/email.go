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
	LetterFields
}

// LetterFields say how the cover letter leaves as a PDF next to the CV. LetterFilename is its name without
// extension (empty: the default name); AttachLetter false sends no letter (absent: one is sent); Language is the
// letter's (absent: ar for a body in Arabic, else fr).
type LetterFields struct {
	LetterFilename string `json:"letter_filename" binding:"max=120"`
	AttachLetter   *bool  `json:"attach_letter"`
	Language       string `json:"language" binding:"omitempty,oneof=fr en ar"`
}

// AttachmentNamesQuery is GET /emails/attachment-names?resume_id=&language=.
type AttachmentNamesQuery struct {
	ResumeID string `form:"resume_id" binding:"omitempty,uuid"`
	Language string `form:"language" binding:"omitempty,oneof=fr en ar"`
}

// AttachmentNamesResponse are the names the files of an application get: the CV's (with extension, "" without a
// CV) and the default name of the cover letter (without extension).
type AttachmentNamesResponse struct {
	CVFilename     string `json:"cv_filename"`
	LetterFilename string `json:"letter_filename"`
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
// month (Limit nil: no limit). Origin "score" is a quick score not yet read as an analysis: its advice is withheld
// (empty) until POST /applications/match is allowed by the plan.
type MatchResponse struct {
	Score           int           `json:"score"`
	Level           string        `json:"level"`
	Strengths       []string      `json:"strengths"`
	Gaps            []string      `json:"gaps"`
	Advice          string        `json:"advice"`
	Origin          string        `json:"origin"`
	Keywords        MatchKeywords `json:"keywords"`
	KeywordCoverage int           `json:"keyword_coverage"`
	CreatedAt       time.Time     `json:"created_at"`
	Limit           *int          `json:"limit"`
	Used            int           `json:"used"`
}

// ScoreRequest is the body of POST /applications/scores: quick AI scores of up to 10 offers for one CV.
type ScoreRequest struct {
	ResumeID string   `json:"resume_id" binding:"required,uuid"`
	Language string   `json:"language" binding:"omitempty,oneof=fr en ar"`
	JobIDs   []string `json:"job_ids" binding:"required,min=1,max=10,dive,uuid"`
}

// ScoreQuery is GET /applications/scores?resume_id=&job_ids=a,b&language=: the scores already made, nothing asked.
type ScoreQuery struct {
	ResumeID string `form:"resume_id" binding:"required,uuid"`
	Language string `form:"language" binding:"omitempty,oneof=fr en ar"`
	// JobIDs is a comma-separated list of up to 10 offer ids.
	JobIDs string `form:"job_ids" binding:"required"`
}

// Status of one offer in a score answer.
const (
	ScoreOK     = "ok"     // scored now, counted in today's limit
	ScoreCached = "cached" // read before (score or analysis), not counted
	ScoreQuota  = "quota"  // today's limit of the plan is reached: not scored
	ScoreError  = "error"  // the offer or the AI failed: not scored, not counted
)

// ScoreItem is the score of one offer: Reasons are up to 3 of the reading's strengths then gaps. Score and Level are
// empty unless Status is ok or cached.
type ScoreItem struct {
	JobID   string   `json:"job_id"`
	Score   int      `json:"score"`
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
	Status  string   `json:"status"`
}

// ScoreAllowance is the plan's daily limit of scores (Tunis day; nil: no limit) and how many were made today.
type ScoreAllowance struct {
	Limit *int `json:"limit"`
	Used  int  `json:"used"`
}

// ScoreResponse answers POST and GET /applications/scores. GET gives only the offers already scored, without
// allowance.
type ScoreResponse struct {
	Items     []ScoreItem     `json:"items"`
	Allowance *ScoreAllowance `json:"allowance,omitempty"`
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
// AttachCV false sends it without a CV; true attaches ResumeID, or the CV it already carries. LetterFilename ""
// gives the cover letter its default name back.
type UpdateEmailRequest struct {
	To             *string    `json:"to" binding:"omitempty,email,max=254"`
	Subject        *string    `json:"subject" binding:"omitempty,max=200"`
	Body           *string    `json:"body" binding:"omitempty,max=10000"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
	AttachCV       *bool      `json:"attach_cv"`
	ResumeID       *string    `json:"resume_id" binding:"omitempty,uuid"`
	LetterFilename *string    `json:"letter_filename" binding:"omitempty,max=120"`
	AttachLetter   *bool      `json:"attach_letter"`
	Language       *string    `json:"language" binding:"omitempty,oneof=fr en ar"`
}
