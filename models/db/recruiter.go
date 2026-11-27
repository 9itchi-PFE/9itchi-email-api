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
	// Stats is what a fresh search took from the pool and from the providers (nil: not stored, written as NULL).
	Stats *SearchStats
}

// SearchStats are the counts of one fresh search (recruiter_searches columns, 0014).
type SearchStats struct {
	CompaniesFromPool   int
	CompaniesDiscovered int
	ContactsFromPool    int
	ContactsFound       int
}

// PoolCounts are the live numbers of the prospect pool. Contact counts leave opted-out people out; Fresh counts
// the people the pipeline still uses (checked or updated since a time); BySource counts people per source.
type PoolCounts struct {
	Companies int
	Contacts  int
	Verified  int
	HR        int
	Fresh     int
	BySource  map[string]int
}

// SearchTotals sums the fresh searches with stored stats. PoolOnly counts the searches no provider was asked for
// (people from the pool only).
type SearchTotals struct {
	Searches         int
	ContactsFromPool int
	ContactsFound    int
	PoolOnly         int
}

// SearchDay is SearchTotals for one UTC day (YYYY-MM-DD).
type SearchDay struct {
	Day string
	SearchTotals
}

// ProviderCreditsDay is what one provider spent on one UTC day (YYYY-MM-DD).
type ProviderCreditsDay struct {
	Day      string
	Provider string
	Credits  int
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
	// ContactID is the pool person the row came from ("" on rows made before the pool, or once that person left it).
	ContactID string
	// CompanySummary is a short description of the company from the pool; MatchedKeywords the terms of the search
	// found in what the pool knows of the company. Verified: the provider said the address is valid.
	CompanySummary  string
	MatchedKeywords []string
	Verified        bool
	// LogoURL is the logo of the pool company of ContactID, read with the row ("" when unknown or no contact).
	LogoURL string
}
