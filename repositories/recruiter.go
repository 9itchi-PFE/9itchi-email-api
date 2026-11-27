package repositories

import (
	"context"
	"time"

	"9itchi-email-api/models/db"
)

// RecruiterRepository keeps the recruiter searches of the students and the people they found.
type RecruiterRepository interface {
	// CountSearchesSince counts the searches the user made since a time: the plan's daily limit counts these.
	CountSearchesSince(ctx context.Context, userID string, since time.Time) (int, error)
	// LatestSearch returns the newest search of the user with this query digest made since a time (ErrNotFound
	// when none).
	LatestSearch(ctx context.Context, userID, queryHash string, since time.Time) (*db.RecruiterSearch, error)
	// SaveSearch stores a search and the recruiters it found, in one transaction, and returns them as stored.
	SaveSearch(ctx context.Context, s *db.RecruiterSearch, found []db.Recruiter) (*db.RecruiterSearch, []db.Recruiter, error)
	// ListBySearch returns the recruiters one search of the user found, most certain first.
	ListBySearch(ctx context.Context, userID, searchID string) ([]db.Recruiter, error)
	// Get returns one recruiter found for the user (ErrNotFound when none, or someone else's).
	Get(ctx context.Context, userID, id string) (*db.Recruiter, error)
	// List returns every recruiter found for the user, each person (dedupe key) once as last found, newest first.
	List(ctx context.Context, userID string, limit int) ([]db.Recruiter, error)
	// GetSearch returns one search of the user (ErrNotFound when none, or someone else's).
	GetSearch(ctx context.Context, userID, id string) (*db.RecruiterSearch, error)
	// RecentDomains returns the company domains of the recruiters found for the user since a time.
	RecentDomains(ctx context.Context, userID string, since time.Time) ([]string, error)
}

type recruiterRepository struct {
	store *Store
}

func NewRecruiterRepository(store *Store) RecruiterRepository {
	return &recruiterRepository{store: store}
}

const (
	searchColumns    = `id, user_id, query, query_hash, provider, result_count, created_at`
	recruiterColumns = `id, user_id, search_id, dedupe_key, name, title, company, domain, email, linkedin_url, confidence, provider, created_at,
		COALESCE(contact_id::text, '') AS contact_id, company_summary, matched_keywords, verified`
)

func scanSearch(r rowScanner) (*db.RecruiterSearch, error) {
	var s db.RecruiterSearch
	if err := r.Scan(&s.ID, &s.UserID, &s.Query, &s.QueryHash, &s.Provider, &s.ResultCount, &s.CreatedAt); err != nil {
		return nil, MapError(err)
	}
	return &s, nil
}

func scanRecruiter(r rowScanner) (*db.Recruiter, error) {
	var x db.Recruiter
	err := r.Scan(&x.ID, &x.UserID, &x.SearchID, &x.DedupeKey, &x.Name, &x.Title, &x.Company, &x.Domain, &x.Email,
		&x.LinkedInURL, &x.Confidence, &x.Provider, &x.CreatedAt, &x.ContactID, &x.CompanySummary, &x.MatchedKeywords, &x.Verified)
	if err != nil {
		return nil, MapError(err)
	}
	return &x, nil
}

func (r *recruiterRepository) CountSearchesSince(ctx context.Context, userID string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT count(*) FROM recruiter_searches WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n)
	return n, MapError(err)
}

func (r *recruiterRepository) LatestSearch(ctx context.Context, userID, queryHash string, since time.Time) (*db.RecruiterSearch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanSearch(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+searchColumns+` FROM recruiter_searches
		WHERE user_id = $1 AND query_hash = $2 AND created_at >= $3
		ORDER BY created_at DESC LIMIT 1`, userID, queryHash, since))
}

func (r *recruiterRepository) SaveSearch(ctx context.Context, s *db.RecruiterSearch, found []db.Recruiter) (*db.RecruiterSearch, []db.Recruiter, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var saved *db.RecruiterSearch
	var out []db.Recruiter
	err := r.store.WithTx(ctx, func(ctx context.Context) error {
		var err error
		saved, err = scanSearch(r.store.Conn(ctx).QueryRow(ctx, `
			INSERT INTO recruiter_searches (user_id, query, query_hash, provider, result_count,
				companies_from_pool, companies_discovered, contacts_from_pool, contacts_found)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING `+searchColumns, append([]any{s.UserID, s.Query, s.QueryHash, s.Provider, len(found)}, searchStatsArgs(s.Stats)...)...))
		if err != nil {
			return err
		}
		saved.Stats = s.Stats
		out = make([]db.Recruiter, 0, len(found))
		for _, f := range found {
			x, err := scanRecruiter(r.store.Conn(ctx).QueryRow(ctx, `
				INSERT INTO recruiters (user_id, search_id, dedupe_key, name, title, company, domain, email, linkedin_url, confidence, provider,
					contact_id, company_summary, matched_keywords, verified)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, '')::uuid, $13, $14, $15)
				RETURNING `+recruiterColumns,
				saved.UserID, saved.ID, f.DedupeKey, f.Name, f.Title, f.Company, f.Domain, f.Email, f.LinkedInURL, f.Confidence, saved.Provider,
				f.ContactID, f.CompanySummary, nonNil(f.MatchedKeywords), f.Verified))
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return saved, out, nil
}

// searchStatsArgs are the four stats columns of a search in insert order (all NULL when not stored).
func searchStatsArgs(s *db.SearchStats) []any {
	if s == nil {
		return []any{nil, nil, nil, nil}
	}
	return []any{s.CompaniesFromPool, s.CompaniesDiscovered, s.ContactsFromPool, s.ContactsFound}
}

func (r *recruiterRepository) ListBySearch(ctx context.Context, userID, searchID string) ([]db.Recruiter, error) {
	return r.list(ctx, `SELECT `+recruiterColumns+` FROM recruiters WHERE user_id = $1 AND search_id = $2
		ORDER BY confidence DESC, name, id`, userID, searchID)
}

func (r *recruiterRepository) List(ctx context.Context, userID string, limit int) ([]db.Recruiter, error) {
	return r.list(ctx, `SELECT `+recruiterColumns+` FROM (
			SELECT DISTINCT ON (dedupe_key) `+recruiterColumns+` FROM recruiters WHERE user_id = $1
			ORDER BY dedupe_key, created_at DESC, id
		) r ORDER BY created_at DESC, confidence DESC, name LIMIT $2`, userID, limit)
}

func (r *recruiterRepository) list(ctx context.Context, query string, args ...any) ([]db.Recruiter, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Recruiter{}
	for rows.Next() {
		x, err := scanRecruiter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, MapError(rows.Err())
}

func (r *recruiterRepository) Get(ctx context.Context, userID, id string) (*db.Recruiter, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanRecruiter(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+recruiterColumns+` FROM recruiters WHERE id = $1 AND user_id = $2`, id, userID))
}

func (r *recruiterRepository) GetSearch(ctx context.Context, userID, id string) (*db.RecruiterSearch, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanSearch(r.store.Conn(ctx).QueryRow(ctx,
		`SELECT `+searchColumns+` FROM recruiter_searches WHERE id = $1 AND user_id = $2`, id, userID))
}

func (r *recruiterRepository) RecentDomains(ctx context.Context, userID string, since time.Time) ([]string, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT DISTINCT domain FROM recruiters
		WHERE user_id = $1 AND created_at >= $2 AND domain <> ''`, userID, since)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, MapError(err)
		}
		out = append(out, d)
	}
	return out, MapError(rows.Err())
}

// nonNil keeps a text[] column from receiving NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
