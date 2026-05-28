-- +goose Up
CREATE TABLE user_ssh_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 255),
    public_key   TEXT NOT NULL CHECK (length(public_key) BETWEEN 1 AND 8192),
    fingerprint  TEXT NOT NULL CHECK (fingerprint ~ '^SHA256:[A-Za-z0-9+/]{43}=?$'),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_user_ssh_keys_user_fp
    ON user_ssh_keys(user_id, fingerprint);

-- +goose Down
DROP TABLE IF EXISTS user_ssh_keys;
