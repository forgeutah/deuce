-- +goose Up

-- Collapse the multi-agent model into a single built-in agent "deuce"
-- (docs/plans/2026-06-09-001-refactor-single-deuce-agent-plan.md, R2/R3/R12).
-- Destructive by design — multi-agent rosters, role metadata, and per-pair Pi
-- session state are discarded. ORDER MATTERS: tasks.agent_id carries
-- ON DELETE CASCADE to agents, so the column must go BEFORE any agent rows are
-- deleted or all task history silently cascade-deletes.

-- 1. Drop tasks.agent_id. Dropping the column drops its FK constraint and the
--    (session_id, agent_id, state) index with it. Tasks are session-scoped now.
ALTER TABLE tasks DROP COLUMN agent_id;

-- Replacement index for the scheduler's per-session queue/state walks.
CREATE INDEX idx_tasks_session_state ON tasks (session_id, state);

-- 2. Cancel still-queued tasks. Boot recovery only fails running/awaiting_input;
--    a queued task carrying a stale persona-targeted prompt must not be
--    promoted under deuce days later.
UPDATE tasks SET state = 'cancelled', updated_at = now() WHERE state = 'queued';

-- 3. Repoint historical agent-authored messages to deuce so the visibility
--    filter (pinned to deuce's UUID) treats them consistently. The IN-subquery
--    guard excludes the nil-UUID system-notice sentinel, which is not an agents
--    row and must stay nil (and visible).
UPDATE messages
SET author_id = '00000000-0000-0000-0000-00000000000d'
WHERE author_type = 'agent'
  AND author_id IN (SELECT id FROM agents);

-- 4. Drop the per-session roster (takes claude_session_id and pi_session_id
--    with it — Pi resume-across-restart was never wired up; re-add on sessions
--    when actually implemented).
DROP TABLE session_agents;

-- 5. Reshape agents to the single built-in row: id + system_prompt. Name and
--    color render from constants (agent.DeuceAgentName / the frontend DEUCE
--    constant); provider/model are owned by DEUCE_PI_PROVIDER / DEUCE_PI_MODEL.
DELETE FROM agents;
ALTER TABLE agents
    DROP COLUMN name,
    DROP COLUMN role,
    DROP COLUMN color,
    DROP COLUMN color_muted,
    DROP COLUMN provider,
    DROP COLUMN model,
    DROP COLUMN description,
    DROP COLUMN deleted_at,
    DROP COLUMN created_at,
    DROP COLUMN updated_at;
INSERT INTO agents (id, system_prompt)
VALUES ('00000000-0000-0000-0000-00000000000d', '');

-- 6. Mention plumbing is gone — the server parses @deuce from message content.
ALTER TABLE messages DROP COLUMN mentions;
ALTER TABLE activity_items DROP COLUMN agent_id;

-- +goose Down

-- Restores the pre-013 SCHEMA and the five role-agent seed rows so a Down→Up
-- cycle returns to a known dev state (007 precedent). Data is only partially
-- reversible: cancelled queued tasks stay cancelled, repointed message
-- authorship stays on deuce, and session_agents rosters are not restored.

ALTER TABLE activity_items ADD COLUMN agent_id UUID;
ALTER TABLE messages ADD COLUMN mentions TEXT[] NOT NULL DEFAULT '{}';

-- Agents back to the 001+004 shape, then reseed the five role presets
-- (002 seed values + 004's empty system_prompt defaults).
DELETE FROM agents;
ALTER TABLE agents
    ADD COLUMN name TEXT NOT NULL DEFAULT '',
    ADD COLUMN role TEXT NOT NULL DEFAULT '',
    ADD COLUMN color TEXT NOT NULL DEFAULT '',
    ADD COLUMN color_muted TEXT NOT NULL DEFAULT '',
    ADD COLUMN provider TEXT NOT NULL DEFAULT '',
    ADD COLUMN model TEXT NOT NULL DEFAULT '',
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
INSERT INTO agents (id, name, role, color, color_muted, provider, model, description) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Coder',    'coder',    '#58a6ff', '#0c2d6b', 'Anthropic', 'Claude Sonnet 4', 'Writes and modifies code'),
    ('00000000-0000-0000-0000-000000000002', 'Reviewer', 'reviewer', '#BE8FFF', '#3c1e70', 'Anthropic', 'Claude Sonnet 4', 'Reviews code changes'),
    ('00000000-0000-0000-0000-000000000003', 'Planner',  'planner',  '#3fb950', '#033a16', 'OpenAI',    'GPT-4o',          'Creates implementation plans'),
    ('00000000-0000-0000-0000-000000000004', 'Tester',   'tester',   '#d29922', '#4b2900', 'Anthropic', 'Claude Sonnet 4', 'Writes and runs tests'),
    ('00000000-0000-0000-0000-000000000005', 'Designer', 'designer', '#f778ba', '#5e103e', 'OpenAI',    'GPT-4o',          'UI/UX suggestions');

CREATE TABLE session_agents (
    session_id        UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_id          UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    status            TEXT NOT NULL DEFAULT 'idle',
    claude_session_id TEXT NOT NULL DEFAULT '',
    pi_session_id     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (session_id, agent_id)
);

-- tasks.agent_id must be backfilled before NOT NULL + FK can apply against a
-- data-bearing table; existing rows repoint to the Coder seed row.
DROP INDEX idx_tasks_session_state;
ALTER TABLE tasks ADD COLUMN agent_id UUID;
UPDATE tasks SET agent_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE tasks ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE tasks
    ADD CONSTRAINT tasks_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;
CREATE INDEX idx_tasks_session_agent_state ON tasks (session_id, agent_id, state);
