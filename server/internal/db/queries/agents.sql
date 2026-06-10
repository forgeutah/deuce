-- name: GetDeuceAgent :one
-- The agents table holds exactly one row — the built-in "deuce" agent
-- (migration 013; id + system_prompt only, identity renders from constants).
-- Single-row read backs GET /api/agent and the runtime's launch-time
-- system-prompt fetch.
SELECT * FROM agents LIMIT 1;

-- name: UpdateDeuceSystemPrompt :one
UPDATE agents SET system_prompt = $1 RETURNING *;
