-- +goose Up
-- An application sent without a subscription carries the file the student imported, not the CV built in 9itchi.
-- cv_original marks those emails: they are the ones counted against the free applications of the account.
ALTER TABLE emails ADD COLUMN cv_original boolean NOT NULL DEFAULT false;
CREATE INDEX emails_user_original_idx ON emails (user_id) WHERE cv_original;

-- +goose Down
DROP INDEX emails_user_original_idx;
ALTER TABLE emails DROP COLUMN cv_original;
