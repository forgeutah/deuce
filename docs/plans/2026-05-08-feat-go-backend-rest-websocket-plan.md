---
title: "feat: Go Backend with REST API and WebSocket Hub"
type: feat
status: active
date: 2026-05-08
origin: docs/brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md
---

# Go Backend with REST API and WebSocket Hub

## Overview

Add a Go backend server to Deuce that replaces the hardcoded seed data with a real Postgres database and REST API, and adds real-time updates via a WebSocket hub. The frontend will be wired to fetch from real endpoints and receive live updates. Agent responses remain canned/simulated (moved server-side), no auth, no DevPod integration.

## Problem Statement

The Deuce frontend prototype runs entirely on hardcoded data loaded into a Zustand store at startup. There are no API calls, no persistence, and no multi-client support. To progress toward a usable product, we need a real backend that persists data, serves it via REST, and pushes real-time updates via WebSockets so multiple users can collaborate in the same session.

(See brainstorm: `docs/brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md` — session-centric monolith architecture, Go backend, Postgres, WebSocket hub.)

## Proposed Solution

A Go monolith server using chi router, Postgres via sqlc/pgx, and coder/websocket. The server:
1. Serves REST endpoints matching the data shapes the frontend already expects
2. Runs a WebSocket hub that broadcasts session events to connected clients
3. Simulates agent responses server-side (canned responses with delays, same as the frontend does now)
4. Proxies through Vite in development

## Tech Stack

| Layer | Package | Version |
|-------|---------|---------|
| Router | `github.com/go-chi/chi/v5` | v5.x |
| WebSocket | `github.com/coder/websocket` | v1.8+ |
| DB Driver | `github.com/jackc/pgx/v5` | v5.x |
| Query Codegen | `github.com/sqlc-dev/sqlc` | v1.27+ (CLI) |
| Migrations | `github.com/pressly/goose/v3` | v3.x |
| Hot Reload | `github.com/air-verse/air` | latest |
| UUID | `github.com/google/uuid` | v1.x |
| Env Config | `github.com/caarlos0/env/v11` | v11.x |
| JSON | stdlib `encoding/json` | — |
| Logging | `log/slog` (stdlib) | — |

## Technical Approach

### Go Project Layout

```
server/
├── main.go                     # Entry point, server startup
├── go.mod
├── go.sum
├── .air.toml                   # Hot reload config
├── internal/
│   ├── config/
│   │   └── config.go           # Env-based configuration
│   ├── server/
│   │   ├── server.go           # HTTP server setup
│   │   ├── routes.go           # Route registration
│   │   └── middleware.go       # Logging, CORS, recovery, user-identity
│   ├── handler/
│   │   ├── teams.go            # GET /api/teams
│   │   ├── projects.go         # GET /api/projects
│   │   ├── sessions.go         # CRUD /api/sessions
│   │   ├── messages.go         # GET/POST /api/sessions/:id/messages
│   │   ├── activities.go       # GET /api/sessions/:id/activities
│   │   ├── agents.go           # GET /api/agents, PUT /api/sessions/:id/agents
│   │   └── users.go            # GET /api/me
│   ├── ws/
│   │   ├── hub.go              # WebSocket hub (manages connections + subscriptions)
│   │   ├── client.go           # Per-connection handler (read/write loops)
│   │   └── events.go           # Event type definitions + serialization
│   ├── agent/
│   │   └── simulator.go        # Canned agent response simulator
│   ├── db/
│   │   ├── db.go               # Connection pool setup
│   │   ├── queries/            # sqlc SQL files
│   │   │   ├── teams.sql
│   │   │   ├── projects.sql
│   │   │   ├── sessions.sql
│   │   │   ├── messages.sql
│   │   │   ├── activities.sql
│   │   │   ├── agents.sql
│   │   │   └── users.sql
│   │   └── migrations/
│   │       └── 001_initial_schema.sql
│   └── model/
│       └── models.go           # Shared domain types (if needed beyond sqlc-generated)
├── sqlc.yaml                   # sqlc configuration
└── Makefile                    # dev, build, migrate, generate commands
```

