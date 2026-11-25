package db

import "time"

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
	CreatedAt time.Time
}
