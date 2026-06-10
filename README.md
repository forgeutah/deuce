# Deuce

**A shared workspace for AI-assisted development. Agents as teammates, not tools.**

Deuce is an open source channel-style workspace where humans (engineer, designer, PM) and AI agents collaborate on the same product — across scope, plan, design, build, and test — instead of each person prompting agents in private CLI sessions and meeting up later at the PR.

> Status: **very early, pre-alpha.** Built in the open. Feedback and contributors welcome.

---

## Why Deuce

Implementation is no longer the bottleneck — alignment is. Today, AI coding agents privatize the planning phase: one person scopes and prompts in a solo session, and the rest of the team only sees the work after the diff lands. That's too late to shape scope, design, or intent, and small cross-functional teams end up building the wrong thing fast.

Deuce's bet: bring the agents *into the team* and the team into one shared room. See [STRATEGY.md](STRATEGY.md) for the full strategy.

Heavily inspired by Maggie Appleton's [Zero Alignment](https://maggieappleton.com/zero-alignment) and GitHub's ACE research prototype.

---

## How it works

Each **session** is like a Slack channel for one piece of work:

- Humans and **deuce** — the built-in agent — post messages in the same thread; `@deuce` brings it into the work.
- Every session is backed by an **isolated [DevPod](https://devpod.sh) workspace** — a real dev container with the repo checked out, the shell available, and the build/test tooling installed.
- The agent runs *inside* that container (a persistent [Pi](https://pi.dev) process), so what it proposes is what would actually run.
- The session carries the whole arc: a plan tab, a chat, a file browser, a live terminal, and workspace logs — visible to everyone in the room.

---

## Quickstart

### Requirements

- Node 20+, npm
- Go 1.25+
- Docker (for Postgres and DevPod)
- [DevPod CLI](https://devpod.sh/docs/getting-started/install)
- A GitHub personal access token (for repo discovery)
- An Anthropic API key (for real agent execution)

### Open in a Dev Container (recommended)

If you have Docker Desktop and VS Code with the **Dev Containers** extension, the fastest way in is:

1. Open the repo in VS Code.
2. Run **Dev Containers: Reopen in Container**.

The container ships Go, Node, the DevPod CLI, and the Go tools (`goose`, `sqlc`, `air`) pre-installed and pinned, copies `server/.env.example` to `server/.env` if it doesn't exist, runs migrations on every start, and exposes ports 4000, 8080, and 5432. Set `ANTHROPIC_API_KEY` and `GITHUB_TOKEN` on the host shell before launching VS Code and they'll forward into the container.

Then, in two terminals inside the container:

```bash
cd server && make dev    # backend on :8080
npm run dev              # frontend on :4000
```

> The devcontainer mounts the host Docker socket so the in-container DevPod CLI can spawn workspace containers on the host. GitHub Codespaces blocks this mount, so Codespaces support is best-effort — use the manual flow below in Codespaces.

### Run it manually

```bash
# 1. Start Postgres
docker compose up -d

# 2. Run migrations
cd server && make migrate && cd ..

# 3. Configure the backend
cp server/.env.example server/.env  # fill in GITHUB_TOKEN and ANTHROPIC_API_KEY
                                     # (the devcontainer does this for you)

# 4. Start the Go backend (hot reload via air)
cd server && make dev

# 5. In another terminal, start the frontend
npm install
npm run dev
```

Open <http://localhost:4000>.

See [CLAUDE.md](CLAUDE.md) for the full developer guide (commands, conventions, adding endpoints/migrations).

---

## Architecture

**Monorepo** — React frontend at the repo root, Go backend in `server/`.

| Layer | Stack |
| --- | --- |
| Frontend | React 19, Vite, TypeScript, Tailwind v4, shadcn/ui (Radix), Zustand, xterm.js |
| Backend | Go, chi v5, pgx/v5, sqlc, coder/websocket |
| Database | Postgres 17 (migrations via goose) |
| Workspaces | DevPod (Docker provider by default) |
| Agent | Pi (`pi --mode rpc`), one persistent process per session inside the dev container via `devpod ssh` |

Real-time updates use a single WebSocket hub with per-session subscriptions. The frontend keeps state in a single Zustand store and lazy-loads messages/activities per session.

---

## Roadmap

Organized by the three tracks in [STRATEGY.md](STRATEGY.md). Anything unchecked is fair game for contributors — open an issue or PR and let's talk.

### Foundation (cross-cutting plumbing)

- [x] Go REST API + Postgres + sqlc-generated queries
- [x] WebSocket hub with per-session pub/sub
- [x] React + Vite frontend with shadcn/ui design system
- [x] Single Zustand store with WS-driven updates
- [x] DevPod workspace lifecycle (create, status, delete) wired to sessions
- [x] GitHub repo + org discovery for session creation
- [x] `.env` config loading on the server
- [ ] Real authentication (currently a single mock user via `DEUCE_USER_ID`)
- [ ] Multi-tenant teams + invites
- [ ] Hosted demo / one-command Docker compose for end users (not just dev)

### Track 1 — Agent Team

The "how agents think and act as teammates" layer.

- [x] One built-in agent (**deuce**) in every session — @mention to invoke, serial task queue with cancel, live task/action cards
- [x] Real agent execution inside the DevPod container (persistent Pi process per session)
- [x] Interactive agent questions (`ask_user` → typed prompts in the thread drawer)
- [x] Global system-prompt editing for deuce
- [ ] Skills: modulate deuce's behavior per task (`/skill:` expansion) instead of role agents
- [ ] Subagents as the parallelism tier (bounded workers reporting back as deuce)
- [ ] Agent session continuity across server restarts
- [ ] Autonomous agent behaviors (proactive review on commits, etc.)

### Track 2 — Chat & Presence

The channel itself — where humans and agents are visibly co-present.

- [x] Session sidebar, create-session dialog, session switching
- [x] Real-time chat with @mentions
- [x] Typing indicators / thinking-agent indicator
- [x] Activity feed (right rail)
- [x] Unread counts + mark-as-read
- [x] Per-session plan tab (markdown plan content)
- [ ] Live human presence (avatars, cursors, "who's looking at this")
- [ ] Threaded replies / reactions
- [ ] Slash commands (`/plan`, `/handoff`, `/stop`, etc.)
- [ ] Notifications (in-app + email/webhook for offline mentions)
- [ ] Search across sessions and history
- [ ] Session archive / pin / favorite

### Track 3 — Coding & Preview

File browsing, collaborative editing, previews, PR review — **every surface here must be agent-callable**.

- [x] File browser tab in-session (read-only)
- [x] Workspace logs panel (streaming `devpod up` output)
- [x] Shared terminal tab attached to the DevPod container via PTY + WebSocket
- [ ] Collaborative VS Code (or web-based IDE) attached to the container
- [ ] Live UI preview of the running app (with a public URL the team can click)
- [ ] Screenshot + annotation surface (for design feedback)
- [ ] Figma / design-asset import into the session
- [ ] GitHub PR integration (open, review, comment from inside the session)
- [ ] Agent-driven test runs visible in the workspace
- [ ] Diff viewer with inline chat on hunks
- [ ] Agent-callable APIs for **every** human action above (the agent-native parity rule)

---

## Contributing

This is an open source project for the masses. Help shape it.

- **Pick something unchecked above** and open an issue to claim it before you start.
- **Read [CLAUDE.md](CLAUDE.md)** for the dev guide — commands, conventions, how to add endpoints/migrations.
- **Read [STRATEGY.md](STRATEGY.md)** so your contribution lands inside the bet, not next to it.
- **Plans and brainstorms** for in-flight work live in [`docs/plans/`](docs/plans/) and [`docs/brainstorms/`](docs/brainstorms/).
- Small, focused PRs are easier to land. If you're not sure, open a draft and we'll talk.

---

## License

TBD. Open source, intended permissive.
