---
title: "feat: Real AI Agent Execution Inside DevContainers"
status: active
origin: docs/brainstorms/2026-05-08-real-agents-in-devcontainers-brainstorm.md
created: 2026-05-08
depth: standard
---

# feat: Real AI Agent Execution Inside DevContainers

## Problem Frame

Deuce's agents are currently simulated — hardcoded canned responses with random delays. Users @mention agents and get fake output. To deliver on the promise of a shared AI-assisted workspace, agents need to actually execute inside the DevPod devcontainer with full access to the dev environment (shell, files, git, tests, builds, linters) and report real results back to the chat.

Additionally, agents are hardcoded seed data with no way to create, edit, or delete them. Users need a settings page to manage their agent pool.

---

## Scope Boundaries

### In Scope
- Replace agent simulation with real Claude Code headless execution inside devcontainers
- Install Claude Code into devcontainers at workspace startup (post-creation step)
- Stream and parse Claude Code output, surface as chat messages with expandable details
- Agent session continuity via `--continue`/`--resume` + chat history context
- Sequential agent execution with queue (one agent at a time per session)
- Cancellation via stop button and chat command
- Error handling with error messages in chat
- Full CRUD agent management settings page (global scope)
- Database migration to support custom agents with system prompts

### Deferred to Follow-Up Work
- Concurrent agent execution (multiple agents working simultaneously via git branches)
- Autonomous agent behavior (observing commits, proactive reviews)
- Plugin configuration UI (which Claude Code plugins to install per agent)
- Per-user or per-project agent scoping
- Per-user API key management

---

## Key Technical Decisions

1. **Claude Code headless via SSH** — Run `claude -p` inside the devcontainer via `devpod ssh`. The Go backend SSHs into the container and invokes Claude Code non-interactively. This preserves plugin access and avoids reimplementing the agentic loop. (see origin: brainstorm "Why This Approach")

2. **Streaming JSON output** — Use `--output-format stream-json --verbose` to get structured events from Claude Code. Parse `stream_event` objects to extract text deltas, tool calls, file edits, and shell output. This feeds the chat summary + expandable details UX.

3. **Reuse workspace streaming pattern** — The existing `startWorkspace` goroutine pattern (detached context, `LogFunc` callback, WebSocket broadcast, DB status update) is the blueprint for agent execution. Agent runs are long-running goroutines that stream output via WebSocket.

4. **Session continuity via Claude Code sessions** — Store the Claude Code session ID per agent per session. Use `--resume <sessionID>` on subsequent mentions so the agent remembers prior context. Additionally, inject recent chat history as context in the system prompt.

5. **Sequential execution with channel-based queue** — Use a Go channel per session to serialize agent execution requests. When an agent is already working, new mentions queue up. The queue is designed so a concurrent model (goroutine-per-agent with git branch isolation) can replace it later. Queue has a buffer of 10, idle worker timeout of 10 minutes, and a `Shutdown()` method wired into server lifecycle.

6. **Server-level API key** — `ANTHROPIC_API_KEY` env var on the Deuce server, passed per-command to `devpod ssh` invocations (never persisted to the container filesystem). Simplest and most secure starting point.

8. **Soft-delete agents** — Agents use a `deleted_at` column instead of hard delete. This preserves historical message author resolution (name, color) for messages from agents that were later removed. Soft-deleted agents are hidden from the UI and not selectable for sessions.

9. **SSH command execution via `ExecInWorkspace`** — The existing `SSHCommand()` returns an interactive SSH `*exec.Cmd` (used by the terminal). Agent execution needs a new `ExecInWorkspace(ctx, workspaceID, command string) *exec.Cmd` method that uses `devpod ssh --command "..."` for non-interactive command execution. This keeps stdin free for piping the user's prompt.

7. **Agent CRUD replaces seed data** — The agents table gets a `system_prompt` column. Default agents are created on first run (or via migration), but are fully editable/deletable. The `role` field becomes free-text rather than an implicit enum.

---

## Implementation Units

### U1. Database Migration: Agent System Prompt and CRUD Support

**Goal:** Extend the agents table to support custom agents with system prompts and add the necessary SQL queries for full CRUD.

