# Brainstorm: Deuce — Open Source Shared Agent Sessions

**Date:** 2026-05-08
**Status:** Draft
**Participants:** Clint Berry, Claude

---

## What We're Building

**Deuce** is an open source shared workspace for AI-assisted development. It's inspired by GitHub's ACE research prototype but built as a community-driven alternative.

The core concept: **sessions are like Slack channels where multiple team members and N agents with specific roles collaborate in real-time**, backed by isolated cloud dev environments.

### The Problem (from Maggie Appleton's "Zero Alignment")

Coding agents today are isolated, single-player experiences, but software development is a team sport. Key pain points:

- **Speed collapse** — implementation happens in minutes, eliminating natural team alignment checkpoints
- **Misplaced bottlenecks** — review happens after work is done, when course-correction is expensive
- **Lost context** — agent planning is unshared, so teams can't validate approaches before execution
- **Tool mismatch** — Slack, GitHub, Jira weren't designed for agentic workflows

Deuce addresses this by making agent sessions inherently multiplayer and team-visible.

### Target Users

Cross-functional teams: engineers, PMs, designers, and stakeholders. The interface should be accessible to non-technical participants while providing full dev capabilities for engineers.

---

## Why This Approach

### Architecture: Session-Centric Monolith

A single Go server handles session management, WebSocket connections, DevPod orchestration, and agent routing. React frontend delivered via a Tauri desktop app.

**Why monolith for v1:**
- Fastest path to a working MVP
- Simple deployment: one binary + DevPod
- Easier for open source contributors to onboard
- Can refactor to services later once boundaries are battle-tested

**Rejected alternatives:**
- Microservice split — too much complexity before we know the right boundaries
- Plugin architecture — premature abstraction risk; design with plugin-friendly boundaries but don't build the plugin system yet

### Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Backend | Go | Performance, concurrency model fits WebSockets + agent orchestration |
| Frontend | React (Tauri desktop app) | Rich ecosystem, Tauri gives native feel with web tech |
| Real-time | WebSockets | Built into Go's stdlib, battle-tested for chat |
| Dev Environments | DevPod | Open source, provider-agnostic devcontainer orchestration |
| Database | Postgres | Sessions, users, agent configs, message history |

### Session = DevPod Workspace

Each session maps 1:1 to a DevPod workspace (devcontainer). This provides:
- **Complete isolation** — each session has its own filesystem, git state, and running processes
- **Terminal access** — users get a terminal tab connected directly to the session's workspace
- **Agent sandboxing** — agents execute within the devcontainer, not on the host
- **Reproducibility** — devcontainer.json defines the environment declaratively

---

## Key Decisions

1. **Chat-first UI with terminal tab** — Primary interface is a message thread (like Slack). Agents respond inline with rich embeds (code diffs, previews). A separate tab provides direct terminal access to the DevPod workspace.

2. **Hybrid agent model** — Ship with preset functional roles (Coder, Reviewer, Planner, Tester) but let users bring their own LLM API keys and define custom agent roles. Roles are defined by persona (system prompt) + tool access (MCP servers/capabilities).

3. **Session = DevPod workspace** — One session, one devcontainer. Isolation is at the workspace level. Git branches, terminal access, and file changes are all scoped to the session.

4. **Desktop app + self-hosted server** — Tauri desktop app for the client experience. Go server is self-hosted by teams (Docker Compose or similar). Server manages DevPod lifecycle, WebSocket hub, and agent orchestration.

5. **Cross-functional accessibility** — The chat interface should be usable by non-engineers. Technical capabilities (terminal, git) are available but not required to participate.

6. **Open source from day one** — MIT or Apache 2.0 license. Community-driven development.

7. **Human-in-the-loop with sub-agents** — Agents respond to humans only, but can spawn sub-agents for scoped tasks (tests, linting, etc.). Sub-agent work doesn't flood the chat.

8. **Suspend-on-idle workspaces** — DevPod workspaces auto-suspend after configurable idle timeout. Resumes on next interaction.

9. **Port forwarding for previews** — No embedded browser preview in v1. DevPod port forwarding lets users view running apps in their own browser.

10. **OAuth for auth** — GitHub and Google OAuth for v1. Simple, standard, no custom auth flows.

---

## Core Concepts

### Sessions
- Persistent chat rooms tied to a DevPod workspace
- Multiple human members + multiple agents
- Full message history with context for agents
- States: active, paused, archived

### Agent Roles
- **Preset roles:** Coder, Reviewer, Planner, Tester, Designer (ship with sensible defaults)
- **Custom roles:** Users define arbitrary roles with:
  - Custom system prompts (persona)
  - Tool/MCP server access (capabilities)
  - LLM provider + model selection (BYO keys)
- Agents respond when mentioned or when their expertise is relevant
- Each agent has its own context window into the session conversation

### Workspaces
- Backed by DevPod (devcontainer spec)
- Created when a session starts, destroyed or suspended when archived
- Provide terminal access, file system, git, and execution environment
- Can be configured per-project via devcontainer.json

### Teams
- Groups of users who share access to sessions
- Team-level agent role configuration
- API key management at team level

---

## Resolved Questions

1. **Agent-to-agent communication** — Human-in-the-loop for top-level agent interactions. Agents only respond to human messages or @mentions. However, agents *can* spin up sub-agents for tasks (e.g., a Coder agent spawns a sub-agent to run tests). Sub-agent work is scoped and reported back to the parent agent, not broadcast to the chat.

2. **Session persistence model** — Auto-suspend DevPod workspaces after idle timeout (configurable, e.g., 30 minutes). Resume on next interaction. Balances cost and user experience.

3. **File/preview sharing** — v1 uses port forwarding only. DevPod forwards container ports to the user's machine; users open previews in their own browser. Embedded live preview is a future enhancement.

4. **Auth & identity** — OAuth via GitHub and Google for v1. Standard, familiar, low friction for open source users.

5. **Message format** — Deferred to a dedicated UX brainstorm session. The chat UI, message rendering, and overall user experience deserve their own deep exploration.

---

## Open Questions

1. **UX design** — The full user experience (message rendering, session navigation, agent interaction patterns, non-engineer workflows) needs its own brainstorm session.

2. **Licensing** — MIT vs Apache 2.0. Need to decide before first public commit.

3. **DevPod provider defaults** — Which DevPod providers to support/document first? Docker (local), Kubernetes, cloud providers (AWS, GCP)?
