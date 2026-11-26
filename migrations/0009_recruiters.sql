-- +goose Up
-- Recruiter Radar: each search a student runs (the query as asked, a digest of it, which provider answered) and the
-- recruiters it found. Searches made today (Tunis time) are what the plan's daily limit counts; the same query
-- again the same day gives back the stored result without asking the provider nor counting it.
CREATE TABLE recruiter_searches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  query jsonb NOT NULL,
  query_hash text NOT NULL CHECK (length(query_hash) = 64),
  provider text NOT NULL,
  result_count int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recruiter_searches_user_created_idx ON recruiter_searches (user_id, created_at);
CREATE INDEX recruiter_searches_hash_idx ON recruiter_searches (user_id, query_hash, created_at DESC);

CREATE TABLE recruiters (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  search_id uuid NOT NULL REFERENCES recruiter_searches(id) ON DELETE CASCADE,
  dedupe_key text NOT NULL,
  name text NOT NULL,
  title text NOT NULL DEFAULT '',
  company text NOT NULL DEFAULT '',
  domain text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  linkedin_url text NOT NULL DEFAULT '',
  confidence smallint NOT NULL DEFAULT 0 CHECK (confidence BETWEEN 0 AND 100),
  provider text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recruiters_user_created_idx ON recruiters (user_id, created_at DESC);
CREATE INDEX recruiters_search_idx ON recruiters (search_id);

-- +goose Down
DROP TABLE recruiters;
DROP TABLE recruiter_searches;
