-- +goose Up

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    email      TEXT UNIQUE NOT NULL,
    avatar     TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'offline',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE teams (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    slug       TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);

CREATE TABLE projects (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name     TEXT NOT NULL,
    repo_url TEXT NOT NULL DEFAULT '',
    team_id  UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE
);

CREATE TABLE agents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    role        TEXT NOT NULL,
    color       TEXT NOT NULL,
    color_muted TEXT NOT NULL,
    provider    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status           TEXT NOT NULL DEFAULT 'active',
    workspace_status TEXT NOT NULL DEFAULT 'ready',
    plan_content     TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE session_members (
    session_id   UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

CREATE TABLE session_agents (
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'idle',
    PRIMARY KEY (session_id, agent_id)
);

CREATE TABLE messages (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    author_id          UUID NOT NULL,
    author_type        TEXT NOT NULL,
    content            TEXT NOT NULL,
    expandable_content JSONB,
    mentions           TEXT[] NOT NULL DEFAULT '{}',
    status             TEXT NOT NULL DEFAULT 'sent',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_session_created ON messages(session_id, created_at DESC);

CREATE TABLE activity_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    description TEXT NOT NULL,
    agent_id    UUID,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_activity_items_session ON activity_items(session_id, created_at DESC);
CREATE INDEX idx_sessions_project ON sessions(project_id);

-- +goose Down
DROP TABLE IF EXISTS activity_items;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS session_agents;
DROP TABLE IF EXISTS session_members;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS users;
