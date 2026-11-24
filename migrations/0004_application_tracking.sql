-- +goose Up
-- An application keeps the title and company of its offer (offers are removed once expired) and how it was
-- sent: from the student's Gmail, or by the student on another site or from their own mailbox ("self").
ALTER TABLE applications
  ADD COLUMN job_title text NOT NULL DEFAULT '',
  ADD COLUMN company text NOT NULL DEFAULT '',
  ADD COLUMN via text NOT NULL DEFAULT 'gmail',
  ADD CONSTRAINT applications_via_check CHECK (via IN ('gmail', 'self'));

-- +goose Down
ALTER TABLE applications
  DROP CONSTRAINT applications_via_check,
  DROP COLUMN via,
  DROP COLUMN company,
  DROP COLUMN job_title;
