-- +goose Up
-- Recruiter Radar v2: the shared prospect pool. Companies and the people who hire there are platform data (no
-- user_id, not erased with a user): built from every student's searches, the platform's offers and paid lookups,
-- so a company or person paid for once serves the next student. contact_lookups remembers which provider was asked
-- for which domain (0 results included) so the same provider is not asked again for 90 days.
CREATE TABLE companies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  domain text NOT NULL UNIQUE CHECK (domain = lower(domain) AND domain NOT LIKE 'www.%'),
  name text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
  industry text NOT NULL DEFAULT '',
  size_range text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '' CHECK (country = '' OR country ~ '^[A-Z]{2}$'),
  city text NOT NULL DEFAULT '',
  keywords text[] NOT NULL DEFAULT '{}',
  search_text text NOT NULL DEFAULT '',
  source text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX companies_search_idx ON companies USING gin (to_tsvector('simple', search_text));
CREATE INDEX companies_country_idx ON companies (country);

CREATE TABLE contacts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  company_id uuid NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
  email text NOT NULL,
  first_name text NOT NULL DEFAULT '',
  last_name text NOT NULL DEFAULT '',
  title text NOT NULL DEFAULT '',
  department text NOT NULL DEFAULT 'other' CHECK (department IN ('hr', 'management', 'executive', 'engineering', 'other')),
  linkedin_url text NOT NULL DEFAULT '',
  source text NOT NULL,
  confidence smallint NOT NULL DEFAULT 0 CHECK (confidence BETWEEN 0 AND 100),
  verification text NOT NULL DEFAULT '' CHECK (verification IN ('valid', 'accept_all', 'unknown', 'invalid', '')),
  verified_at timestamptz,
  opted_out boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX contacts_email_idx ON contacts (lower(email));
CREATE INDEX contacts_company_idx ON contacts (company_id) WHERE NOT opted_out;

CREATE TABLE contact_lookups (
  domain text NOT NULL,
  provider text NOT NULL,
  result_count int NOT NULL DEFAULT 0,
  credits_used int NOT NULL DEFAULT 0,
  fetched_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (domain, provider)
);
CREATE INDEX contact_lookups_provider_fetched_idx ON contact_lookups (provider, fetched_at);

-- Per user (erased with the user like the rest of recruiters): the pool person a row came from, why the company
-- was picked and whether the provider said the address is valid.
ALTER TABLE recruiters
  ADD COLUMN contact_id uuid REFERENCES contacts (id) ON DELETE SET NULL,
  ADD COLUMN company_summary text NOT NULL DEFAULT '',
  ADD COLUMN matched_keywords text[] NOT NULL DEFAULT '{}',
  ADD COLUMN verified boolean NOT NULL DEFAULT false;
CREATE INDEX recruiters_user_domain_idx ON recruiters (user_id, domain, created_at DESC);

-- +goose Down
DROP INDEX recruiters_user_domain_idx;
ALTER TABLE recruiters DROP COLUMN contact_id, DROP COLUMN company_summary, DROP COLUMN matched_keywords, DROP COLUMN verified;
DROP TABLE contact_lookups;
DROP TABLE contacts;
DROP TABLE companies;
