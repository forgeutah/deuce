---
date: 2026-06-03
topic: pi-harness-integration
---

# Pi Harness Integration for Super Threads

## Summary

Replace the `claude -p` one-shot harness with [Pi](https://pi.dev/) running in `--mode rpc` inside each session's DevPod container, driven by Go over a persistent JSONL stdin/stdout channel. Pi's lifecycle and tool events map onto the Super Threads `AgentRunEvent` stream so the UI gains a live per-action log, a per-agent serial queue, and steerable agent sessions where agents can pause to ask and users can redirect mid-run.

## Problem Frame

Deuce already runs real agents — `server/internal/agent/executor.go` shells `claude -p --output-format stream-json` into each DevPod container via `devpod ssh --command`. Two things about that shape break the experience the Super Threads design is built around.

First, `claude -p` is **fire-and-forget**. It feels batchy — the user @mentions an agent and then waits, with no honest sense of progress, until a single final message lands. The handler does forward coarse `agent_output` events, but the model is request-in / response-out, not a live stream the UI renders as work happens.

Second, `claude -p` has **no back-channel**. When the agent needs a decision, or the user wants to redirect it, there is nowhere for that exchange to go — the run is a closed transaction. The Super Threads design papers over this by treating every drawer reply as a brand-new task, but that is a workaround for a harness that cannot hold a conversation.

Pi's RPC mode is built around a persistent, steerable session (`prompt` / `steer` / `follow_up`, steering modes) with a fully-specified incremental event stream. That is precisely the missing back-channel and the missing real-time signal. It is also model-agnostic and extensible, which opens later doors (per-role plugins, non-Claude models) without re-architecting the harness.

The Super Threads design README explicitly defers the backend: *"Deuce does not have an agent-run event stream yet."* This brainstorm decides the harness that produces that stream and the interaction model it enables.

## Key Decisions

- **Pi replaces `claude -p` as the agent harness.** The existing `agent.Executor` one-shot path is retired in favor of a Pi-driven runtime. Session-continuity that today rides on Claude's `--resume <claudeSessionID>` moves to Pi's own session model.

- **Topology A: Go drives Pi over RPC, Pi runs in-container.** Pi is launched in `--mode rpc` through the existing `devpod ssh --command` mechanism, so Pi's read/write/edit/bash tools operate natively against the repo in the container — no `docker exec` tool-routing layer. Go owns the process: it writes JSONL commands to stdin and reads JSONL events from stdout.

- **"Use the SDK" resolves to Pi's RPC interface, not the typed TS SDK.** Pi's typed SDK is TypeScript-only and the backend is Go; adding a Node runtime was rejected for v1. Go hand-writes structs for Pi's documented event schema. This is the deliberate trade for keeping the stack single-language and tools in-container.

- **Live, steerable interaction model.** A run is a persistent session, not a transaction. The task lifecycle gains an `awaiting input` state: the agent can pause to ask, and the user can interject to steer at any time, both feeding the same live run.

- **The drawer composer feeds the live run; it does not enqueue.** This reverses the design README, where a drawer reply always enqueues a new task. While a run is active, a reply is delivered to that run via Pi `steer`/`follow_up`. The reply still posts to the channel for shared visibility but spawns **no** new task card. A drawer reply when the agent is idle (no active run) enqueues a new task as before.

- **Anyone in the session can steer a live agent, in arrival order.** Consistent with Deuce's shared-workspace model, steering is not locked to the original requester. Concurrent steers from multiple members are serialized in arrival order and shown in the shared thread.

- **The queue is per-agent, not per-session.** Today `agent.Queue` keys by `sessionID`, serializing all agents in a session. The design requires one running task *per agent*, with multiple agents (Coder, Reviewer) able to run concurrently. The scheduler re-keys to `(sessionID, agentID)`.

- **Idle-awaiting-input is excluded from the run timeout.** The current 5-minute execution timeout cannot apply while an agent is blocked on a human. Active-work time and waiting-on-human time are accounted separately.

## Actors

- A1. Requester — the session member whose `@mention` created a task.
- A2. Other session members — present in the same session; can watch every thread and steer any live agent (A1 has no special privilege over a running task).
- A3. Pi agent — one persistent Pi session per agent role per Deuce session; emits the run/tool/lifecycle events.
- A4. Scheduler / runtime (Go) — owns the per-agent queue, the Pi subprocess lifecycle, and the translation of Pi events into `AgentRunEvent`s broadcast over WebSocket.
- A5. DevPod container — the workspace where Pi runs and where its file/bash tools take effect.

## Requirements

### Harness and process lifecycle

- R1. Pi runs inside the session's DevPod container in `--mode rpc`, launched over the existing `devpod ssh --command` path; its file and bash tools act on the in-container repo with no separate tool-routing layer.
- R2. The runtime maintains one persistent Pi RPC process per active agent and drives it via JSONL on stdin/stdout (`prompt`, `steer`, `follow_up`, plus steering-mode control).
- R3. Pi is provisioned into the container on workspace setup, mirroring today's Claude install path; absence of Pi is surfaced to the session rather than failing silently.
- R4. Session continuity uses Pi's native session model in place of Claude's `--resume`; a steer/follow-up continues the same Pi session.
- R5. Process exit, crash, or container restart is detected; the runtime cleans up the process, marks any in-flight task `failed`, and does not leak orphaned subprocesses.

### Event stream and contract

- R6. The runtime translates Pi events into the design's `AgentRunEvent` contract — `task.enqueued`, `task.started`, `action.started`, `action.completed`, `task.completed` — each carrying `sessionId`, `taskId`, `agentId`, and a monotonic `seq`.
- R7. The interaction model adds events for the steerable lifecycle: an agent question that pauses the run (`task.awaiting_input` with the question text) and its resolution when a steer/reply resumes the run.
- R8. Pi tool calls map to the action log's tool vocabulary (Read, Grep, Edit, Write, Bash, Think); `action.started` drives the live tool line + spinner and `action.completed` carries the diff/output payload.
- R9. A reconnect/late-join snapshot returns each agent's task list including actions already taken for any in-flight task, the current lifecycle state (including `awaiting input` and any pending question), and the latest `seq`; clients apply only events with `seq >` snapshot.
- R10. Events are append-only and ordered per session; gap detection on reconnect relies on `seq`.

### Queue and scheduling

- R11. The scheduler enforces one running task per `(sessionId, agentId)`; distinct agents in the same session run concurrently.
- R12. A new `@mention` of a busy agent enqueues a task in `queued` state; queue position `#N` is derived by walking the agent's task list.
- R13. On task completion the scheduler atomically marks the finished task `done` and promotes the next queued task to `running` (emitting its `task.started`) in one state transition — the UI never shows an agent simultaneously idle and holding a queued item.
- R14. The server scheduler is the source of truth for queue order and promotion; the client may render an optimistic `queued` card on send, reconciled by the server event.

### Interaction model

- R15. While a run is active, a reply from the thread drawer is delivered to that run as a steer/follow-up; it posts to the channel for shared visibility but creates no new task card.
- R16. When the agent pauses with a question, the task enters `awaiting input`; the card and thread show the pending question, and the next reply from any session member resumes that same run.
- R17. A session member may interject to steer a `running` task without the agent having asked; the interjection feeds the live run.
- R18. Steers and replies from multiple members targeting the same live run are serialized in arrival order and rendered in the shared thread.
- R19. A reply to an agent with no active run (idle) enqueues a new task via the normal enqueue/promote path.

### Lifecycle states and failure

- R20. The task lifecycle is `queued → running ⇄ awaiting input → done`, with `failed` and `cancelled` as terminal states reachable from `running` or `awaiting input`.
- R21. `/stop` (and the stop control) cancels the running task for the targeted agent, marks it `cancelled`, and promotes the next queued task.
- R22. The active-work timeout does not run while a task is `awaiting input`; only time spent actively executing counts toward it.
- R23. Tasks and threads persist and are shared across all session members in real time; they survive reload via the snapshot in R9.

## Key Flows

- F1. Mention to live run
  - **Trigger:** A1 sends a channel message that `@mentions` an idle agent.
  - **Actors:** A1, A4, A3
  - **Steps:** Message posts → scheduler enqueues a task (`task.enqueued`) → promotes immediately since the agent is idle (`task.started`) → Pi runs, streaming `action.started`/`action.completed` → run ends (`task.completed`).
  - **Outcome:** An anchored card under the message shows live tool actions, then shrinks to a done chip with the reply.
  - **Covered by:** R6, R8, R11, R12, R13

- F2. Agent asks, user answers, run continues
  - **Trigger:** Mid-run, the Pi agent emits a question.
  - **Actors:** A3, any of A1/A2, A4
  - **Steps:** Run enters `awaiting input` (`task.awaiting_input` with the question) → card flips to a "needs you" state → a session member replies in the drawer → reply is delivered to the same Pi session as a steer → run resumes and continues streaming actions → `task.completed`.
  - **Outcome:** A single continuous task/thread containing the question, the human answer, and the resumed work — not two separate tasks.
  - **Covered by:** R7, R15, R16, R22

- F3. Concurrent mention while busy
  - **Trigger:** A second `@mention` of the same agent arrives while it is `running` or `awaiting input`.
  - **Actors:** A2, A4
  - **Steps:** Scheduler enqueues the new task (`queued · #N`) → on completion of the active task, atomic done + promote of the queued task.
  - **Outcome:** Serial per-agent execution with visible queue position; other agents are unaffected.
  - **Covered by:** R11, R12, R13

- F4. Reconnect / late join
  - **Trigger:** A member opens or reloads the session mid-run.
  - **Actors:** A2, A4
  - **Steps:** Client reads the snapshot (task lists, in-flight actions, current state incl. pending question, latest `seq`) → renders it → applies subsequent events with `seq >` snapshot.
  - **Outcome:** The newcomer sees current truth, including an agent that is mid-action or waiting on input.
  - **Covered by:** R9, R10, R23

- F5. Interject to steer
  - **Trigger:** A member wants to redirect a `running` agent that has not asked anything.
  - **Actors:** A2, A3, A4
  - **Steps:** Member sends a drawer reply → delivered to the live run as a steer → posted to channel, no new card → agent incorporates it.
  - **Outcome:** Mid-flight course correction without ending or duplicating the task.
  - **Covered by:** R15, R17, R18

- F6. Failure and cancel
  - **Trigger:** Pi process exits abnormally, the active-work timeout trips, or a user issues `/stop`.
  - **Actors:** A4, A1/A2
  - **Steps:** Runtime marks the task `failed` or `cancelled` (`task.completed` with the terminal status) → cleans up the subprocess → promotes the next queued task.
  - **Outcome:** No orphaned processes; the queue keeps moving; the failure is visible in the thread.
  - **Covered by:** R5, R20, R21

## Acceptance Examples

- AE1. Drawer reply during a live run
  - **Covers R15, R17.**
  - **Given** an agent's task is `running`, **when** a member sends a reply in that agent's drawer, **then** the reply is delivered to the live Pi session and posted to the channel, and **no** new task card is created.

- AE2. Drawer reply to an idle agent
  - **Covers R19.**
  - **Given** an agent has no active run, **when** a member sends a reply in that agent's drawer, **then** a new `queued` task is created and promoted via the normal path.

- AE3. Question pauses the run
  - **Covers R7, R16, R22.**
  - **Given** a `running` task, **when** the agent emits a question, **then** the task enters `awaiting input`, the pending question is shown, the active-work timeout is suspended, and the next reply from any member resumes the same task.

- AE4. Atomic done-and-promote
  - **Covers R13.**
  - **Given** an agent with one `running` task and one `queued` task, **when** the running task finishes, **then** in a single transition the first becomes `done` and the second becomes `running` — there is no observable state where the agent is idle while a queued item exists.

- AE5. Concurrent agents
  - **Covers R11.**
  - **Given** Coder and Reviewer are both `@mentioned`, **then** both run at the same time; a second `@mention` of Coder queues behind Coder only and does not block Reviewer.

- AE6. Snapshot includes pending question
  - **Covers R9.**
  - **Given** an agent is `awaiting input`, **when** a member joins late, **then** the snapshot conveys the `awaiting input` state and the pending question, and live events resume from the snapshot `seq`.

## Scope Boundaries

### Deferred for later

- Per-role Pi extensions / a Compound-Engineering-style plugin capability. Pi's extensibility makes this possible, but the Compound Engineering Plugin is a Claude Code plugin (SKILL.md skills via Claude Code's Skill tool), not a Pi extension — it is not a drop-in port. Topology A also does not make the TS extension ecosystem first-class, so this is explicitly post-v1.
- Arbitrary multi-provider, per-agent model selection. v1 continues running Claude models *through* Pi; Pi's model-agnostic upside is a later unlock, not a v1 deliverable.
- Reordering or cancelling individual queued tasks, and "run next" queue controls (named in the design as designed-for-but-unbuilt).

