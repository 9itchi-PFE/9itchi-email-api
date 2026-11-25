-- +goose Up
-- The AI's reading of how well one CV fits one offer, kept so the same question is not paid for twice. input_hash
-- is a digest of what the AI read (the offer and the CV as text): when either changes, the next request asks again.
-- Rows made this month are what the plan's monthly limit of analyses counts.
CREATE TABLE job_matches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  job_id uuid NOT NULL,
  resume_id uuid NOT NULL,
  language text NOT NULL CHECK (language IN ('fr', 'en', 'ar')),
  input_hash text NOT NULL CHECK (length(input_hash) = 64),
  score int NOT NULL CHECK (score BETWEEN 0 AND 100),
  level text NOT NULL CHECK (level IN ('strong', 'good', 'partial', 'weak')),
  strengths jsonb NOT NULL DEFAULT '[]',
  gaps jsonb NOT NULL DEFAULT '[]',
  advice text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, job_id, resume_id, language, input_hash)
);
CREATE INDEX job_matches_user_created_idx ON job_matches (user_id, created_at);

-- +goose Down
DROP TABLE job_matches;
