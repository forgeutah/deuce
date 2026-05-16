-- +goose Up
ALTER TABLE patches ADD COLUMN failed_mid_turn BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE patches DROP COLUMN failed_mid_turn;
