package api

import "time"

// RecruiterSearchRequest is the body of POST /recruiters/search: a CV (its skills), what the student looks for in
// their own words, the contract, optional keywords, where (country ISO 3166 alpha-2, default TN; city) and how many
// people to find (1 to 10, default 5).
type RecruiterSearchRequest struct {
	ResumeID string   `json:"resume_id" binding:"omitempty,uuid"`
	Goal     string   `json:"goal" binding:"max=300"`
	Contract string   `json:"contract" binding:"omitempty,oneof=pfe stage alternance cdi cdd"`
	Keywords []string `json:"keywords" binding:"max=10,dive,max=40"`
	Country  string   `json:"country" binding:"max=2"`
	City     string   `json:"city" binding:"max=80"`
	Contacts int      `json:"contacts"`
}

// CompanyRecruiterRequest is the body of POST /recruiters/company: the company of an offer (its name, and its
// domain or site when known) and what the student wants there (the offer title).
type CompanyRecruiterRequest struct {
	Company string `json:"company" binding:"required,max=120"`
	Domain  string `json:"domain" binding:"max=255"`
	Goal    string `json:"goal" binding:"max=300"`
}

// RecruiterItem is one recruiter found for the student.
type RecruiterItem struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Title           string    `json:"title"`
	Company         string    `json:"company"`
	Domain          string    `json:"domain"`
	Email           string    `json:"email"`
	LinkedInURL     string    `json:"linkedin_url"`
	Confidence      int       `json:"confidence"`
	Verified        bool      `json:"verified"`
	CompanySummary  string    `json:"company_summary"`
	MatchedKeywords []string  `json:"matched_keywords"`
	LogoURL         string    `json:"logo_url"` // http(s) only, "" when unknown
	CreatedAt       time.Time `json:"created_at"`
}

// RecruiterAllowance is the plan's daily limit of searches (Limit and Remaining nil: no limit), how many were made
// today and when the day ends (midnight, Tunis time).
type RecruiterAllowance struct {
	Limit     *int      `json:"limit"`
	Used      int       `json:"used"`
	Remaining *int      `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at"`
}

// ProspectStats tells where the companies and people of a search came from: the shared pool, or found now.
type ProspectStats struct {
	CompaniesFromPool   int `json:"companies_from_pool"`
	CompaniesDiscovered int `json:"companies_discovered"`
	ContactsFromPool    int `json:"contacts_from_pool"`
	ContactsFound       int `json:"contacts_found"`
}

// RecruiterSearchResponse is what a search found. Cached says the same search was already made today: it was given
// back without searching again nor counting it (its stats are then zero). The provider stays internal.
type RecruiterSearchResponse struct {
	SearchID  string             `json:"search_id"`
	Cached    bool               `json:"cached"`
	Items     []RecruiterItem    `json:"items"`
	Allowance RecruiterAllowance `json:"allowance"`
	Stats     ProspectStats      `json:"stats"`
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
