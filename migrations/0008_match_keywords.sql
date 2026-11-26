-- +goose Up
-- Offer keywords the AI read in the offer, split by whether the CV mentions them, and the share found (0 to 100).
-- Rows made before this column exist read as no keywords.
ALTER TABLE job_matches
  ADD COLUMN keywords jsonb NOT NULL DEFAULT '{"matched":[],"missing":[]}',
  ADD COLUMN keyword_coverage int NOT NULL DEFAULT 0 CHECK (keyword_coverage BETWEEN 0 AND 100);

-- +goose Down
ALTER TABLE job_matches DROP COLUMN keywords, DROP COLUMN keyword_coverage;
