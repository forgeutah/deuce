---
title: "feat: Session Creation Dialog with DevPod Workspace"
type: feat
status: active
date: 2026-05-08
origin: docs/brainstorms/2026-05-08-session-creation-devpod-brainstorm.md
---

# Session Creation Dialog with DevPod Workspace

## Overview

Build the full session creation flow: a frontend dialog triggered by the "+" button, connected to the backend API, with DevPod integration to spin up an isolated dev container from the selected GitHub repo. Users pick a repo from their GitHub repos, name the session (Slack-style slug), select agents, and hit create. Chat works immediately; workspace starts in the background.

(See brainstorm: `docs/brainstorms/2026-05-08-session-creation-devpod-brainstorm.md`)

## Proposed Solution

Three pieces of work:

1. **Frontend**: CreateSessionDialog component with name input (auto-slugify), GitHub repo dropdown, agent checkboxes
2. **Backend - GitHub API**: New endpoint `GET /api/github/repos` that proxies GitHub repo listing (server holds the PAT)
3. **Backend - DevPod**: Workspace lifecycle manager that calls `devpod up` via os/exec when a session is created, monitors completion, broadcasts status via WebSocket

## Implementation Phases

### Phase 1: GitHub Repo Listing API

Add a server-side endpoint that lists the user's GitHub repos using a PAT from an env var.

**Tasks:**

- [ ] `server/internal/config/config.go` — Add `GitHubToken string` env var (`GITHUB_TOKEN`)
- [ ] `go get github.com/google/go-github/v68` — Install GitHub client library (use a stable recent version)
- [ ] `server/internal/handler/github.go` — New handler `GET /api/github/repos`
  - Create `github.Client` with PAT from config
  - Call `client.Repositories.ListByAuthenticatedUser` with `Affiliation: "owner"`, `Sort: "updated"`, `PerPage: 100`
  - Paginate to fetch all repos
  - Return simplified response: `[{ name, fullName, cloneUrl, description, language, private, defaultBranch }]`
  - Cache results for 5 minutes (in-memory, per-user) to avoid burning rate limits
- [ ] `server/internal/server/server.go` — Register `GET /api/github/repos` route
- [ ] `src/lib/api.ts` — Add `listGitHubRepos()` function
- [ ] Test: `curl http://localhost:8080/api/github/repos` returns repo list

**Success criteria:** Frontend can fetch the user's GitHub repos from the backend.

### Phase 2: DevPod Workspace Manager

Add a workspace lifecycle manager that calls the DevPod CLI to create and monitor workspaces.

**Tasks:**

- [ ] `server/internal/workspace/manager.go` — DevPod workspace manager
  - `Create(ctx, workspaceID, repoURL string) error` — Runs `devpod up <repoURL> --id <workspaceID> --ide none --log-output json` via `os/exec.CommandContext`
  - `Stop(ctx, workspaceID string) error` — Runs `devpod stop <workspaceID>`
  - `Delete(ctx, workspaceID string) error` — Runs `devpod delete <workspaceID> --force --ignore-not-found`
  - `Status(ctx, workspaceID string) (string, error)` — Runs `devpod status <workspaceID> --output json`
  - All methods log output via slog
  - Runs `devpod up` synchronously (blocking) — the caller wraps it in a goroutine
- [ ] `server/internal/config/config.go` — Add `DevPodBin string` env var (`DEVPOD_BIN`, default `devpod`) and `DevPodProvider string` env var (`DEVPOD_PROVIDER`, default empty = use default provider)
- [ ] Wire into session creation: after `POST /api/sessions` creates the DB record, spawn a goroutine that:
  1. Calls `workspace.Create(ctx, sessionSlug, repoCloneURL)`
  2. On success: update session `workspaceStatus` to `"ready"` in DB, broadcast `session_update` via WebSocket
  3. On failure: update to `"failed"`, broadcast error
- [ ] `server/internal/handler/sessions.go` — Update `CreateSession` to accept `repoUrl` in request body (for the clone URL), store it, and trigger DevPod
- [ ] Add `repo_url` column to sessions table (migration `003_add_session_repo_url.sql`)
- [ ] Add sqlc query for updating workspace status + new column

**DevPod workspace ID rules** (from research):
- Max 48 characters
- Lowercase alphanumeric + hyphens only: `[a-z0-9-]`
- No leading/trailing hyphens
- The session slug (Slack-style) naturally satisfies these constraints

**Success criteria:** Creating a session via API triggers `devpod up` in the background. Workspace status transitions from "starting" to "ready" (or "failed") and broadcasts via WebSocket.

### Phase 3: Frontend - CreateSessionDialog

Build the dialog component and wire it to the "+" button.

**Tasks:**

