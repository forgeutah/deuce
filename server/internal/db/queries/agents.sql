-- name: ListAgents :many
SELECT * FROM agents WHERE deleted_at IS NULL ORDER BY name;

-- name: GetAgent :one
SELECT * FROM agents WHERE id = $1;

-- name: CreateAgent :one
INSERT INTO agents (name, role, color, color_muted, provider, model, description, system_prompt)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateAgent :one
UPDATE agents
SET name = $2,
    role = $3,
    provider = $4,
    model = $5,
    description = $6,
    system_prompt = $7,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAgent :exec
UPDATE agents SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;
