package repositories

import (
	"context"
	"time"

	"9itchi-email-api/models/db"
)

// PoolStatsRepository reads the admin numbers of the prospect pool: the pool itself, what the fresh searches took
// from it and the credits the providers spent. Read only.
type PoolStatsRepository interface {
	// PoolCounts counts the companies and the people of the pool; Fresh counts the people checked or updated since
	// a time.
	PoolCounts(ctx context.Context, freshSince time.Time) (*db.PoolCounts, error)
	// SearchTotals sums the searches with stored stats made since a time.
	SearchTotals(ctx context.Context, since time.Time) (*db.SearchTotals, error)
	// SearchDays sums the searches with stored stats made since a time per UTC day, oldest first (days without
	// searches are absent).
	SearchDays(ctx context.Context, since time.Time) ([]db.SearchDay, error)
	// CreditDays sums the provider_credits log since a time per UTC day and provider, oldest first.
	CreditDays(ctx context.Context, since time.Time) ([]db.ProviderCreditsDay, error)
}

type poolStatsRepository struct {
	store *Store
}

func NewPoolStatsRepository(store *Store) PoolStatsRepository {
	return &poolStatsRepository{store: store}
}

// searchSums are the SearchTotals columns over recruiter_searches rows with stored stats.
const searchSums = `count(*), COALESCE(sum(contacts_from_pool), 0), COALESCE(sum(contacts_found), 0),
	count(*) FILTER (WHERE contacts_found = 0 AND contacts_from_pool > 0)`

func (r *poolStatsRepository) PoolCounts(ctx context.Context, freshSince time.Time) (*db.PoolCounts, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	c := db.PoolCounts{BySource: map[string]int{}}
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM companies),
			count(*),
			count(*) FILTER (WHERE verification = 'valid'),
			count(*) FILTER (WHERE department = 'hr'),
			count(*) FILTER (WHERE verified_at >= $1 OR updated_at >= $1)
		FROM contacts WHERE NOT opted_out`, freshSince).Scan(&c.Companies, &c.Contacts, &c.Verified, &c.HR, &c.Fresh)
	if err != nil {
		return nil, MapError(err)
	}
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT source, count(*) FROM contacts WHERE NOT opted_out GROUP BY source`)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var n int
		if err := rows.Scan(&source, &n); err != nil {
			return nil, MapError(err)
		}
		c.BySource[source] = n
	}
	if err := rows.Err(); err != nil {
		return nil, MapError(err)
	}
	return &c, nil
}

func (r *poolStatsRepository) SearchTotals(ctx context.Context, since time.Time) (*db.SearchTotals, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var t db.SearchTotals
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT `+searchSums+` FROM recruiter_searches
		WHERE created_at >= $1 AND contacts_from_pool IS NOT NULL AND contacts_found IS NOT NULL`, since).
		Scan(&t.Searches, &t.ContactsFromPool, &t.ContactsFound, &t.PoolOnly)
	if err != nil {
		return nil, MapError(err)
	}
	return &t, nil
}

func (r *poolStatsRepository) SearchDays(ctx context.Context, since time.Time) ([]db.SearchDay, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, `+searchSums+`
		FROM recruiter_searches
		WHERE created_at >= $1 AND contacts_from_pool IS NOT NULL AND contacts_found IS NOT NULL
		GROUP BY day ORDER BY day`, since)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.SearchDay{}
	for rows.Next() {
		var d db.SearchDay
		if err := rows.Scan(&d.Day, &d.Searches, &d.ContactsFromPool, &d.ContactsFound, &d.PoolOnly); err != nil {
			return nil, MapError(err)
		}
		out = append(out, d)
	}
	return out, MapError(rows.Err())
}

func (r *poolStatsRepository) CreditDays(ctx context.Context, since time.Time) ([]db.ProviderCreditsDay, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, provider, sum(credits)
		FROM provider_credits WHERE created_at >= $1
		GROUP BY day, provider ORDER BY day, provider`, since)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.ProviderCreditsDay{}
	for rows.Next() {
		var d db.ProviderCreditsDay
		if err := rows.Scan(&d.Day, &d.Provider, &d.Credits); err != nil {
			return nil, MapError(err)
		}
		out = append(out, d)
	}
	return out, MapError(rows.Err())
}
