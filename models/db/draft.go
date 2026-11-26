package db

import "time"

// AIDraft maps table ai_drafts: a letter the AI wrote for one offer and one CV. The offer is a 9itchi offer (JobID)
// or one found elsewhere, given by its normalized link (SourceURL).
type AIDraft struct {
	ID        string
	UserID    string
	JobID     *string
	SourceURL *string
	Language  string
	Tone      string
	InputHash string
	Subject   string
	Body      string
	CreatedAt time.Time
}
