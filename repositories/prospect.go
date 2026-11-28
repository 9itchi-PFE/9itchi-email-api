package repositories

import (
	"context"
	"errors"
	"strings"
	"time"

	"9itchi-email-api/models/db"
)

// ProspectRepository keeps the shared prospect pool: companies, the people who hire there and which provider was
// asked for which domain. Platform data: nothing here belongs to a user.
type ProspectRepository interface {
	// SearchCompanies returns the companies whose search text matches the full-text query (tsquery syntax, 'simple'
	// configuration), of a country when given, best match first.
	SearchCompanies(ctx context.Context, tsQuery, country string, limit int) ([]db.Company, error)
	// CompanyByDomain returns the company of a domain (ErrNotFound when none).
	CompanyByDomain(ctx context.Context, domain string) (*db.Company, error)
	// CompaniesNamed returns the companies whose name starts with the prefix, any case, most recently updated first.
	CompaniesNamed(ctx context.Context, prefix string, limit int) ([]db.Company, error)
	// SaveCompany inserts the company, or replaces the one with the same domain, and returns it as stored.
	SaveCompany(ctx context.Context, c *db.Company) (*db.Company, error)
	// CompanyContacts returns the people of a company still in the pool (not opted out) checked or updated since
	// a time, most certain first.
	CompanyContacts(ctx context.Context, companyID string, since time.Time, limit int) ([]db.Contact, error)
	// SaveContact inserts the person, or updates the one with the same address (any case: what is known stays when
	// the new value is empty; opted_out never changes), and returns them as stored.
	SaveContact(ctx context.Context, c *db.Contact) (*db.Contact, error)
	// LookupSince tells whether the provider was asked for the domain since a time.
	LookupSince(ctx context.Context, domain, provider string, since time.Time) (bool, error)
	// SaveLookup records that the provider was asked for the domain now, what it found and the credits it took.
	SaveLookup(ctx context.Context, domain, provider string, resultCount, credits int) error
	// CreditsSince sums the credits the provider used since a time.
	CreditsSince(ctx context.Context, provider string, since time.Time) (int, error)
	// SaveCredits appends the credits a provider just spent to the provider_credits log (credits > 0).
	SaveCredits(ctx context.Context, provider string, credits int) error
}

type prospectRepository struct {
	store *Store
}

func NewProspectRepository(store *Store) ProspectRepository {
	return &prospectRepository{store: store}
}

const (
	companyColumns = `id, domain, name, description, industry, size_range, country, city, keywords, search_text, source, logo_url, created_at, updated_at`
	contactColumns = `id, company_id, email, first_name, last_name, title, department, linkedin_url, source, confidence, verification,
		verified_at, opted_out, created_at, updated_at`
)

func scanCompany(r rowScanner) (*db.Company, error) {
	var c db.Company
	err := r.Scan(&c.ID, &c.Domain, &c.Name, &c.Description, &c.Industry, &c.SizeRange, &c.Country, &c.City, &c.Keywords,
		&c.SearchText, &c.Source, &c.LogoURL, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, MapError(err)
	}
	return &c, nil
}

func scanContact(r rowScanner) (*db.Contact, error) {
	var c db.Contact
	err := r.Scan(&c.ID, &c.CompanyID, &c.Email, &c.FirstName, &c.LastName, &c.Title, &c.Department, &c.LinkedInURL,
		&c.Source, &c.Confidence, &c.Verification, &c.VerifiedAt, &c.OptedOut, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, MapError(err)
	}
	return &c, nil
}

func (r *prospectRepository) SearchCompanies(ctx context.Context, tsQuery, country string, limit int) ([]db.Company, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT `+companyColumns+` FROM companies
		WHERE to_tsvector('simple', search_text) @@ to_tsquery('simple', $1) AND ($2 = '' OR country = $2)
		ORDER BY ts_rank(to_tsvector('simple', search_text), to_tsquery('simple', $1)) DESC, updated_at DESC
		LIMIT $3`, tsQuery, country, limit)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Company{}
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, MapError(rows.Err())
}

func (r *prospectRepository) CompanyByDomain(ctx context.Context, domain string) (*db.Company, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanCompany(r.store.Conn(ctx).QueryRow(ctx, `SELECT `+companyColumns+` FROM companies WHERE domain = $1`, domain))
}

// likeEscaper escapes the LIKE wildcards of a typed prefix.
var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func (r *prospectRepository) CompaniesNamed(ctx context.Context, prefix string, limit int) ([]db.Company, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT `+companyColumns+` FROM companies WHERE lower(name) LIKE $1
		ORDER BY updated_at DESC LIMIT $2`, likeEscaper.Replace(strings.ToLower(prefix))+"%", limit)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Company{}
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, MapError(rows.Err())
}

