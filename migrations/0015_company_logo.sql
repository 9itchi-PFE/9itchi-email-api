-- +goose Up
-- Company logo from the providers ("" = unknown). An upsert without a logo keeps the stored one.
ALTER TABLE companies ADD COLUMN logo_url text NOT NULL DEFAULT '' CHECK (length(logo_url) <= 500);

-- +goose Down
ALTER TABLE companies DROP COLUMN logo_url;
