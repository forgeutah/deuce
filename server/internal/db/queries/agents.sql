-- name: ListAgents :many
SELECT * FROM agents ORDER BY name;

-- name: GetAgent :one
SELECT * FROM agents WHERE id = $1;
