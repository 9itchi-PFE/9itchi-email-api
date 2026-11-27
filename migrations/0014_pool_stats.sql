-- +goose Up
-- Admin pool stats: what each fresh search took from the pool and from the providers (NULL = a search made before
-- this migration, or a cached one: left out of the rates), and a log of the credits the providers spent (platform
-- data: no user_id, not erased). contact_lookups keeps only the last lookup per (domain, provider): not a log.
ALTER TABLE recruiter_searches
  ADD COLUMN contacts_from_pool int,
  ADD COLUMN contacts_found int,
  ADD COLUMN companies_from_pool int,
  ADD COLUMN companies_discovered int;

CREATE TABLE provider_credits (
  id bigserial PRIMARY KEY,
  provider text NOT NULL,
  credits int NOT NULL CHECK (credits > 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX provider_credits_created_idx ON provider_credits (created_at);

-- +goose Down
DROP TABLE provider_credits;
ALTER TABLE recruiter_searches DROP COLUMN contacts_from_pool, DROP COLUMN contacts_found, DROP COLUMN companies_from_pool,
  DROP COLUMN companies_discovered;