- [ ] `src/components/session/CreateSessionDialog.tsx` — shadcn Dialog with:
  - **Session name input**: text field with live validation + auto-slugify preview
    - On input: lowercase, replace spaces/underscores with hyphens, strip invalid chars
    - Show the slugified result below: `# auth-module`
    - Validation: required, 1-48 chars, `[a-z0-9-]`, no leading/trailing hyphens
    - Error message if invalid
  - **Repository dropdown**: shadcn Select populated from `GET /api/github/repos`
    - Shows repo name + language badge + private/public indicator
    - Search/filter within dropdown
    - Loading state while fetching repos
  - **Agent checkboxes**: list of agent presets from `GET /api/agents`
    - Each shows agent avatar (colored circle), name, role description
    - Coder pre-checked by default
  - **Create Session button**: disabled until name and repo are selected
  - On submit: `POST /api/sessions` with `{ name, repoUrl, agentIds }`
  - On success: close dialog, select the new session in sidebar
  - On error: show error message in dialog
- [ ] `src/components/layout/SessionSidebar.tsx` — Wire the "+" button `onClick` to open `CreateSessionDialog`
- [ ] State management: dialog uses local React state, no Zustand needed for form fields. On success, the new session arrives via the API response and is added to the store

**Auto-slugify logic:**
```
"Auth Module" → "auth-module"
"My Cool Feature!" → "my-cool-feature"
"  test--name  " → "test-name"
```

**Success criteria:** User clicks "+", fills in the form, creates a session. New session appears in sidebar. DevPod workspace starts in background. Chat works immediately.

### Phase 4: Polish and Edge Cases

- [ ] Handle DevPod not installed: check `devpod version` on server startup, log warning if not available, skip workspace creation gracefully
- [ ] Handle duplicate workspace IDs: before `devpod up`, call `devpod status` to check if ID exists. If collision, append 4-char random suffix
- [ ] Session sidebar: new session appears at top of its project group with "starting" status dot (yellow, animated)
- [ ] Workspace status transition: when WebSocket `session_update` arrives with `workspaceStatus: "ready"`, Terminal and Files tabs become active (this already works from the frontend prototype)
- [ ] GitHub repo fetch error: show "Failed to load repos" with retry button in the dropdown
- [ ] Empty agent selection: valid — creates a session with no agents (just a team chat with workspace)

## Database Changes

```sql
-- 003_add_session_repo_url.sql
-- +goose Up
ALTER TABLE sessions ADD COLUMN repo_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN repo_url;
```

## API Changes

### New Endpoint

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/github/repos` | List authenticated user's GitHub repos |

**Response:**
```json
[
  {
    "name": "forge-api",
    "fullName": "forgeutah/forge-api",
    "cloneUrl": "https://github.com/forgeutah/forge-api.git",
    "description": "Backend API for Forge platform",
    "language": "Go",
    "private": false,
    "defaultBranch": "main"
  }
]
```

### Modified Endpoint

`POST /api/sessions` request body updated:

```json
{
  "name": "auth-module",
  "repoUrl": "https://github.com/forgeutah/forge-api.git",
  "projectId": "uuid",
  "agentIds": ["uuid", "uuid"],
  "memberIds": ["uuid"]
}
```

`repoUrl` is the clone URL from the GitHub repo selection. It's stored on the session and passed to `devpod up`.

## Acceptance Criteria

- [ ] "+" button opens the create session dialog
- [ ] Session name auto-slugifies with live preview (# prefix)
- [ ] GitHub repos load in a searchable dropdown
- [ ] Agent presets shown as checkboxes with colors
- [ ] Creating a session: adds to sidebar, navigates to it, chat works immediately
- [ ] DevPod workspace starts in background (visible as yellow "starting" dot)
- [ ] Workspace transitions to "ready" (green dot) or "failed" (red dot)
- [ ] Terminal and Files tabs gate on workspace readiness
- [ ] Validation: name required, repo required, slug format enforced
- [ ] Graceful degradation: works without DevPod installed (just no workspace)

## Dependencies

- DevPod CLI installed on the server machine (`devpod` in PATH)
- GitHub PAT with `repo` scope in `GITHUB_TOKEN` env var
- Docker (or other DevPod provider) configured

## Sources & References

### Origin

- **Brainstorm:** [docs/brainstorms/2026-05-08-session-creation-devpod-brainstorm.md](../brainstorms/2026-05-08-session-creation-devpod-brainstorm.md) — Slack-style names, GitHub repo picker, DevPod via CLI, immediate chat access

### Internal References

- Session sidebar "+" button: `src/components/layout/SessionSidebar.tsx:140-149`
- Backend CreateSession handler: `server/internal/handler/sessions.go:162-238`
- WebSocket session_update event: `server/internal/ws/events.go`
- Workspace status rendering: `src/components/files/FilesView.tsx`, `src/components/terminal/TerminalView.tsx`

### External References

- [DevPod CLI docs](https://devpod.sh/docs/quickstart/devpod-cli)
- [DevPod `up` command flags](https://github.com/loft-sh/devpod/blob/main/cmd/up.go)
- [DevPod workspace ID rules](https://github.com/loft-sh/devpod/blob/main/pkg/workspace/id.go) — max 48 chars, `[a-z0-9-]`
- [google/go-github](https://github.com/google/go-github)
- [GitHub REST API - List repos](https://docs.github.com/en/rest/repos/repos)