**Requirements:** Agent settings page needs create/update/delete. Agents need system prompts for Claude Code execution.

**Dependencies:** None

**Files:**
- `server/internal/db/migrations/NNN_agent_system_prompt.sql` (create — assign sequence number at implementation time)
- `server/internal/db/queries/agents.sql` (modify)
- Run `make generate` to regenerate Go code

**Approach:**
- Add `system_prompt TEXT NOT NULL DEFAULT ''` column to `agents` table
- Add `deleted_at TIMESTAMPTZ` column for soft-delete (nullable, NULL = active)
- Add `created_at TIMESTAMPTZ` and `updated_at TIMESTAMPTZ` columns for tracking
- Update seed data migration approach: keep existing agents as defaults, but make them mutable
- Add sqlc queries: `CreateAgent`, `UpdateAgent`, `SoftDeleteAgent` (set `deleted_at`), `ListActiveAgents` (WHERE `deleted_at IS NULL`)
- Update existing `ListAgents` to filter out soft-deleted agents by default
- Soft-deleted agents remain in DB for historical message resolution (author name, color)

**Patterns to follow:** Existing migration files in `server/internal/db/migrations/` use goose `-- +goose Up` / `-- +goose Down` directives. Queries use sqlc comment syntax.

**Test scenarios:**
- Create an agent with all fields populated and verify it persists
- Update an agent's system prompt and verify the change
- Soft-delete an agent — `deleted_at` is set, agent excluded from `ListActiveAgents`
- Soft-deleted agent still resolvable by ID (for historical message rendering)
- Create an agent with empty system prompt (should use default empty string)

**Verification:** `make migrate` succeeds. `make generate` produces updated Go code. Queries compile without errors.

---

### U2. Agent CRUD API Endpoints

**Goal:** Add REST endpoints for creating, updating, and deleting agents.

**Requirements:** Frontend settings page needs API endpoints to manage agents.

**Dependencies:** U1

**Files:**
- `server/internal/handler/agents.go` (create — currently agents are handled inline in other files)
- `server/internal/server/server.go` (modify — add routes)
- `src/lib/api.ts` (modify — add API wrappers)

**Approach:**
- `POST /api/agents` — create agent. Request body: `{name, systemPrompt, model, provider}`. Auto-assign color from a rotating palette of the existing agent colors.
- `PUT /api/agents/{id}` — update agent. Same body fields, all optional.
- `DELETE /api/agents/{id}` — soft-delete agent. Returns 204. If agent is currently executing in any session, return 409 Conflict with message indicating the agent is busy.
- Move existing `ListAgents` and `GetAgent` handler logic into the new `agents.go` file for cohesion.
- Use `writeError()` for error responses following existing convention.
- Add typed API wrapper functions in `src/lib/api.ts`.

**Patterns to follow:** Existing handler methods in `server/internal/handler/sessions.go` — parse path params with `chi.URLParam`, decode JSON body, call sqlc query, marshal response. Error responses use `writeError()`.

**Test scenarios:**
- Create agent with valid fields returns 201 with the created agent
- Create agent with missing required field (name) returns 400
- Update agent's name and system prompt returns 200 with updated agent
- Soft-delete existing agent returns 204
- Delete non-existent agent returns 404
- Delete agent currently executing returns 409 Conflict
- List agents returns all agents including newly created ones
- Get single agent by ID returns correct agent

**Verification:** All CRUD operations work via curl/httpie. API wrappers compile in TypeScript.

---

### U3. Agent Settings Page (Frontend)

**Goal:** Build a global settings page where users can view, create, edit, and delete agents.

**Requirements:** Users need to manage their agent pool — the 5 defaults are editable, and custom agents can be added.

**Dependencies:** U2

**Files:**
- `src/components/settings/AgentSettingsPage.tsx` (create)
- `src/components/settings/AgentForm.tsx` (create)
- `src/App.tsx` or router config (modify — add settings route)
- `src/types/index.ts` (modify — add `systemPrompt` to Agent type, change `AgentRole` from union to `string`)

