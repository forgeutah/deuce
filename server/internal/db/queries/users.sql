-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY name;

-- name: LookupUserByForgeID :one
SELECT * FROM users WHERE forge_user_id = $1;

-- name: CreateUserByForgeID :one
INSERT INTO users (forge_user_id, name, email, avatar, status, forge_first_seen_at)
VALUES ($1, $2, $3, $4, 'online', now())
ON CONFLICT (forge_user_id) DO NOTHING
RETURNING *;
