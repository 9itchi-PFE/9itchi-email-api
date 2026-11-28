-- +goose Up
-- tracker-api tracking ids are 43-char base64url strings, not uuids: as uuid, storing one failed and every email
-- left without its open pixel. New rows still start with a random uuid until the tracker's id replaces it.
ALTER TABLE emails ALTER COLUMN tracking_id TYPE text;

-- +goose Down
UPDATE emails SET tracking_id = gen_random_uuid()::text
WHERE tracking_id !~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';
ALTER TABLE emails ALTER COLUMN tracking_id TYPE uuid USING tracking_id::uuid;
