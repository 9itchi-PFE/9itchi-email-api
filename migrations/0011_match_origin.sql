-- +goose Up
-- Why a reading was made: 'analysis' is the full analysis a student asked for on one offer (the monthly limit counts
-- these); 'score' is a quick score asked for a list of offers (the daily limit of scores counts these). A score read
-- again as an analysis becomes an analysis. Rows made before are analyses.
ALTER TABLE job_matches
  ADD COLUMN origin text NOT NULL DEFAULT 'analysis' CHECK (origin IN ('analysis', 'score'));
CREATE INDEX job_matches_user_origin_created_idx ON job_matches (user_id, origin, created_at);

-- +goose Down
DROP INDEX job_matches_user_origin_created_idx;
ALTER TABLE job_matches DROP COLUMN origin;
