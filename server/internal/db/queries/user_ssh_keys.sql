-- name: ListUserSSHKeys :many
SELECT * FROM user_ssh_keys
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetUserSSHKey :one
SELECT * FROM user_ssh_keys
WHERE id = $1 AND user_id = $2;

-- name: CreateUserSSHKey :one
INSERT INTO user_ssh_keys (user_id, label, public_key, fingerprint)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteUserSSHKey :exec
DELETE FROM user_ssh_keys
WHERE id = $1 AND user_id = $2;

-- name: LookupSessionMemberKeyByFingerprint :one
SELECT k.*
FROM user_ssh_keys k
JOIN session_members sm ON sm.user_id = k.user_id
WHERE sm.session_id = $1
  AND k.fingerprint = $2;

-- name: TouchUserSSHKeyLastUsed :exec
UPDATE user_ssh_keys
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