### Outside this brainstorm

- The Super Threads visual layer (cards, drawer, action log styling). That is specified in `tmp/design_handoff_super_threads/` and is consumed by, not decided by, this harness work.

## Dependencies / Assumptions

- Pi can be installed and launched in `--mode rpc` inside the project's devcontainers over `devpod ssh --command`, the same way `claude` is today. **Assumption to verify in planning:** Pi's RPC process tolerates a long-lived stdin/stdout session over the DevPod SSH transport without buffering or idle-disconnect issues.
- Pi's documented RPC event schema is stable enough to hand-model in Go, and exposes (or can be made to expose, e.g. via steering modes) a question/await signal distinct from run completion. **Assumption to verify:** that a mid-run agent question is observable as a discrete event rather than only as terminal output.
- The existing WebSocket hub (`server/internal/ws/`) is the transport for the new `AgentRunEvent`s; no new transport is introduced.
- Container access (`devpod ssh` / `docker exec`) and credentials currently used by the executor remain available to the new runtime.

## Outstanding Questions

### Resolve before planning

- None blocking. The harness, topology, interaction model, queue model, and steering policy are decided above.

### Deferred to planning

- Exact Go process-supervision shape for long-lived Pi RPC processes (per-agent goroutine + lifecycle, restart/backoff policy, idle reaping vs the current 10-minute worker timeout).
- Where Pi session identifiers are persisted (the schema currently stores `claudeSessionID` per `(session, agent)`); whether that column is repurposed or replaced.
- Concrete `AgentRunEvent` wire shapes and the new awaiting-input event names in `server/internal/ws/events.go`.
- Whether the active-work timeout is enforced by Go wall-clock around RPC turns or derived from Pi's own turn boundaries.

## Sources

- `tmp/design_handoff_super_threads/README.md` — Super Threads interaction model, state machine, and the not-yet-built `AgentRunEvent` backend contract this work fulfills.
- `server/internal/agent/executor.go`, `output.go`, `queue.go` — the current `claude -p` harness, stream-json parser, and per-session queue being replaced.
- `server/internal/handler/messages.go` — mention parsing, enqueue/execute path, and WebSocket broadcast of agent status/output.
- `server/internal/workspace/manager.go` — DevPod `ssh --command` exec pattern and in-container tool install (`InstallTools`) that Pi provisioning mirrors.
- Pi RPC mode and SDK docs (`https://pi.dev/docs/latest/rpc`, `https://pi.dev/docs/latest/sdk`) — event schema, steering modes, and the cross-language integration path chosen for Approach A.
