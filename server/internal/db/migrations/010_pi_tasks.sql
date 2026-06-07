-- +goose Up

-- Pi session continuity (KTD13): the Pi RPC session id to re-attach to on
-- relaunch (the analog of the retired claude --resume). Parallel to
-- claude_session_id, which is left in place — non-destructive migration. Empty
-- string is the "not yet established" sentinel; resume logic tests <> ''.
ALTER TABLE session_agents ADD COLUMN pi_session_id TEXT NOT NULL DEFAULT '';

-- Durable per-session monotonic event-sequence allocator backing the
-- AgentRunEvent stream (KTD6). A dedicated table (rather than MAX(seq)+1 over
-- tasks/task_actions) because seq is one namespace shared across both tables,
-- allocated in a single step. Bumped in the SAME transaction as each task/
-- action write, so the broadcast seq and persisted state never diverge, and it
-- survives restart (seeded from the row, not an in-memory counter).
CREATE TABLE session_event_seq (
    session_id UUID PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    next_seq   BIGINT NOT NULL DEFAULT 1
);

-- Super Threads tasks: one row per @mention-spawned agent run. Anchored to the
-- channel message that created it. tasks die with their session (CASCADE,
-- matching messages); a deleted anchor message must not delete or block the
-- task (SET NULL); requested_by is a bare UUID (matching messages.author_id)
-- so a user soft-delete neither orphans nor blocks tasks.
CREATE TABLE tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id        UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_id          UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    requested_by      UUID,
    anchor_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    prompt            TEXT NOT NULL DEFAULT '',
    state             TEXT NOT NULL DEFAULT 'queued'
        CHECK (state IN ('queued', 'running', 'awaiting_input', 'done', 'failed', 'cancelled')),
    seq               BIGINT NOT NULL DEFAULT 0,
    pending_question  TEXT NOT NULL DEFAULT '',
    reply             TEXT NOT NULL DEFAULT '',
    work              JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Snapshot + queue-position walks (R12) read by (session, agent, state); the
-- seq index supports ordered snapshot reads.
CREATE INDEX idx_tasks_session_agent_state ON tasks (session_id, agent_id, state);
CREATE INDEX idx_tasks_session_seq ON tasks (session_id, seq);

-- Action log: one row per tool call within a task. A separate table (not JSONB
-- on tasks) so concurrent appends never contend on a single document. Unique on
-- (task_id, call_id) makes append idempotent — a re-attach replay of an
-- already-persisted tool call is a no-op (KTD13).
CREATE TABLE task_actions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    call_id    TEXT NOT NULL,
    seq        BIGINT NOT NULL DEFAULT 0,
    tool       TEXT NOT NULL DEFAULT '',
    arg        TEXT NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL DEFAULT '',
    stat       TEXT NOT NULL DEFAULT '',
    diff       JSONB,
    out        JSONB,
    status     TEXT NOT NULL DEFAULT 'started'
        CHECK (status IN ('started', 'completed', 'error', 'interrupted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, call_id)
);

CREATE INDEX idx_task_actions_task_seq ON task_actions (task_id, seq);

-- +goose Down
DROP TABLE task_actions;
DROP TABLE tasks;
DROP TABLE session_event_seq;
ALTER TABLE session_agents DROP COLUMN pi_session_id;
