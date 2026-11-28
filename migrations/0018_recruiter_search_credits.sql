-- +goose Up
-- Credits a search took from the student's daily recruiter allowance: 1 for a radar search, 1 per recruiter given
-- back for a search at one company. Older rows count 1, as before.
ALTER TABLE recruiter_searches ADD COLUMN credits int NOT NULL DEFAULT 1 CHECK (credits >= 0);

-- +goose Down
ALTER TABLE recruiter_searches DROP COLUMN credits;
