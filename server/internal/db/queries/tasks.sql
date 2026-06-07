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
INSERT INTO tasks (session_id, agent_id, requested_by, anchor_message_id, prompt, state, seq)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = $1;

-- name: UpdateTaskState :exec
UPDATE tasks SET state = $2, seq = $3, updated_at = now() WHERE id = $1;

-- name: SetTaskAwaitingInput :exec
UPDATE tasks SET state = 'awaiting_input', pending_question = $2, seq = $3, updated_at = now() WHERE id = $1;

-- name: ResolveTaskInput :exec
UPDATE tasks SET state = 'running', pending_question = '', seq = $2, updated_at = now() WHERE id = $1;

-- name: FinishTask :exec
UPDATE tasks SET state = $2, reply = $3, work = $4, pending_question = '', seq = $5, updated_at = now() WHERE id = $1;

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
SELECT * FROM tasks WHERE session_id = $1 ORDER BY created_at ASC;

-- name: ListAgentTasks :many
-- An agent's tasks in creation order — drives queue-position derivation (R12)
-- and promotion (R13) in the scheduler.
SELECT * FROM tasks WHERE session_id = $1 AND agent_id = $2 ORDER BY created_at ASC;

-- name: ListTaskActions :many
SELECT * FROM task_actions WHERE task_id = $1 ORDER BY seq ASC, created_at ASC;

-- name: ListSessionTaskActions :many
-- All actions for a session's tasks, for the snapshot read (joined to tasks so
-- it is scoped by session in one query).
SELECT ta.* FROM task_actions ta
JOIN tasks t ON t.id = ta.task_id
WHERE t.session_id = $1
ORDER BY ta.task_id, ta.seq ASC;

-- name: GetPiSessionID :one
SELECT pi_session_id FROM session_agents WHERE session_id = $1 AND agent_id = $2;

-- name: UpdatePiSessionID :exec
UPDATE session_agents SET pi_session_id = $3 WHERE session_id = $1 AND agent_id = $2;

-- name: IsSessionMember :one
-- Membership gate for steering + snapshot authorization (KTD14).
SELECT EXISTS (
    SELECT 1 FROM session_members WHERE session_id = $1 AND user_id = $2
) AS is_member;

-- name: FailStuckTasks :exec
-- Boot recovery (KTD10): tasks left running/awaiting_input by a crash are
-- reconciled to failed before the scheduler starts.
UPDATE tasks SET state = 'failed', updated_at = now() WHERE state IN ('running', 'awaiting_input');

-- name: ClearStuckPiSessions :exec
-- Boot recovery companion: clear pi_session_id for (session, agent) pairs with
-- a stuck in-flight task, so relaunch won't resume a dead Pi session. MUST run
-- BEFORE FailStuckTasks — it keys on the pre-failure states, not on 'failed'
-- (which would also clear legitimately-failed historical tasks).
UPDATE session_agents sa
SET pi_session_id = ''
WHERE EXISTS (
    SELECT 1 FROM tasks t
    WHERE t.session_id = sa.session_id AND t.agent_id = sa.agent_id
      AND t.state IN ('running', 'awaiting_input')
);