The Go server lives in `server/` alongside the existing frontend at the repo root. This keeps the monorepo clean.

### Database Schema

```sql
-- 001_initial_schema.sql (goose migration)

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

-- Seed agent presets
INSERT INTO agents (id, name, role, color, color_muted, provider, model, description) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Coder', 'coder', '#58a6ff', '#0c2d6b', 'Anthropic', 'Claude Sonnet 4', 'Writes and modifies code'),
    ('00000000-0000-0000-0000-000000000002', 'Reviewer', 'reviewer', '#BE8FFF', '#3c1e70', 'Anthropic', 'Claude Sonnet 4', 'Reviews code changes'),
    ('00000000-0000-0000-0000-000000000003', 'Planner', 'planner', '#3fb950', '#033a16', 'OpenAI', 'GPT-4o', 'Creates implementation plans'),
    ('00000000-0000-0000-0000-000000000004', 'Tester', 'tester', '#d29922', '#4b2900', 'Anthropic', 'Claude Sonnet 4', 'Writes and runs tests'),
    ('00000000-0000-0000-0000-000000000005', 'Designer', 'designer', '#f778ba', '#5e103e', 'OpenAI', 'GPT-4o', 'UI/UX suggestions');

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
```

### REST API Endpoints

All endpoints return JSON. Error responses use `{ "error": { "code": "ERROR_CODE", "message": "Human-readable message" } }`.

| Method | Path | Description | Request Body | Response |
|--------|------|-------------|-------------|----------|
| GET | `/api/me` | Current user (hardcoded for no-auth) | — | `User` |
| GET | `/api/teams` | List user's teams with members | — | `Team[]` (includes nested `members`) |
| GET | `/api/projects?teamId=` | List projects (teamId optional) | — | `Project[]` |
| GET | `/api/agents` | List available agent presets | — | `Agent[]` |
| GET | `/api/sessions?projectId=` | List sessions (projectId optional) | — | `SessionSummary[]` (lightweight, includes unread count for current user) |
| GET | `/api/sessions/:id` | Get full session details | — | `Session` (includes agents with per-session status, members) |
| POST | `/api/sessions` | Create session | `{ name, projectId, agentIds[], memberIds[] }` | `Session` |
| PATCH | `/api/sessions/:id` | Update session | `{ status?, planContent?, workspaceStatus? }` | `Session` |
| GET | `/api/sessions/:id/messages?before=&limit=` | Paginated messages (cursor-based) | — | `{ messages: Message[], cursor: string, hasMore: bool }` |
| POST | `/api/sessions/:id/messages` | Send message | `{ content, mentions[] }` | `Message` |
| GET | `/api/sessions/:id/activities?limit=` | Recent activities | — | `ActivityItem[]` |
| PUT | `/api/sessions/:id/agents` | Replace agent roster | `{ agentIds: string[] }` | `Agent[]` (with per-session status) |

**Current user identity (no-auth):** A middleware injects a default user ID from config (e.g., env var `DEUCE_USER_ID`). All requests are attributed to this user. The `GET /api/me` endpoint returns this user's data. This will be replaced with OAuth when auth is added.

**Message pagination:** Cursor-based using `?before=<messageId>&limit=50`. Messages returned newest-first by `created_at DESC`. Frontend reverses for display. First load omits `before` to get the most recent messages.

**Unread counts:** Computed server-side as `COUNT(messages WHERE created_at > session_members.last_read_at)` for the current user. Updated when the user views a session (implicit via WebSocket `join` or explicit PATCH to `session_members.last_read_at`).

**Session status transitions:**
- `active` ↔ `paused`
- `active` → `archived`
- `paused` → `archived`
- `archived` is terminal (no un-archiving in v0)

### WebSocket Protocol

Single endpoint: `GET /ws`

**Wire format:** JSON messages with `type` and `payload`.

#### Client → Server Messages

```json
// Subscribe to a session (full events)
{ "type": "join", "sessionId": "uuid" }

// Unsubscribe from a session
{ "type": "leave", "sessionId": "uuid" }

// Mark session as read (updates last_read_at)
{ "type": "mark_read", "sessionId": "uuid" }
```

#### Server → Client Messages

