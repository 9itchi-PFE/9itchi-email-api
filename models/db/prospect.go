package db

import "time"

// Company maps table companies: one company of the shared prospect pool (platform data, no user). SearchText is
// name, industry, description and keywords, lower case without accents, built in Go; the pool search matches it.
type Company struct {
	ID          string
	Domain      string
	Name        string
	Description string
	Industry    string
	SizeRange   string
	Country     string
	City        string
	Keywords    []string
	SearchText  string
	Source      string
	LogoURL     string // "" = unknown; a save without one keeps the stored logo
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Contact maps table contacts: one person (or recruiting mailbox) of a pool company. Email is unique, any case.
type Contact struct {
	ID           string
	CompanyID    string
	Email        string
	FirstName    string
	LastName     string
	Title        string
	Department   string
	LinkedInURL  string
	Source       string
	Confidence   int
	Verification string
	VerifiedAt   *time.Time
	OptedOut     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
