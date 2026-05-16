-- +goose Up
--
-- Seed patches for the auth-module session demonstrating:
--   - basic patch landing (v1)
--   - supersession chain (v2 supersedes v1)
--   - independent patch (v3) for an unrelated change
--
-- Uses SELECT-INSERT so missing producing_message_id or session_id (e.g. on a
-- DB where the 002 seed sessions were never loaded) silently skips the insert
-- instead of failing the migration.
--
-- Patch IDs use the 50... prefix per the deterministic-UUID convention.
-- Hunks are intentionally compact (no embedded tabs) so the JSONB parser
-- accepts them as inline literals.

INSERT INTO patches (id, session_id, producing_message_id, parent_patch_id, origin_type, workspace_sha, hunks, file_count, hunk_count, created_at)
SELECT
    '50000000-0000-0000-0000-0000000000a1',
    s.id,
    '60000000-0000-0000-0000-000000000003',
    NULL,
    'agent',
    'abc1230000000000000000000000000000000000',
    '[{"path":"internal/auth/middleware.go","hunks":[{"oldStart":1,"oldLines":0,"newStart":1,"newLines":6,"lines":["+package auth","+","+import \"net/http\"","+","+func Middleware(next http.Handler) http.Handler { return next }","+"]}]}]'::jsonb,
    1, 1,
    now() - interval '3 hours'
FROM sessions s
WHERE s.id = '40000000-0000-0000-0000-000000000001'
  AND EXISTS (SELECT 1 FROM messages m WHERE m.id = '60000000-0000-0000-0000-000000000003');

INSERT INTO patches (id, session_id, producing_message_id, parent_patch_id, origin_type, workspace_sha, hunks, file_count, hunk_count, created_at)
SELECT
    '50000000-0000-0000-0000-0000000000a2',
    s.id,
    '60000000-0000-0000-0000-000000000005',
    '50000000-0000-0000-0000-0000000000a1',
    'agent',
    'def4560000000000000000000000000000000000',
    '[{"path":"internal/auth/validate.go","hunks":[{"oldStart":1,"oldLines":0,"newStart":1,"newLines":8,"lines":["+package auth","+","+import \"errors\"","+","+func Validate(token string) error {","+    if expired(token) { return errors.New(\"token expired\") }","+    return nil","+}"]}]}]'::jsonb,
    1, 1,
    now() - interval '1 hour 30 minutes'
FROM sessions s
WHERE s.id = '40000000-0000-0000-0000-000000000001'
  AND EXISTS (SELECT 1 FROM messages m WHERE m.id = '60000000-0000-0000-0000-000000000005')
  AND EXISTS (SELECT 1 FROM patches p WHERE p.id = '50000000-0000-0000-0000-0000000000a1');

INSERT INTO patches (id, session_id, producing_message_id, parent_patch_id, origin_type, workspace_sha, hunks, file_count, hunk_count, created_at)
SELECT
    '50000000-0000-0000-0000-0000000000a3',
    s.id,
    '60000000-0000-0000-0000-000000000007',
    NULL,
    'agent',
    'def4560000000000000000000000000000000000',
    '[{"path":"internal/auth/validate_test.go","hunks":[{"oldStart":1,"oldLines":0,"newStart":1,"newLines":7,"lines":["+package auth","+","+import \"testing\"","+","+func TestValidate_Expired(t *testing.T) {","+    if err := Validate(expiredToken); err == nil { t.Fatal(\"want error\") }","+}"]}]}]'::jsonb,
    1, 1,
    now() - interval '45 minutes'
FROM sessions s
WHERE s.id = '40000000-0000-0000-0000-000000000001'
  AND EXISTS (SELECT 1 FROM messages m WHERE m.id = '60000000-0000-0000-0000-000000000007');

-- +goose Down
DELETE FROM patches WHERE id IN (
    '50000000-0000-0000-0000-0000000000a1',
    '50000000-0000-0000-0000-0000000000a2',
    '50000000-0000-0000-0000-0000000000a3'
);
