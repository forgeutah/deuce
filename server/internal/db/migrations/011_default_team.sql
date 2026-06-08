-- +goose Up

-- Team membership is the read boundary for sessions (see ListSessionsForUser).
-- A newly provisioned user with no team would therefore see nothing, so every
-- user is auto-joined to a single "default" team on first login. This flag
-- marks that team. Exactly one team may carry it (partial unique index below).
ALTER TABLE teams
    ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false;

-- At most one default team. Partial unique index: multiple `false` rows are
-- fine, but two `true` rows are rejected.
CREATE UNIQUE INDEX teams_single_default_idx
    ON teams (is_default)
    WHERE is_default;

-- Guarantee a team exists to host the flag even on a fresh install with no
-- seed data, so provisioning always has somewhere to put new users.
INSERT INTO teams (name, slug)
SELECT 'Default', 'default'
WHERE NOT EXISTS (SELECT 1 FROM teams);

-- Backfill: mark the earliest-created team as default if none is set yet.
-- Operators can re-point the flag with a single UPDATE later.
UPDATE teams SET is_default = true
WHERE id = (SELECT id FROM teams ORDER BY created_at ASC, id ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM teams WHERE is_default);

-- +goose Down

DROP INDEX IF EXISTS teams_single_default_idx;
ALTER TABLE teams DROP COLUMN IF EXISTS is_default;