```json
// New message in a session (NOT sent to the originating client)
{
  "type": "new_message",
  "sessionId": "uuid",
  "payload": { /* full Message object */ }
}

// Agent status changed
{
  "type": "agent_status",
  "sessionId": "uuid",
  "payload": { "agentId": "uuid", "status": "working|idle|error" }
}

// Agent is "thinking" (typing indicator)
{
  "type": "typing_indicator",
  "sessionId": "uuid",
  "payload": { "agentId": "uuid", "active": true|false }
}

// New activity item
{
  "type": "activity_update",
  "sessionId": "uuid",
  "payload": { /* full ActivityItem object */ }
}

// Session updated (status, workspace, new session created)
{
  "type": "session_update",
  "sessionId": "uuid",
  "payload": { /* partial Session fields that changed */ }
}

// Unread count changed (sent for ALL sessions user is member of, not just joined)
{
  "type": "unread_update",
  "sessionId": "uuid",
  "payload": { "unreadCount": 5 }
}
```

**Subscription model:**
- On WebSocket connect, the server automatically subscribes the client to lightweight events (`unread_update`, `session_update`) for ALL sessions the user is a member of
- `join` adds subscription to heavy events (`new_message`, `agent_status`, `typing_indicator`, `activity_update`) for a specific session
- `leave` removes heavy subscription but keeps lightweight events
- `mark_read` updates `last_read_at` and recomputes unread count

**Broadcast rules:**
- `new_message` from a human: broadcast to all joined clients EXCEPT the sender
- `new_message` from an agent: broadcast to ALL joined clients (including the one who triggered the @mention)
- All other events: broadcast to all subscribed clients

**Heartbeat:** Server sends WebSocket ping every 30s. Client must respond with pong within 10s or connection is closed.

**Reconnection:** Client responsibility. On reconnect, client re-sends `join` for the active session and fetches missed messages via REST.

### Agent Simulation (Server-Side)

