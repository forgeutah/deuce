# Brainstorm: Session Creation with DevPod Workspace

**Date:** 2026-05-08
**Status:** Draft
**Participants:** Clint Berry, Claude

---

## What We're Building

A complete session creation flow: frontend dialog triggered by the "+" button, connected to the existing backend `POST /api/sessions` endpoint, with DevPod integration to spin up an isolated dev container for each new session.

### User Flow

1. User clicks "+" in the session sidebar
2. **Create Session Dialog** opens with:
   - **Session name** — Slack-style: lowercase, hyphens, no spaces. Auto-slugifies input (e.g., "Auth Module" → "auth-module"). Max 80 chars, `[a-z0-9-]` only. Displayed with `#` prefix
   - **Repository** — Dropdown populated from the user's GitHub repos (requires GitHub API integration). User selects which repo this session works on
   - **Agents** — Checkboxes for available agent presets (Coder, Reviewer, Planner, Tester, Designer). Pre-checked defaults TBD
   - "Create Session" button
3. On submit → `POST /api/sessions` → server creates DB record + starts DevPod workspace in background
4. User lands in the new session immediately — Chat and Plan tabs work right away
5. Terminal and Files tabs show "Workspace warming up..." with a spinner
6. When DevPod finishes → workspace status updates to "ready" → Terminal and Files become active
7. New session appears in other team members' sidebars via WebSocket

### Backend Flow

1. `POST /api/sessions` receives name, repo URL, agent IDs
2. Server creates session in DB (status: "active", workspaceStatus: "starting")
3. Server kicks off DevPod in a goroutine: `devpod up <repo-url> --id <session-slug>`
4. Server monitors DevPod progress, updates workspaceStatus via WebSocket when complete
5. If DevPod fails → workspaceStatus: "failed", broadcast error

---

## Key Decisions

1. **Repo selection via GitHub API** — Dropdown of user's GitHub repos. Requires a GitHub token (personal access token or OAuth token stored at the team/user level). For v1, use a PAT stored in the team settings.

2. **DevPod via CLI (os/exec)** — Shell out to `devpod up` rather than importing Go packages. The CLI is the stable, documented interface. Parse JSON output for status.

3. **Slack-style session names** — Auto-slugify: lowercase, hyphens only, max 80 chars. The slug also becomes the DevPod workspace ID for clean mapping.

4. **Immediate chat, workspace in background** — Users start chatting as soon as the session is created. DevPod spins up asynchronously. Terminal and Files tabs are gated on workspace readiness.

5. **Session name = DevPod workspace ID** — The slugified session name is used as the DevPod `--id` parameter, creating a 1:1 mapping between sessions and workspaces.

---

## Resolved Questions

1. **GitHub repo listing** — Use GitHub API with a stored PAT (per team or user). The API call is `GET /user/repos` or `GET /orgs/{org}/repos`. Server-side proxy to avoid exposing the token to the frontend.

2. **DevPod provider** — Use whatever DevPod provider is configured on the server (Docker locally, Kubernetes, cloud). The session creation doesn't need to know the provider — DevPod handles that via its provider config.

3. **Workspace failure handling** — If `devpod up` fails, set workspaceStatus to "failed" and broadcast via WebSocket. The Files and Terminal tabs already show an error state with a "Retry" button. Retry would call `devpod up` again.

4. **Session → workspace lifecycle** — Creating a session starts a workspace. Archiving/deleting a session should stop/destroy the workspace (future concern, not in this brainstorm scope).

---

## Resolved (Defaults)

5. **GitHub token storage** — Environment variable (`GITHUB_TOKEN`) on the server for v1. Simple, single-team. Upgrade to per-team DB storage when multi-team auth is added.

6. **DevPod progress** — Start/done/failed only for v1. Show spinner, no streaming. Simpler to implement, can add progress streaming later.

7. **Workspace naming conflicts** — Append a short random suffix if the slug collides (e.g., `auth-module-x3k`). Check DevPod workspace list before creating.
