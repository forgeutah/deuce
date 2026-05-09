-- +goose Up
ALTER TABLE sessions ADD COLUMN repo_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN repo_url;
