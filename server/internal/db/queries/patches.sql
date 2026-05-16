-- name: ListPatchesBySession :many
SELECT * FROM patches
WHERE session_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetPatchBySessionAndID :one
SELECT * FROM patches
WHERE session_id = $1 AND id = $2;

-- name: CreatePatch :one
INSERT INTO patches (
    session_id,
    producing_message_id,
    parent_patch_id,
    origin_type,
    workspace_sha,
    committed_sha,
    hunks,
    file_count,
    hunk_count,
    failed_mid_turn
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;
