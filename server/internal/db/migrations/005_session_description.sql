-- +goose Up
ALTER TABLE sessions ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN description;
