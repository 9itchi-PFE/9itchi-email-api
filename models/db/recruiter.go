package db

import "time"

// RecruiterSearch maps table recruiter_searches: one search a student ran, the query as normalized (JSON) and its
// digest, and the provider that answered.
type RecruiterSearch struct {
	ID          string
	UserID      string
	Query       []byte
	QueryHash   string
	Provider    string
	ResultCount int
	CreatedAt   time.Time
}

// Recruiter maps table recruiters: one person a search found. DedupeKey identifies the same person across
// searches (the address, else the LinkedIn profile, else name and company, lower case).
type Recruiter struct {
	ID          string
	UserID      string
	SearchID    string
	DedupeKey   string
	Name        string
	Title       string
	Company     string
	Domain      string
	Email       string
	LinkedInURL string
	Confidence  int
	Provider    string
	CreatedAt   time.Time
}
