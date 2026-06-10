-- name: AllocateEventSeq :one
-- Allocate the next per-session event seq, creating the counter row on first
-- use. Returns the allocated value; the stored next_seq is left pointing at the
-- following value. Run inside the same tx as the state write it stamps (KTD6).
INSERT INTO session_event_seq (session_id, next_seq)
VALUES ($1, 2)
ON CONFLICT (session_id)
DO UPDATE SET next_seq = session_event_seq.next_seq + 1
RETURNING (next_seq - 1)::bigint AS seq;

-- name: PeekEventSeq :one
-- Highest allocated seq for a session (next_seq - 1), or 0 if none. Used as the
-- snapshot's latest_seq lower bound and to seed any in-memory cache on boot.
SELECT COALESCE((SELECT next_seq - 1 FROM session_event_seq WHERE session_id = $1), 0)::bigint AS seq;

-- name: CreateTask :one
INSERT INTO tasks (session_id, requested_by, anchor_message_id, prompt, state, seq)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = $1;

-- name: UpdateTaskState :exec
UPDATE tasks SET state = $2, seq = $3, updated_at = now() WHERE id = $1;

-- name: SetTaskAwaitingInput :exec
UPDATE tasks SET state = 'awaiting_input', pending_question = $2, pending_question_kind = $3, pending_question_options = $4, seq = $5, updated_at = now() WHERE id = $1;

-- name: ResolveTaskInput :exec
UPDATE tasks SET state = 'running', pending_question = '', pending_question_kind = '', pending_question_options = '{}', seq = $2, updated_at = now() WHERE id = $1;

-- name: FinishTask :exec
UPDATE tasks SET state = $2, reply = $3, work = $4, pending_question = '', pending_question_kind = '', pending_question_options = '{}', seq = $5, updated_at = now() WHERE id = $1;

-- name: AppendAction :exec
-- Idempotent on (task_id, call_id): a replayed tool-start after re-attach is a
-- no-op (KTD13).
INSERT INTO task_actions (task_id, call_id, seq, tool, arg, status)
VALUES ($1, $2, $3, $4, $5, 'started')
ON CONFLICT (task_id, call_id) DO NOTHING;

-- name: CompleteAction :exec
UPDATE task_actions
SET status = $3, text = $4, out = $5, diff = $6, stat = $7, seq = $8
WHERE task_id = $1 AND call_id = $2;

-- name: ForceResolveOpenActions :exec
-- On task terminal, any still-'started' action is forced terminal so the
-- snapshot can never show a perpetually-spinning action (KTD: orphan resolve).
UPDATE task_actions SET status = 'interrupted' WHERE task_id = $1 AND status = 'started';

-- name: ListSessionTasks :many
-- A session's tasks in creation order — the snapshot read.
SELECT * FROM tasks WHERE session_id = $1 ORDER BY created_at ASC;

-- The scheduler's hot-path lookups below are filtered server-side against the
-- (session_id, state) index from migration 013 instead of walking the
-- session's whole task history.

-- name: GetActiveTaskID :many
-- The session's running-or-awaiting task (at most one, R11). LIMIT 1 + :many
-- instead of :one so "no active task" is an empty slice, not an error.
SELECT id FROM tasks
WHERE session_id = $1 AND state IN ('running', 'awaiting_input')
ORDER BY created_at ASC LIMIT 1;

-- name: GetNextQueuedTask :many
SELECT id, prompt FROM tasks
WHERE session_id = $1 AND state = 'queued'
ORDER BY created_at ASC LIMIT 1;

-- name: ListQueuedTaskIDs :many
SELECT id FROM tasks
WHERE session_id = $1 AND state = 'queued'
ORDER BY created_at ASC;

-- name: CountQueuedTasks :one
SELECT count(*) FROM tasks WHERE session_id = $1 AND state = 'queued';

-- name: ListTaskActions :many
SELECT * FROM task_actions WHERE task_id = $1 ORDER BY seq ASC, created_at ASC;

-- name: ListSessionTaskActions :many
-- All actions for a session's tasks, for the snapshot read (joined to tasks so
-- it is scoped by session in one query).
SELECT ta.* FROM task_actions ta
JOIN tasks t ON t.id = ta.task_id
WHERE t.session_id = $1
ORDER BY ta.task_id, ta.seq ASC;

-- name: IsSessionMember :one
-- Membership gate for steering + snapshot authorization (KTD14).
SELECT EXISTS (
    SELECT 1 FROM session_members WHERE session_id = $1 AND user_id = $2
) AS is_member;

-- name: FailStuckTasks :exec
-- Boot recovery (KTD10): tasks left running/awaiting_input by a crash are
-- reconciled to failed before the scheduler starts. (The pre-013 companion
-- ClearStuckPiSessions is gone with session_agents.pi_session_id — Pi
-- resume-across-restart was never wired up, so there is no session id to
-- clear; every relaunch starts a fresh Pi process.)
UPDATE tasks SET state = 'failed', updated_at = now() WHERE state IN ('running', 'awaiting_input');
