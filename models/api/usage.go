package api

// UsageItem is one limit of the plan and how much of it is used. Limit is nil when the plan sets none.
type UsageItem struct {
	Limit *int `json:"limit"`
	Used  int  `json:"used"`
}

// UserUsageResponse is what the team sees of a student's use of what the plan allows in email-api: the numbers
// the student sees, counted over the same period as the limit (month in UTC, Tunis day, or the life of the
// account). Nothing of what was written is returned.
type UserUsageResponse struct {
	// Applications: Mode "plan" counts the month, "original" the life of the account (no subscription).
	Applications AllowanceResponse `json:"applications"`
	AIDrafts     UsageItem         `json:"ai_drafts"`
	AIMatches    UsageItem         `json:"ai_matches"`
	// AIScores and RecruiterSearches are counted per Tunis day.
	AIScores          UsageItem `json:"ai_scores"`
	RecruiterSearches UsageItem `json:"recruiter_searches"`
}
