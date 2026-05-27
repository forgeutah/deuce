-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY name;

-- name: LookupUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CreateUserByEmail :one
INSERT INTO users (email, name, avatar, status)
VALUES ($1, $2, $3, 'online')
ON CONFLICT (email) DO NOTHING
RETURNING *;

-- name: UpdateUserName :one
UPDATE users SET name = $2 WHERE id = $1 RETURNING *;
