-- name: ListSessionsForUser :many
SELECT s.* FROM sessions s
JOIN session_members sm ON s.id = sm.session_id
WHERE sm.user_id = $1
ORDER BY s.last_activity_at DESC;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: CreateSession :one
INSERT INTO sessions (name, description, project_id, repo_url, status, workspace_status, plan_content)
VALUES ($1, $2, $3, $4, 'active', 'starting', '')
RETURNING *;

-- name: UpdateSessionStatus :one
UPDATE sessions SET status = $2 WHERE id = $1 RETURNING *;

-- name: UpdateSessionPlan :one
UPDATE sessions SET plan_content = $2 WHERE id = $1 RETURNING *;

-- name: UpdateSessionDescription :one
UPDATE sessions SET description = $2 WHERE id = $1 RETURNING *;

-- name: UpdateSessionWorkspaceStatus :one
UPDATE sessions SET workspace_status = $2 WHERE id = $1 RETURNING *;

-- name: UpdateSessionWorkspaceStatusIfMatches :execrows
UPDATE sessions
SET workspace_status = @new_status
WHERE id = @id AND workspace_status = @expected_status;

-- name: ListNonArchivedSessions :many
SELECT * FROM sessions
WHERE status != 'archived'
ORDER BY id;

-- name: ResetStaleWorkspaceTransitions :exec
UPDATE sessions
SET workspace_status = 'failed'
WHERE workspace_status IN ('starting', 'stopping', 'rebuilding', 'deleting');

-- name: UpdateSessionLastActivity :exec
UPDATE sessions SET last_activity_at = now() WHERE id = $1;

-- name: ListSessionAgents :many
SELECT a.*, sa.status as agent_status FROM agents a
JOIN session_agents sa ON a.id = sa.agent_id
WHERE sa.session_id = $1
ORDER BY a.name;

-- name: ListSessionMembers :many
SELECT u.* FROM users u
JOIN session_members sm ON u.id = sm.user_id
WHERE sm.session_id = $1
ORDER BY u.name;

-- name: AddSessionMember :exec
INSERT INTO session_members (session_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: AddSessionAgent :exec
INSERT INTO session_agents (session_id, agent_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveAllSessionAgents :exec
DELETE FROM session_agents WHERE session_id = $1;

-- name: UpdateSessionAgentStatus :exec
UPDATE session_agents SET status = $3
WHERE session_id = $1 AND agent_id = $2;

-- name: GetUnreadCount :one
SELECT COUNT(*)::int FROM messages m
JOIN session_members sm ON m.session_id = sm.session_id AND sm.user_id = $2
WHERE m.session_id = $1
AND m.created_at > sm.last_read_at;

-- name: MarkSessionRead :exec
UPDATE session_members SET last_read_at = now()
WHERE session_id = $1 AND user_id = $2;