When a message with `mentions` is POSTed:
1. Server persists the human message
2. Server broadcasts `new_message` to other clients
3. For each mentioned agent in the session:
   a. Set agent status to `"working"` → broadcast `agent_status`
   b. Send `typing_indicator` with `active: true`
   c. Wait 1.5–3.5 seconds (random delay)
   d. Generate canned response based on agent role (same content as frontend's `getAgentResponse`)
   e. Persist agent message
   f. Broadcast `new_message` (agent message to ALL clients)
   g. Send `typing_indicator` with `active: false`
   h. Set agent status to `"idle"` → broadcast `agent_status`
   i. Create activity item → broadcast `activity_update`

Agent mentions are processed sequentially per agent per session (queued) to avoid interleaved responses.

### Development Workflow

**Vite proxy config** (add to existing `vite.config.ts`):

```typescript
server: {
  proxy: {
    "/api": "http://localhost:8080",
    "/ws": { target: "ws://localhost:8080", ws: true },
  },
},
```

**Dev commands:**
- Terminal 1: `cd server && air` (Go with hot reload on :8080)
- Terminal 2: `npm run dev` (Vite on :5173, proxies to Go)
- Access: `http://localhost:5173`

**Database:** Local Postgres via Docker Compose or installed.

```yaml
# docker-compose.yml (at repo root)
services:
  postgres:
    image: postgres:17
    environment:
      POSTGRES_DB: deuce
      POSTGRES_USER: deuce
      POSTGRES_PASSWORD: deuce
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

### Frontend Integration

Replace seed data with API calls. Key changes:

1. **`src/app/App.tsx`** — Replace `seedStore()` with `fetch` calls on mount:
   ```
   GET /api/me → store current user
   GET /api/teams → store.setTeams()
   GET /api/projects → store.setProjects()
   GET /api/sessions → store.setSessions()
   Connect WebSocket
   ```

2. **`src/stores/session-store.ts`** — Add async actions:
   - `fetchSessions()`, `fetchMessages(sessionId)`, `fetchActivities(sessionId)`
   - `sendMessage(sessionId, content, mentions)` → POST + optimistic update
   - `createSession(...)` → POST
   - `updateSession(id, fields)` → PATCH
   - Message status: add `"pending"` to MessageStatus for optimistic sends

3. **`src/hooks/use-websocket.ts`** — New hook:
   - Connects to `/ws` on mount
   - Handles all server event types, dispatching to store actions
   - Sends `join`/`leave` when active session changes
   - Sends `mark_read` when viewing a session
   - Auto-reconnects with exponential backoff

4. **`src/components/chat/ChatView.tsx`** — Remove `simulateAgentResponse`. `handleSend` calls `store.sendMessage()` which POSTs to the API. Agent responses arrive via WebSocket events.

5. **Files tab and Terminal tab** — Remain on mock data for this pass (no DevPod).

### Implementation Phases

#### Phase 1: Go Project + Database

- [ ] `server/go.mod` — Initialize Go module
- [ ] `server/main.go` — Basic entry point with chi server
- [ ] `server/internal/config/config.go` — Env-based config (port, database URL, default user ID)
- [ ] `server/internal/db/db.go` — pgx connection pool setup
- [ ] `server/internal/db/migrations/001_initial_schema.sql` — Full schema + seed agents
- [ ] `server/sqlc.yaml` — sqlc configuration targeting pgx
- [ ] `server/internal/db/queries/*.sql` — All sqlc query files
- [ ] `server/Makefile` — `make dev`, `make migrate`, `make generate`, `make build`
- [ ] `docker-compose.yml` — Postgres container
- [ ] `server/.air.toml` — Hot reload config
- [ ] Run `sqlc generate`, verify generated code compiles
- [ ] Run migration against local Postgres, verify schema

**Success:** `make migrate` runs clean. `sqlc generate` produces compilable Go code.

#### Phase 2: REST API Handlers

- [ ] `server/internal/server/server.go` — HTTP server with chi, CORS, logging, recovery
- [ ] `server/internal/server/routes.go` — Register all API routes
- [ ] `server/internal/server/middleware.go` — User identity middleware (injects hardcoded user ID)
- [ ] `server/internal/handler/users.go` — `GET /api/me`
- [ ] `server/internal/handler/teams.go` — `GET /api/teams`
- [ ] `server/internal/handler/projects.go` — `GET /api/projects`
- [ ] `server/internal/handler/agents.go` — `GET /api/agents`
- [ ] `server/internal/handler/sessions.go` — `GET /api/sessions`, `GET /api/sessions/:id`, `POST /api/sessions`, `PATCH /api/sessions/:id`
- [ ] `server/internal/handler/messages.go` — `GET /api/sessions/:id/messages` (cursor pagination), `POST /api/sessions/:id/messages`
- [ ] `server/internal/handler/activities.go` — `GET /api/sessions/:id/activities`
- [ ] Seed script or migration to populate initial teams, users, projects, sessions (matching current frontend seed data)
- [ ] Test all endpoints with curl

**Success:** All endpoints return JSON matching the frontend's TypeScript types. Seed data matches what the frontend currently displays.

#### Phase 3: WebSocket Hub

- [ ] `server/internal/ws/events.go` — Event type constants + JSON marshaling for all message types
- [ ] `server/internal/ws/hub.go` — Hub struct: manages client registry, per-session subscription sets, broadcast channels. Runs in a goroutine
- [ ] `server/internal/ws/client.go` — Client struct: per-connection read/write goroutines, handles join/leave/mark_read, dispatches to hub
- [ ] WebSocket endpoint handler: upgrade connection, register with hub, run client
- [ ] Wire hub into message handler: after POST message, broadcast `new_message` (exclude sender)
- [ ] Wire hub into session handler: after PATCH, broadcast `session_update`
- [ ] Heartbeat: server pings every 30s, disconnects unresponsive clients
- [ ] `unread_update` events: compute and send when messages arrive for sessions user is member of

**Success:** Two browser tabs can be open. Send a message in one, it appears in the other via WebSocket.

#### Phase 4: Agent Simulation

- [ ] `server/internal/agent/simulator.go` — Canned response generator (same responses as frontend's `getAgentResponse` + `getExpandableContent`)
- [ ] Wire into message handler: detect mentions, queue agent response with delay
- [ ] Sequential processing: one goroutine per session with a channel for agent work items
- [ ] Broadcast full event sequence: agent_status → typing_indicator → new_message → activity_update → agent_status
- [ ] Create activity items server-side as side effects of agent responses

**Success:** Send `@Coder fix something` — see typing indicator, then agent response with expandable diff, all via WebSocket.

#### Phase 5: Frontend Integration

- [ ] `vite.config.ts` — Add proxy config for `/api` and `/ws`
- [ ] `src/hooks/use-websocket.ts` — WebSocket hook with auto-reconnect
- [ ] `src/stores/session-store.ts` — Add async fetch/send actions
- [ ] `src/app/App.tsx` — Replace `seedStore()` with API fetch on mount + WebSocket connect
- [ ] `src/components/chat/ChatView.tsx` — Remove `simulateAgentResponse`, wire `handleSend` to POST API
- [ ] `src/components/plan/PlanView.tsx` — Debounce plan updates, PATCH to API
- [ ] `src/components/layout/SessionSidebar.tsx` — Unread counts from API (computed server-side)
- [ ] Test multi-tab: open two tabs, send message in one, verify it appears in the other
- [ ] Test agent simulation: @mention agent, verify typing indicator + response via WebSocket

**Success:** The app works identically to the mock version but with persistent data. Opening two browser tabs shows real-time sync.

## Acceptance Criteria

### Functional Requirements

- [ ] Go server starts, connects to Postgres, serves REST API on :8080
- [ ] All REST endpoints return data matching frontend TypeScript types
- [ ] Messages persist across page refresh
- [ ] Cursor-based message pagination works (load older messages)
- [ ] Sessions can be created, updated (status, plan), and listed
- [ ] Agent roster can be modified per session
- [ ] WebSocket connection established on app load
- [ ] Real-time message delivery between multiple clients
- [ ] Agent simulation works server-side (@mention → typing indicator → canned response)
- [ ] Unread counts update in real-time across clients
- [ ] Session status changes broadcast to all clients
- [ ] Activity feed updates in real-time
- [ ] Frontend loads from API instead of seed data

### Non-Functional Requirements

- [ ] WebSocket reconnects automatically after disconnect (exponential backoff)
- [ ] Message send uses optimistic update (appears instantly, confirmed by server)
- [ ] Plan updates debounced (300ms) before PATCH
- [ ] Server handles 10+ concurrent WebSocket connections without issues
- [ ] Database migrations run idempotently via goose

### Quality Gates

- [ ] `go vet ./...` passes
- [ ] `go build ./...` succeeds
- [ ] sqlc generates without errors
- [ ] Frontend TypeScript still compiles with no errors
- [ ] Manual test: two browser tabs show real-time sync

## Dependencies & Prerequisites

- Go 1.22+ installed
- Docker (for Postgres) or local Postgres 15+
- sqlc CLI installed (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`)
- goose CLI installed (`go install github.com/pressly/goose/v3/cmd/goose@latest`)
- Air installed (`go install github.com/air-verse/air@latest`)

## Sources & References

### Origin

- **Architecture brainstorm:** [docs/brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md](../brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md) — Go monolith, Postgres, WebSocket hub, session-centric architecture, hybrid agent model
- **UX brainstorm:** [docs/brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md](../brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md) — Chat-first UI, @mention interaction, real-time updates

### Internal References

- Frontend types: `src/types/index.ts` — all data models the API must serve
- Zustand store: `src/stores/session-store.ts` — actions that become API calls / WS handlers
- Seed data: `src/mocks/data/seed.ts` — exact data shapes for seeding the database
- Agent simulation: `src/components/chat/ChatView.tsx:162-206` — logic to move server-side

### External References

- [chi router](https://github.com/go-chi/chi)
- [coder/websocket](https://github.com/coder/websocket)
- [sqlc docs](https://docs.sqlc.dev/)
- [pgx driver](https://github.com/jackc/pgx)
- [goose migrations](https://github.com/pressly/goose)
- [Air hot reload](https://github.com/air-verse/air)
- [Vite proxy config](https://vite.dev/config/server-options.html#server-proxy)
