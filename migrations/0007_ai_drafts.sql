-- +goose Up
-- The letters the AI wrote, kept so that opening the same offer again gives back the same letter without asking
-- the AI nor counting it; « Réécrire » asks for a new one. input_hash is a digest of the offer and of the CV as the
-- AI read them. Rows made this month are what the plan's monthly limit of AI letters counts.
CREATE TABLE ai_drafts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  job_id uuid NOT NULL,
  language text NOT NULL CHECK (language IN ('fr', 'en', 'ar')),
  tone text NOT NULL CHECK (tone IN ('formal', 'friendly')),
  input_hash text NOT NULL CHECK (length(input_hash) = 64),
  subject text NOT NULL,
  body text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_drafts_user_created_idx ON ai_drafts (user_id, created_at);
CREATE INDEX ai_drafts_lookup_idx ON ai_drafts (user_id, job_id, input_hash, language, tone, created_at DESC);

-- +goose Down
DROP TABLE ai_drafts;
