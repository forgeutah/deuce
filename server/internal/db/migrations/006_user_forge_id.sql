-- +goose Up
ALTER TABLE users
    ADD COLUMN forge_user_id BIGINT UNIQUE,
    ADD COLUMN forge_first_seen_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users
    DROP COLUMN forge_first_seen_at,
    DROP COLUMN forge_user_id;
