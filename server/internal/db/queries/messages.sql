-- name: ListMessages :many
SELECT * FROM messages
WHERE session_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListMessagesBefore :many
SELECT messages.* FROM messages
WHERE messages.session_id = $1
  AND messages.created_at < (SELECT m2.created_at FROM messages m2 WHERE m2.id = $2)
ORDER BY messages.created_at DESC
LIMIT $3;

-- name: CreateMessage :one
INSERT INTO messages (session_id, author_id, author_type, content, expandable_content, mentions, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;