**Approach:**
- New route: `/settings/agents` (or similar — check existing routing pattern)
- List view shows all agents as cards/rows: name, model, color indicator, truncated system prompt
- "Add Agent" button opens a form (dialog or inline)
- Edit button on each agent opens the same form pre-filled
- Delete button with confirmation dialog
- Form fields: name (text input), system prompt (textarea, generous height), model (select dropdown with known Claude models)
- Color auto-assigned on create, shown as a visual indicator but not editable in v1
- Use existing shadcn/ui components: Dialog, Button, Input, Textarea, Select
- Follow dark mode design system with Primer color tokens

**Patterns to follow:** Existing dialog patterns in `src/components/session/CreateSessionDialog.tsx` — form state management, API calls, error handling. Dark mode styling conventions in `src/styles/globals.css`.

**Test scenarios:**
- Settings page loads and displays all existing agents
- Create new agent via form — agent appears in list after save
- Edit existing agent — changes reflect in list
- Delete agent — confirmation dialog appears, agent removed from list after confirm
- Form validation: name is required, system prompt can be empty
- Navigation to/from settings page works correctly

**Verification:** Settings page renders all agents. CRUD operations reflect immediately in the UI. Form validation works.

---

### U4. Workspace Claude Code Installation

**Goal:** After DevPod creates a workspace, automatically install Claude Code inside the devcontainer and configure it with the API key.

**Requirements:** Agents need Claude Code available inside the container to execute.

**Dependencies:** None (can be built in parallel with U1-U3)

**Files:**
- `server/internal/workspace/manager.go` (modify — add `InstallTools` and `ExecInWorkspace` methods)
- `server/internal/handler/sessions.go` (modify — call install after workspace creation)
- `server/internal/config/config.go` (modify — add `AnthropicAPIKey` config)

**Approach:**
- Add `ExecInWorkspace(ctx, workspaceID, command string) *exec.Cmd` to workspace manager — uses `devpod ssh --command "..."` for non-interactive command execution (distinct from interactive `SSHCommand` used by terminals)
- Add `InstallTools(ctx, workspaceID string, logFn LogFunc)` to workspace manager
- Implementation: use `ExecInWorkspace` to run install commands:
  1. Check if Claude Code is already installed: `claude --version`
  2. If not: `npm install -g @anthropic-ai/claude-code` (pin to a specific version)
- **API key is NOT written to the container filesystem.** It is passed per-invocation as an environment variable on the `exec.Cmd` object when the agent executor runs Claude Code. This avoids exposing the key to terminal users.
- Call `InstallTools` from `startWorkspace` after `Create` succeeds but before setting status to "ready"
- Stream install output via the existing `workspace_log` WebSocket event so users see progress
- Add `ANTHROPIC_API_KEY` to config struct with `env:"ANTHROPIC_API_KEY"`
- Handle the case where npm/node isn't available in the container — log a warning but don't fail the workspace

**Patterns to follow:** The existing `Create` method in `workspace/manager.go` — uses `exec.CommandContext`, `StdoutPipe`, `bufio.Scanner` for streaming. The `startWorkspace` goroutine in `sessions.go` for the overall flow.

**Test scenarios:**
- Workspace creation with valid API key installs Claude Code and sets env var
- Install progress streams via `workspace_log` events
- Workspace still reaches "ready" status after successful install
- Missing `ANTHROPIC_API_KEY` env var logs a warning but doesn't prevent workspace creation
- Container without npm/node logs a warning about Claude Code unavailability
- Idempotent: re-running install on a container that already has Claude Code doesn't error

**Verification:** After workspace creation, SSH into the container and run `claude --version` to confirm installation. `ANTHROPIC_API_KEY` is set in the environment.

---

### U5. Agent Executor: Claude Code Headless via SSH

**Goal:** Build the core agent execution engine that SSHs into the devcontainer, runs Claude Code headless, parses streaming output, and produces structured results.

**Requirements:** This is the heart of the feature — replacing canned responses with real LLM-powered execution.

**Dependencies:** U4

**Files:**
- `server/internal/agent/executor.go` (create)
- `server/internal/agent/output.go` (create — streaming JSON parser)

