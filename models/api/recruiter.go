package api

import "time"

// RecruiterSearchRequest is the body of POST /recruiters/search: at least one of a CV (its skills), keywords, a
// company or a domain.
type RecruiterSearchRequest struct {
	ResumeID string   `json:"resume_id" binding:"omitempty,uuid"`
	Keywords []string `json:"keywords" binding:"max=10,dive,max=60"`
	Company  string   `json:"company" binding:"max=120"`
	Domain   string   `json:"domain" binding:"max=253"`
	Location string   `json:"location" binding:"max=120"`
}

// RecruiterItem is one recruiter found for the student.
type RecruiterItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Company     string    `json:"company"`
	Domain      string    `json:"domain"`
	Email       string    `json:"email"`
	LinkedInURL string    `json:"linkedin_url"`
	Confidence  int       `json:"confidence"`
	CreatedAt   time.Time `json:"created_at"`
}

// RecruiterAllowance is the plan's daily limit of searches (Limit and Remaining nil: no limit), how many were made
// today and when the day ends (midnight, Tunis time).
type RecruiterAllowance struct {
	Limit     *int      `json:"limit"`
	Used      int       `json:"used"`
	Remaining *int      `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at"`
}

// RecruiterSearchResponse is what a search found. Cached says the same search was already made today: it was given
// back without asking the provider again nor counting it.
type RecruiterSearchResponse struct {
	SearchID  string             `json:"search_id"`
	Provider  string             `json:"provider"`
	Cached    bool               `json:"cached"`
	Items     []RecruiterItem    `json:"items"`
	Allowance RecruiterAllowance `json:"allowance"`
}

// RecruitersResponse is every recruiter found for the student, each person once (newest first).
type RecruitersResponse struct {
	Items []RecruiterItem `json:"items"`
}

// RecruiterDraftRequest is the body of POST /recruiters/:id/draft: a spontaneous application to that recruiter.
// Goal says what the student looks for (an internship in data, a PFE in Sfax...).
type RecruiterDraftRequest struct {
	ResumeID string `json:"resume_id" binding:"omitempty,uuid"`
	Language string `json:"language" binding:"omitempty,oneof=fr en ar"`
	Tone     string `json:"tone" binding:"omitempty,oneof=formal friendly"`
	Goal     string `json:"goal" binding:"max=500"`
	Fresh    bool   `json:"fresh"`
}
