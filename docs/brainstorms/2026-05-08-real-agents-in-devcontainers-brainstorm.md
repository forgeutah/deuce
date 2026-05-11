# Brainstorm: Real Agents Working Inside DevContainers

**Date:** 2026-05-08
**Status:** Draft

## What We're Building

Replace the simulated agent system (canned responses with random delays) with real AI agents that execute inside the DevPod devcontainer. When a user @mentions an agent in a session, the agent runs Claude Code headless inside the container with full access to the dev environment — shell, files, git, tests, builds, linters — and reports back to the chat.

### Core User Story

A developer creates a session linked to a repo. DevPod spins up the devcontainer. The developer @mentions `@Coder` with "implement the login endpoint from the spec." The Coder agent activates inside the devcontainer, reads the codebase, writes code, runs tests, and posts a summary with expandable diffs and shell output back into the session chat.

## Why This Approach

### Claude Code Headless Inside the Container

**Decision:** Install Claude Code into the devcontainer at workspace startup (post-creation step), then invoke it via SSH with `claude -p`.

**Why not API-from-backend with SSH tool execution?**
- Loses access to Claude Code plugins (e.g., Compound Engineering skills, review agents, custom workflows)
- Plugins are a core part of the value proposition — they turn a generic LLM into a specialized dev tool
- Would require reimplementing Claude Code's tool definitions, file handling, and agentic loop in Go

**Why not require Claude Code in the devcontainer config?**
- Deuce should work with any repo — can't require users to modify their devcontainer
- Deuce already owns the workspace creation process (`devpod up`), so injecting Claude Code after creation is clean and invisible to the user

### @Mention-Triggered Only (for now)

**Decision:** Agents respond only when explicitly @mentioned in chat.

**Why:** Keeps the user in control. Autonomous agent behavior (observing commits, proactively reviewing) is a natural extension but adds complexity around when agents should/shouldn't act. Start with the simple, predictable model.

### Sequential Execution (for now)

**Decision:** One agent works at a time per session. Queue additional requests.

**Why:** Multiple agents editing the same codebase simultaneously creates merge conflicts, race conditions, and confusing state. Sequential execution is predictable and simple. The interface should be designed so concurrency (via git branches or working directories) can be added later without breaking changes.

## Key Decisions

1. **Agent runtime:** Claude Code headless (`claude -p`) installed inside the devcontainer at workspace startup
2. **Plugin support:** Full — Claude Code plugins (Compound Engineering, etc.) are available because the full CLI runs in the container
3. **Trigger model:** @mention only. User explicitly directs agents.
4. **Output format:** Summary message in chat + expandable details (diffs, shell output, intermediate steps) using the existing `expandableContent` pattern
5. **Concurrency:** Sequential (one agent at a time per session), designed for future concurrent extension
6. **API key:** Server-level `ANTHROPIC_API_KEY` env var injected into the container. Per-user keys can be added later.
7. **Devcontainer modification:** None to the user's config. Deuce injects Claude Code post-creation via SSH.

## How It Works (High-Level Flow)

```
1. User creates session with repo URL
2. Deuce runs `devpod up` to create workspace
3. Post-creation: Deuce SSHs into container, installs Claude Code + configured plugins
4. User @mentions an agent in chat (e.g., "@Coder fix the auth bug")
5. Go backend receives message with agent mention
6. Backend SSHs into container, runs:
   claude -p "<system prompt for role>\n\n<user message>" \
     --output-format stream-json \
     --allowedTools "Bash,Read,Edit,Write" \
     --append-system-prompt "<role-specific instructions>"
7. Backend streams output, parses events:
   - Updates agent status to "working", broadcasts via WebSocket
   - Shows typing indicator
   - Collects tool calls, file edits, shell output as expandable details
   - Captures final response as the summary message
8. Backend creates agent message with summary + expandableContent
9. Broadcasts new_message via WebSocket
10. Resets agent status to "idle"
```

## What Changes in the Codebase

### Backend (server/)
- **New: `internal/agent/executor.go`** — Manages Claude Code headless execution via SSH. Handles streaming output parsing, timeout/cancellation.
- **New: `internal/agent/roles.go`** — System prompts per agent role (coder, reviewer, planner, tester, designer)
- **Modified: `internal/handler/messages.go`** — Replace `processAgentMentions` simulation with real agent execution. Queue mechanism for sequential processing.
- **Modified: `internal/handler/sessions.go`** — Post-creation step to install Claude Code in the workspace after `devpod up` completes.
- **Modified: `internal/workspace/manager.go`** — Add `InstallClaudeCode(workspaceID)` method that SSHs in and runs npm install.
- **New env var:** `ANTHROPIC_API_KEY` — injected into the container for Claude Code auth.

### Frontend (src/)
- **Modified: `components/chat/ChatView.tsx`** — Enhanced expandable content rendering for richer agent output (multiple diffs, shell transcripts, etc.)
- **Modified: `types/index.ts`** — Possibly extend `ExpandableContent` type for new content kinds (shell-transcript, multi-file-diff)
- **Modified: `stores/session-store.ts`** — May need to handle streaming agent messages (partial updates via WebSocket)

### Database
- Potentially add an `agent_tasks` or `agent_runs` table to track execution history, but could start without this.

## Resolved Questions

1. **Session continuity:** Yes — use Claude Code's `--continue`/`--resume` to maintain per-session agent memory, AND pass recent chat history as context. Agents remember what they did and can see what humans and other agents said.
2. **Plugin configuration:** Start simple — install base Claude Code only. Plugin configuration is a follow-up feature.
3. **Cancellation:** Both a stop button in the UI (next to typing indicator) and a chat command (`@Agent stop` or `/stop`). Both kill the underlying SSH/claude process. Agent posts a "cancelled" status message.

## Agent Settings Page

### Overview
Full CRUD for agents via a global settings page. Replace the hardcoded seed agents with user-manageable agents. The 5 original roles (coder, reviewer, planner, tester, designer) ship as defaults but can be edited or deleted.

### Agent Fields
- **Name** — Display name (e.g., "Coder", "My Custom Linter")
- **System prompt** — Instructions for what the agent does, its personality, constraints
- **Model** — Which Claude model to use (e.g., claude-sonnet-4-6, claude-opus-4-6)
- **Color** — Auto-assigned from a preset palette (can be overridden later)

### Scope
- **Global (instance-level)** for v1 — all projects and sessions see the same agent pool
- Per-project and per-user scoping as a follow-up

### Settings Page UX
- Accessible from a top-level settings/admin area
- List view of all agents with name, model, and status
- Create/edit form with the fields above (system prompt gets a textarea with enough room)
- Delete with confirmation
- Default agents marked as "built-in" but still editable/deletable

### Database Impact
- The existing `agents` table already has `name`, `role`, `provider`, `model`, `description`, `color`, `color_muted`
- Add: `system_prompt` text column
- The `role` column becomes user-defined (free text) rather than an enum
- Remove seed data from migration, replace with a first-run setup or keep as defaults that can be modified

## Open Questions

1. **Error handling:** What happens if Claude Code crashes, times out, or hits rate limits inside the container? How is this surfaced in chat?
