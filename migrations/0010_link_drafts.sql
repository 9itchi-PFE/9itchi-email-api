-- +goose Up
-- A letter can also be written for an offer the student found elsewhere, given by its link: such a row has the
-- normalized link instead of a 9itchi offer.
ALTER TABLE ai_drafts ALTER COLUMN job_id DROP NOT NULL,
  ADD COLUMN source_url text,
  ADD CONSTRAINT ai_drafts_target_chk CHECK (job_id IS NOT NULL OR source_url IS NOT NULL);
CREATE INDEX ai_drafts_url_idx ON ai_drafts (user_id, source_url, input_hash, language, tone, created_at DESC) WHERE source_url IS NOT NULL;

-- +goose Down
DELETE FROM ai_drafts WHERE job_id IS NULL;
DROP INDEX ai_drafts_url_idx;
ALTER TABLE ai_drafts DROP CONSTRAINT ai_drafts_target_chk, DROP COLUMN source_url, ALTER COLUMN job_id SET NOT NULL;
