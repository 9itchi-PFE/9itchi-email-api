-- +goose Up
-- The follow-up (« relance ») of an application: one per application, sent from the student's Gmail to the
-- recipient of the first email. Applying again to the same offer starts over.
ALTER TABLE applications
  ADD COLUMN follow_up_email_id uuid REFERENCES emails (id) ON DELETE SET NULL,
  ADD COLUMN follow_up_sent_at timestamptz;

-- +goose Down
ALTER TABLE applications DROP COLUMN follow_up_email_id, DROP COLUMN follow_up_sent_at;
