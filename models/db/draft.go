package db

import "time"

// AIDraft maps table ai_drafts: a letter the AI wrote for one offer and one CV.
type AIDraft struct {
	ID        string
	UserID    string
	JobID     string
	Language  string
	Tone      string
	InputHash string
	Subject   string
	Body      string
	CreatedAt time.Time
}