**Approach:**
- `Executor` struct holds workspace manager reference and config
- `Execute(ctx context.Context, params ExecuteParams) (*ExecuteResult, error)` — main method
- `ExecuteParams`: workspaceID, agentName, systemPrompt, userMessage, chatHistory, claudeSessionID (for `--resume`), model
- Build the `claude -p` command with flags:
  - `--output-format stream-json --verbose`
  - `--allowedTools "Bash,Read,Edit,Write"`
  - `--append-system-prompt "<system prompt>"`
  - `--resume <sessionID>` if continuing a session
  - Pipe the user message + chat history context via stdin
- Use `workspace.ExecInWorkspace()` to run `claude -p ...` inside the container. Set `ANTHROPIC_API_KEY` on the `exec.Cmd.Env` — the key is passed per-invocation, never persisted
- Handle `--resume` failure gracefully: if Claude Code returns a session-not-found error (e.g., after workspace restart clears `~/.claude/`), retry without `--resume` (fresh session) and update the stored session ID
- When using `--resume`, minimize chat history injection (only messages since the agent's last response) to avoid redundant context. When starting fresh, inject the full N messages
- Parse the streaming JSON output line by line:
  - `stream_event` with `text_delta` → accumulate as summary text
  - `stream_event` with tool use → collect as expandable content (file edits as diffs, bash commands as terminal output)
  - Final `result` message → extract `session_id` for continuity
- `ExecuteResult`: summary text, expandableContent array, claudeSessionID, error info
- Support cancellation via context cancellation — kill the SSH process
- Set a timeout (configurable, default 5 minutes) on the context

**Patterns to follow:** The `LogFunc` + `bufio.Scanner` pattern from `workspace/manager.go` for streaming. The terminal manager's `SSHCommand` usage pattern from `handler/terminal.go`.

**Test scenarios:**
- Execute with a simple prompt returns summary text and session ID
- Execute with a prompt that triggers file edits returns expandable content with diffs
- Execute with a prompt that triggers shell commands returns terminal output in expandable content
- Context cancellation kills the SSH process and returns a cancellation error
- Timeout triggers after configured duration and returns timeout error
- Resume with existing session ID maintains conversation continuity
- Resume with stale session ID (workspace restarted) falls back to fresh session gracefully
- Invalid workspace (not ready) returns an appropriate error
- Claude Code crash (non-zero exit) returns error with stderr content

**Verification:** Executor can run a simple prompt in a running devcontainer and return structured results. Cancellation kills the process within 1 second.

---

### U6. Wire Executor into Message Handler

**Goal:** Replace the `processAgentMentions` simulation with real agent execution. Add sequential queue and session continuity.

**Requirements:** @mentions trigger real agent execution. One agent at a time per session. Agents remember prior context.

**Dependencies:** U5

**Files:**
- `server/internal/handler/messages.go` (modify — replace `processAgentMentions`)
- `server/internal/handler/handler.go` (modify — add executor and queue to Handler struct)
- `server/internal/agent/queue.go` (create — per-session execution queue)
- `server/internal/server/server.go` (modify — initialize executor, inject into handler, add startup recovery)
- `server/internal/db/queries/sessions.sql` (modify — add query to store/retrieve Claude session IDs, add reset-stale-agents query)
- `server/internal/db/migrations/NNN_agent_session_ids.sql` (create — assign sequence number at implementation time)

**Approach:**
- **Queue lifecycle**: `agent.Queue` struct with a map of session ID → buffered channel (buffer size 10). `Enqueue(sessionID, task)` pushes to the channel. If buffer is full, return an error — post "Agent queue full, please wait" to chat. A worker goroutine per session pulls from the channel and executes sequentially. Workers are lazy (start on first enqueue) and exit after 10 minutes idle (re-spin on next enqueue). `Shutdown(ctx)` method cancels all active executions and drains all workers — wired into server shutdown alongside `terminal.Manager.CloseAll()`.
- **Queue feedback**: When a task is queued behind another, post a system message in chat: "Agent is currently busy. Your request has been queued." Include queue position in the `agent_status` WebSocket event.
- **Pre-execution workspace check**: Before executing, query `sessions.workspace_status`. If `starting`, post "Workspace is still starting — your request will run when ready" and re-queue with backoff. If `failed` or `suspended`, post error and do not queue.
- **Startup recovery**: On server startup, reset all `session_agents.status = 'working'` to `'idle'` and broadcast status updates. This handles stale state from server restarts.
- **Session continuity**: Add a `claude_session_id` column to `session_agents` table. After each successful execution, store the returned session ID. On next mention, pass it via `--resume`.
- **Chat history context**: Before executing, fetch the last N messages (e.g., 20) from the session. Format them as context in the system prompt. When using `--resume`, only include messages since the agent's last response to avoid redundant context.
- **Replace `processAgentMentions`**:
  1. For each mentioned agent, create an execution task
  2. Enqueue the task
  3. The queue worker handles the lifecycle: set status → typing → execute → create message → stop typing → reset status
  4. On error: set status to "error", post error message in chat with distinct styling, reset to "idle" after 10 seconds
  5. **Fix existing bug**: the current code passes `nil` context to `UpdateSessionAgentStatus` (messages.go line 254). Use `context.Background()` consistently.
- **Cancellation storage**: Track the cancel function per active execution. Cancel on `/stop` chat command or stop button API call. Cancelling one agent allows the next queued agent to proceed.
- **Session closure**: When a session is archived or paused, cancel all running and queued agent tasks for that session.
- Add `POST /api/sessions/{id}/agents/stop` endpoint for the stop button

**Patterns to follow:** The existing `processAgentMentions` lifecycle (status → typing → work → message → reset). The `context.Background()` pattern for goroutines. Note: the existing `nil` context on line 254 is a bug to fix, not a pattern to follow.

**Test scenarios:**
- @mention a single agent triggers real execution and posts response in chat
- @mention two agents in one message — second agent queues and executes after first completes
- Queued agent mention posts "Your request has been queued" system message
- Queue overflow (>10 pending): agent mention returns "queue full" error
- Agent response includes expandable content (diffs, terminal output) rendered correctly
- Session continuity: second @mention to same agent can reference prior work
- Chat history context: agent receives recent conversation in its prompt
- Error during execution: agent posts error message, status shows "error" for 10s then resets to idle
- Cancel via stop endpoint: running agent is cancelled, posts "cancelled" message, next queued agent proceeds
- Workspace status "starting": agent mention posts "workspace starting" message and re-queues
- Workspace status "failed" or "suspended": agent mention posts error immediately
- Agent status transitions broadcast correctly via WebSocket (working → idle, or working → error → idle)
- Server restart: all agents reset from "working" to "idle" on startup
- Session archived while agent working: agent execution cancelled

**Verification:** @mention an agent in a session with a running devcontainer. The agent executes real work, posts a summary with expandable details, and status indicators update in real time.

---

### U7. Streaming Agent Output to Frontend

**Goal:** Add a new WebSocket event type for agent output streaming so users see real-time progress while an agent works.

**Requirements:** Users should see intermediate output (not just the final message) while an agent is working.

**Dependencies:** U6

**Files:**
- `server/internal/ws/events.go` (modify — add `agent_output` event type)
- `server/internal/agent/executor.go` (modify — add streaming callback)
- `server/internal/handler/messages.go` (modify — pass streaming callback to executor)
- `src/hooks/use-websocket.ts` (modify — handle `agent_output` events)
- `src/stores/session-store.ts` (modify — accumulate agent output per session)
- `src/components/chat/ChatView.tsx` (modify — render streaming agent output)

**Approach:**
- New event type `TypeAgentOutput = "agent_output"` with payload: `{agentId, sessionId, content, contentType}` where contentType is `"text"` | `"tool_use"` | `"tool_result"`
- The executor calls a `StreamFunc` callback (similar to `LogFunc`) as it parses streaming events
- The message handler passes a `StreamFunc` that broadcasts `agent_output` via WebSocket
- Frontend accumulates output in a `agentOutput: Record<string, AgentOutputLine[]>` state
- While agent is working, show a collapsible "Agent working..." section below the typing indicator with streaming output
- When agent completes, replace the streaming section with the final message (with expandable content)
- Clear accumulated output when agent finishes
- **Note**: Streaming is best-effort — the WebSocket hub may drop messages if a client's send buffer is full. The final `new_message` event (with complete summary + expandable content) is the source of truth, not the streaming output.

**Patterns to follow:** The `workspace_log` streaming pattern — line-by-line WebSocket broadcast, frontend accumulation in Zustand store, rendering in a scrollable container.

**Test scenarios:**
- Agent output streams to frontend in real-time while agent is working
- Text deltas appear as the agent "thinks"
- Tool use events show what the agent is doing (e.g., "Reading file: src/auth.ts")
- Streaming output clears when agent posts final message
- Multiple clients in the same session all see streaming output
- Streaming output for queued agents only shows for the currently active agent

**Verification:** While an agent works, the chat shows real-time streaming output. Output transitions smoothly to the final message when complete.

---

### U8. Cancellation UI

**Goal:** Add a stop button in the UI and support `/stop` chat command to cancel a running agent.

**Requirements:** Users need a way to stop an agent mid-execution.

**Dependencies:** U6, U7

**Files:**
- `src/components/chat/ChatView.tsx` (modify — add stop button near typing indicator)
- `src/lib/api.ts` (modify — add stop endpoint wrapper)
- `server/internal/handler/messages.go` (modify — handle `/stop` or `@Agent stop` in message content)

**Approach:**
- **Stop button**: Render a "Stop" button (square icon, red accent) next to the typing indicator when an agent is working. Calls `POST /api/sessions/{id}/agents/stop`.
- **Chat command**: When a message contains `/stop` or `@AgentName stop`, the message handler triggers cancellation instead of queuing a new execution.
- Both paths call the cancel function stored in the queue, which cancels the context and kills the SSH process.
- After cancellation, the agent posts a brief "Cancelled by user" message in chat and resets to idle.

**Patterns to follow:** The existing typing indicator rendering in `ChatView.tsx`. Button styling from shadcn/ui.

**Test scenarios:**
- Stop button appears when an agent is working, disappears when idle
- Clicking stop button cancels the running agent within 2 seconds
- `/stop` in chat cancels the running agent
- `@Coder stop` cancels the Coder specifically (if multiple are queued)
- After cancellation, agent posts "Cancelled" message and status resets to idle
- Stop button does not appear when no agent is working

**Verification:** Start an agent, click stop — agent cancels promptly and posts a cancellation message.

---

## System-Wide Impact

- **WebSocket**: New event type `agent_output` added. Existing events (`agent_status`, `typing_indicator`, `new_message`) unchanged but now carry real data.
- **Workspace startup**: Takes longer due to Claude Code installation step. Users see install progress in workspace logs.
- **Environment variables**: New required `ANTHROPIC_API_KEY` on the Deuce server. Without it, agents will fail gracefully with an error message.
- **Database**: Two new migrations (agent system prompt, Claude session IDs). Non-destructive — adds columns and a table.
- **Performance**: Agent execution is I/O-bound (waiting on Claude Code). The sequential queue ensures at most one SSH connection per session for agent work. No CPU pressure on the Go backend.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Claude Code not installable in some containers (no npm/node) | Agent features unavailable for that session | Detect and warn during workspace setup. Post clear error if agent is mentioned without Claude Code. |
| Claude Code streaming JSON format changes | Output parser breaks | Pin to a known Claude Code version during install. Parser should handle unknown event types gracefully. |
| Long-running agent executions | Users waiting, resource consumption | Default 5-minute timeout. Cancellation support. Status indicators show progress. |
| API key exposure in container environment | Security concern | Pass API key per-invocation via `exec.Cmd.Env`. Never persist to container filesystem. Don't log the key. |
| Server restart during agent execution | Orphaned Claude Code process in container, stale "working" status | Startup recovery resets stale statuses. Orphaned Claude Code processes bounded by 5-minute timeout. |
| Deleting agent with historical messages | Broken message rendering (missing author name/color) | Soft-delete agents — `deleted_at` column preserves author info for historical messages. |

---

## Deferred Implementation Notes

- Exact Claude Code install command may need adjustment based on container base image (Alpine vs Debian vs etc.)
- The streaming JSON parser will need refinement as we discover the full range of event types Claude Code emits
- Chat history formatting for agent context may need tuning (how many messages, how to summarize long conversations)
- Color auto-assignment palette and rotation logic — implementation detail for U2
