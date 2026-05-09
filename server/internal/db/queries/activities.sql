-- name: ListActivities :many
SELECT * FROM activity_items
WHERE session_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: CreateActivity :one
INSERT INTO activity_items (session_id, type, description, agent_id, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
