package repositories

import (
	"context"

	"9itchi-email-api/utils"
)

// Repositories holds every repository plus the shared Store (pool + transactions).
type Repositories struct {
	Store       *Store
	Gmail       GmailRepository
	Email       EmailRepository
	Application ApplicationRepository
	Erase       EraseRepository
	Match       MatchRepository
	Draft       DraftRepository
	Recruiter   RecruiterRepository
	Prospect    ProspectRepository
}

// InitRepositories connects to PostgreSQL, applies migrations and builds the repositories.
func InitRepositories(ctx context.Context, config utils.AppConfig) (*Repositories, error) {
	if err := RunMigrations(ctx, config.DatabaseURL); err != nil {
		return nil, err
	}
	store, err := NewStore(ctx, config.DatabaseURL)
	if err != nil {
		return nil, err
	}
	return &Repositories{
		Store:       store,
		Gmail:       NewGmailRepository(store),
		Email:       NewEmailRepository(store),
		Application: NewApplicationRepository(store),
		Erase:       NewEraseRepository(store),
		Match:       NewMatchRepository(store),
		Draft:       NewDraftRepository(store),
		Recruiter:   NewRecruiterRepository(store),
		Prospect:    NewProspectRepository(store),
	}, nil
}

func (r *Repositories) Close() { r.Store.Close() }
