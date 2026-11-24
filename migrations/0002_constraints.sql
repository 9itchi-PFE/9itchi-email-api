-- +goose Up
-- Constraints and indexes for the email tables (audit point 1 of the study).

-- One Gmail connection per user.
ALTER TABLE gmail_connections
  ADD CONSTRAINT gmail_connections_user_id_key UNIQUE (user_id);

-- Emails: known statuses, one tracking id per email, list and dispatch indexes.
ALTER TABLE emails ALTER COLUMN status SET DEFAULT 'draft';
UPDATE emails SET status = 'draft' WHERE status = '';
ALTER TABLE emails
  ADD CONSTRAINT emails_status_check
    CHECK (status IN ('draft', 'scheduled', 'sending', 'sent', 'failed', 'cancelled')),
  ADD CONSTRAINT emails_scheduled_at_check
    CHECK (status <> 'scheduled' OR scheduled_at IS NOT NULL),
  ADD CONSTRAINT emails_tracking_id_key UNIQUE (tracking_id);
CREATE INDEX emails_user_created_idx ON emails (user_id, created_at DESC, id DESC);
CREATE INDEX emails_due_idx ON emails (scheduled_at, id) WHERE status = 'scheduled';

-- Applications: known statuses, one application per user and job, link to its email.
ALTER TABLE applications ALTER COLUMN status SET DEFAULT 'sent';
UPDATE applications SET status = 'sent' WHERE status = '';
ALTER TABLE applications
  ADD CONSTRAINT applications_status_check
    CHECK (status IN ('scheduled', 'sent', 'opened', 'replied', 'withdrawn', 'closed')),
  ADD CONSTRAINT applications_user_job_key UNIQUE (user_id, job_id),
  ADD CONSTRAINT applications_email_id_fkey
    FOREIGN KEY (email_id) REFERENCES emails (id) ON DELETE SET NULL;
CREATE INDEX applications_user_created_idx ON applications (user_id, created_at DESC, id DESC);
CREATE INDEX applications_email_id_idx ON applications (email_id) WHERE email_id IS NOT NULL;

-- +goose Down
DROP INDEX applications_email_id_idx;
DROP INDEX applications_user_created_idx;
ALTER TABLE applications
  DROP CONSTRAINT applications_email_id_fkey,
  DROP CONSTRAINT applications_user_job_key,
  DROP CONSTRAINT applications_status_check;
ALTER TABLE applications ALTER COLUMN status SET DEFAULT '';

DROP INDEX emails_due_idx;
DROP INDEX emails_user_created_idx;
ALTER TABLE emails
  DROP CONSTRAINT emails_tracking_id_key,
  DROP CONSTRAINT emails_scheduled_at_check,
  DROP CONSTRAINT emails_status_check;
ALTER TABLE emails ALTER COLUMN status SET DEFAULT '';

ALTER TABLE gmail_connections DROP CONSTRAINT gmail_connections_user_id_key;
