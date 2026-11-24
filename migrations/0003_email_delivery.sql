-- +goose Up
-- Recipient of an application email and the moment it left Gmail.
ALTER TABLE emails
  ADD COLUMN recipient text NOT NULL DEFAULT '',
  ADD COLUMN sent_at timestamptz,
  ADD COLUMN schedule_task_id text;

-- +goose Down
ALTER TABLE emails
  DROP COLUMN schedule_task_id,
  DROP COLUMN sent_at,
  DROP COLUMN recipient;