func (r *prospectRepository) SaveCompany(ctx context.Context, c *db.Company) (*db.Company, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanCompany(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO companies (domain, name, description, industry, size_range, country, city, keywords, search_text, source, logo_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (domain) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
			industry = EXCLUDED.industry, size_range = EXCLUDED.size_range, country = EXCLUDED.country, city = EXCLUDED.city,
			keywords = EXCLUDED.keywords, search_text = EXCLUDED.search_text,
			logo_url = COALESCE(NULLIF(EXCLUDED.logo_url, ''), companies.logo_url), updated_at = now()
		RETURNING `+companyColumns,
		c.Domain, c.Name, c.Description, c.Industry, c.SizeRange, c.Country, c.City, nonNil(c.Keywords), c.SearchText, c.Source,
		c.LogoURL))
}

func (r *prospectRepository) CompanyContacts(ctx context.Context, companyID string, since time.Time, limit int) ([]db.Contact, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	rows, err := r.store.Conn(ctx).Query(ctx, `SELECT `+contactColumns+` FROM contacts
		WHERE company_id = $1 AND NOT opted_out AND (verified_at >= $2 OR updated_at >= $2)
		ORDER BY confidence DESC, updated_at DESC, id LIMIT $3`, companyID, since, limit)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []db.Contact{}
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, MapError(rows.Err())
}

func (r *prospectRepository) SaveContact(ctx context.Context, c *db.Contact) (*db.Contact, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	return scanContact(r.store.Conn(ctx).QueryRow(ctx, `
		INSERT INTO contacts (company_id, email, first_name, last_name, title, department, linkedin_url, source, confidence,
			verification, verified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT ((lower(email))) DO UPDATE SET
			first_name = COALESCE(NULLIF(EXCLUDED.first_name, ''), contacts.first_name),
			last_name = COALESCE(NULLIF(EXCLUDED.last_name, ''), contacts.last_name),
			title = COALESCE(NULLIF(EXCLUDED.title, ''), contacts.title),
			department = CASE WHEN EXCLUDED.department = 'other' THEN contacts.department ELSE EXCLUDED.department END,
			linkedin_url = COALESCE(NULLIF(EXCLUDED.linkedin_url, ''), contacts.linkedin_url),
			source = EXCLUDED.source,
			confidence = EXCLUDED.confidence,
			verification = COALESCE(NULLIF(EXCLUDED.verification, ''), contacts.verification),
			verified_at = COALESCE(EXCLUDED.verified_at, contacts.verified_at),
			updated_at = now()
		RETURNING `+contactColumns,
		c.CompanyID, c.Email, c.FirstName, c.LastName, c.Title, c.Department, c.LinkedInURL, c.Source, c.Confidence,
		c.Verification, c.VerifiedAt))
}

func (r *prospectRepository) LookupSince(ctx context.Context, domain, provider string, since time.Time) (bool, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var at time.Time
	err := r.store.Conn(ctx).QueryRow(ctx,
		`SELECT fetched_at FROM contact_lookups WHERE domain = $1 AND provider = $2`, domain, provider).Scan(&at)
	if err = MapError(err); errors.Is(err, ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return !at.Before(since), nil
}

func (r *prospectRepository) SaveLookup(ctx context.Context, domain, provider string, resultCount, credits int) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `
		INSERT INTO contact_lookups (domain, provider, result_count, credits_used) VALUES ($1, $2, $3, $4)
		ON CONFLICT (domain, provider) DO UPDATE SET result_count = EXCLUDED.result_count,
			credits_used = EXCLUDED.credits_used, fetched_at = now()`, domain, provider, resultCount, credits)
	return MapError(err)
}

func (r *prospectRepository) CreditsSince(ctx context.Context, provider string, since time.Time) (int, error) {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	var n int
	err := r.store.Conn(ctx).QueryRow(ctx, `SELECT COALESCE(sum(credits_used), 0) FROM contact_lookups
		WHERE provider = $1 AND fetched_at >= $2`, provider, since).Scan(&n)
	return n, MapError(err)
}

func (r *prospectRepository) SaveCredits(ctx context.Context, provider string, credits int) error {
	ctx, cancel := QueryTimeout(ctx)
	defer cancel()
	_, err := r.store.Conn(ctx).Exec(ctx, `INSERT INTO provider_credits (provider, credits) VALUES ($1, $2)`, provider, credits)
	return MapError(err)
}
