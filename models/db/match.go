package db

import "time"

// MatchKeywords is the jsonb of job_matches.keywords.
type MatchKeywords struct {
	Matched []string `json:"matched"`
	Missing []string `json:"missing"`
}

// JobMatch maps table job_matches: the AI's reading of how well one CV fits one offer.
type JobMatch struct {
	ID        string
	UserID    string
	JobID     string
	ResumeID  string
	Language  string
	InputHash string
	Score     int
	Level     string
	Strengths []string
	Gaps      []string
	Advice    string
	Keywords  MatchKeywords
	// KeywordCoverage is the share of the offer's keywords the CV mentions, 0 to 100.
	KeywordCoverage int
	CreatedAt       time.Time
}
