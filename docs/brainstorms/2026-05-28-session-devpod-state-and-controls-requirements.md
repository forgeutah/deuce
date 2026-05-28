---
date: 2026-05-28
topic: session-devpod-state-and-controls
---

# Session devpod state reconciliation and controls

## Summary

Reconcile each session's `workspace_status` against actual devpod state on a 10-second sweeper, expose Start / Stop / Rebuild / Delete controls per session, and prevent interaction with sessions whose container isn't running by fading the tabs and surfacing the appropriate recovery button.

## Problem Frame

`workspace_status` is written only at session creation. The create flow sets it to `starting`, the background goroutine in [server/internal/handler/sessions.go:464](server/internal/handler/sessions.go#L464) flips it to `ready` or `failed`, and nothing changes it after that. When devpod containers stop for any reason — host reboot, manual docker stop, container OOM, devpod cleanup — the DB still reports `ready`. The UI dots stay green and users click into sessions only to discover the terminal can't connect and file operations 502.

Today three sessions show green; two of them no longer have a running container. The system has no mechanism to notice. There is also no UI for stopping or rebuilding a session's devpod — the workspace lifecycle inside Deuce is effectively "create once, never touch again until the session is abandoned."

## Key Decisions

- **Reconcile via docker ps, not devpod status.** `devpod status <id>` shells out and takes 1–3 seconds per workspace. With N sessions, a sweeper paying that cost serially per cycle is unworkable. `docker ps` with a label filter is sub-100ms regardless of session count. The cost: this fast path is docker-provider-only — non-docker providers (k8s, AWS) are out of scope for v1 reconciliation.
- **Server-side reconciler, not on-demand resolution.** A single background goroutine sweeps all non-archived sessions every 10 seconds and writes truth into `workspace_status`. The DB remains the source of truth that the API and websocket fan-out read from. On-demand resolution per request was rejected as too slow for the session list.
- **Observe, don't auto-recover.** When the sweeper detects drift (DB says `ready`, container says no), it updates the status and broadcasts; it does not attempt to restart. Auto-restart was rejected because a session in a `failed` rebuild loop would spin forever, and silent recovery hides legitimate problems.
- **Disable interaction when the workspace isn't live.** Chat / Terminal / Files tabs fade and become non-clickable when `workspace_status ∉ {ready, starting}`. The recovery button (`Start` when stopped, `Rebuild` when missing) is the only interactive surface. Users cannot accidentally type a message into a session whose backend is dead.
- **Only Delete confirms.** Delete is irreversible — wiping the devpod takes minutes to recover from — so it opens a confirm modal naming consequences. Stop and Rebuild fire immediately on click. Stop is cheaply reversible (`Start` brings the container back in seconds). Rebuild is destructive of in-container work, but the friction cost of a modal on every rebuild outweighs the protection it offers given users already need rebuild to apply `devcontainer.json` changes during normal dev.
- **Single-process reconciler.** One sweeper per server process. Horizontally scaled deployments would double-poll without coordination — explicitly not a v1 target.

## Requirements

### Reconciliation

- R1. A background sweeper runs every 10 seconds for the lifetime of the server process.
- R2. The sweeper enumerates non-archived sessions from the DB, calls `docker ps --filter label=dev.containers.id=<uid>` once per cycle (single call, all containers), and cross-references each session's devpod uid against the result.
- R3. The sweeper updates `workspace_status` only when the resolved status differs from the stored value. No DB write when the state is unchanged.
- R4. Every status change broadcasts a `session_update` over the websocket hub to the session's subscribers.
- R5. The sweeper never flips `starting` to anything else — that state is owned by the create flow until the create goroutine completes. Same for transitional states (`stopping`, `rebuilding`, `deleting`) — the action handler owns the transition.
- R6. Sweeper errors (docker daemon unreachable, devpod workspace.json corruption) log at warn level and do not mutate `workspace_status`. A transient docker failure must not produce false `missing` flips.

### Workspace status state model

- R7. `workspace_status` accepts: `starting`, `ready`, `stopping`, `stopped`, `rebuilding`, `deleting`, `missing`, `failed`. Transitional states (`starting`, `stopping`, `rebuilding`, `deleting`) are explicit values — no separate `pending_action` column.
- R8. `stopped` = devpod metadata exists on disk, container is not running. `missing` = no docker container matches AND/OR no devpod workspace.json — either case requires Rebuild to recover.
- R9. The `suspended` value in the current enum is retired. Any existing row with `suspended` migrates to `stopped` on the next sweep cycle (the sweeper writes truth based on actual docker state).

### Controls

- R10. `POST /api/sessions/{id}/workspace/start` — invokes `devpod up <workspaceID>` in the background. Sets `workspace_status` to `starting` synchronously, broadcasts, returns 202. On completion, the existing `startWorkspace` finalization writes `ready` or `failed`.
- R11. `POST /api/sessions/{id}/workspace/stop` — invokes `devpod stop <workspaceID>` in the background. Sets `workspace_status` to a stopping transitional value synchronously, broadcasts, returns 202. On completion, writes `stopped`.
- R12. `POST /api/sessions/{id}/workspace/rebuild` — `devpod delete --force --ignore-not-found` then `devpod up`. Synchronous transition to a rebuilding state; success flips to `ready`, failure to `failed`. Streams devpod output as `workspace_log` websocket events the same way create does.
- R13. `POST /api/sessions/{id}/workspace/delete` — `devpod delete --force --ignore-not-found`. Wipes the devpod entirely. The session row, messages, and activity history are preserved; only `workspace_status` flips to `missing`.
- R14. All four endpoints require session membership. No per-role gating in v1.
- R15. Each endpoint rejects with 409 if the session is already in a transitional state (`starting`, `stopping`, `rebuilding`, `deleting`). Prevents concurrent operations stomping on each other.

### UI behavior

- R16. The status dot uses `workspace_status` directly: `ready` → green, `starting` / `stopping` / `rebuilding` / `deleting` → animated yellow, `failed` → red, `stopped` → neutral grey, `missing` → muted red.
- R17. When `workspace_status ∉ {ready, starting}`, the Chat / Terminal / Files tabs render in a faded state and are not clickable. The session detail area shows a recovery card with the workspace state, the relevant action button, and the latest stderr if the state is `failed`.
- R18. Recovery card buttons: `Start` when `stopped`. `Rebuild` when `missing` or `failed`. Both available when the user explicitly opens a session-level "Workspace" menu (so a healthy session can still be stopped or rebuilt manually).
- R19. Delete opens a confirmation modal before firing. The modal names what is wiped ("Devpod container and any in-container work will be permanently removed") and what is preserved ("Session, chat history, and messages are kept"). Stop and Rebuild fire immediately with no modal.
- R20. While a transitional action is in flight, the controls are disabled and a progress indicator replaces the button. Devpod log output streams into a log panel in the recovery card.

## Key Flows

- F1. Drift detection (silent)
  - **Trigger:** Sweeper runs every 10s.
  - **Steps:** Read non-archived sessions → single `docker ps` with label filter → for each session, resolve uid from `~/.devpod/contexts/default/workspaces/<id>/workspace.json` → match against docker output → derive status → write only if changed → broadcast `session_update`.
  - **Outcome:** UI dots reflect reality within 10s of any state change. No user-visible recovery action taken.
  - **Covered by:** R1, R2, R3, R4, R6

- F2. Stop a healthy workspace
  - **Trigger:** User clicks Stop in the workspace menu of a `ready` session.
  - **Steps:** `POST /workspace/stop` fires immediately → backend writes `stopping`, broadcasts, returns 202 → backend runs `devpod stop` → on success writes `stopped`, broadcasts. UI tabs fade as soon as the `stopping` state arrives.
  - **Outcome:** Container is halted, session row preserved, `Start` button appears in recovery card.
  - **Covered by:** R11, R15, R17, R18, R20

- F3. Rebuild a broken or stale workspace
  - **Trigger:** User clicks Rebuild on a `failed`, `missing`, or `ready` session.
  - **Steps:** `POST /workspace/rebuild` fires immediately → backend writes `rebuilding`, broadcasts, returns 202 → `devpod delete --force --ignore-not-found` → `devpod up` → log output streams as `workspace_log` WS events → on completion, `ready` or `failed` written and broadcast.
  - **Outcome:** Fresh container from current `devcontainer.json`. In-container work is lost. Chat history preserved.
  - **Covered by:** R12, R15, R17, R18, R20

- F4. Detect a missing container and recover
  - **Trigger:** Sweeper finds a session marked `ready` whose container is no longer in `docker ps`.
  - **Steps:** Sweeper writes `stopped` (devpod metadata still present) or `missing` (metadata gone) → broadcasts → user's UI fades tabs and shows recovery card → user clicks `Start` (if `stopped`) or `Rebuild` (if `missing`) → corresponding action flow runs.
  - **Outcome:** Drift surfaces in ≤10s; user has a one-click recovery path.
  - **Covered by:** R3, R4, R8, R17, R18

## Acceptance Examples

- AE1. Sweeper writes nothing when state matches
  - **Covers R3.**
  - **Given:** A session with `workspace_status='ready'` whose docker container is running.
  - **When:** The sweeper cycle runs.
  - **Then:** No row is updated, no websocket event is broadcast, no log line is emitted.

- AE2. Sweeper does not flip transitional states
  - **Covers R5.**
  - **Given:** A session in `workspace_status='starting'` because a create is in flight. The container does not yet exist in `docker ps`.
  - **When:** The sweeper cycle runs.
  - **Then:** Status remains `starting`. The sweeper does not write `missing`.

- AE3. Concurrent action rejected
  - **Covers R15.**
  - **Given:** A session in `workspace_status='rebuilding'`.
  - **When:** A second user clicks Stop.
  - **Then:** `POST /workspace/stop` returns 409 with an error message naming the in-flight operation. No devpod command runs.

- AE4. Delete preserves chat
  - **Covers R13.**
  - **Given:** A session with 200 messages and a running container.
  - **When:** User clicks Delete, confirms, and the action completes.
  - **Then:** The session row exists, all messages are readable, `workspace_status='missing'`, the Rebuild button is shown, and the devpod workspace directory is gone.

- AE5. Tabs disable on stop
  - **Covers R17.**
  - **Given:** A session is `ready` and the user has the Terminal tab open.
  - **When:** Another user stops the workspace.
  - **Then:** Within 10s the first user's UI receives a `session_update` with `workspace_status='stopped'`. The Terminal tab fades, becomes non-clickable, and the recovery card with a `Start` button replaces the terminal panel.

## Scope Boundaries

- Non-docker devpod providers (k8s, AWS SSH) — sweeper fast path uses `docker ps` and won't cover them. Deferred until a user with non-docker provider exists.
- Auto-restart on drift — explicitly out. Reconciler observes; user decides.
- Idle-session auto-stop — no quota system, no "stop after N hours of inactivity." A user-facing setting may be revisited later.
- Per-role authorization for controls — every session member can Start / Stop / Rebuild / Delete. A future "owner-only Rebuild" gate is out of scope.
- Multi-process / horizontally scaled reconciler — single goroutine per server process; running two servers would double-poll. Not a v1 target.
- Preserving in-container work across Rebuild — Rebuild explicitly wipes. No volume snapshotting.
- "Delete session" (archive) — separate from "Delete workspace" and unchanged by this brainstorm.

## Dependencies / Assumptions

- DevPod docker provider is the only provider in production use today.
- `~/.devpod/contexts/default/workspaces/<id>/workspace.json` continues to expose the `uid` that maps to the `dev.containers.id` docker label, as already relied on by [server/internal/workspace/manager.go:208](server/internal/workspace/manager.go#L208).
- The server runs as a user with read access to its own `~/.devpod/contexts/default/workspaces/` tree and docker exec rights — same prerequisite as the existing SSH proxy and terminal features.
- `session.Name` continues to be the devpod workspace ID. If session rename ever ships, this assumption needs to be revisited everywhere it appears, including this reconciler.
- The websocket hub can be broadcast to from a background goroutine without a request-scoped context — `BroadcastToSession` already supports this from the create-flow goroutine.

## Outstanding Questions

### Resolve before planning

- **`failed` state granularity.** Today a single `failed` value covers create failure, rebuild failure, and start failure. Should the recovery card know which kind of failure to surface differently, or is one generic `failed` + the last log line enough?

### Deferred to planning

- Exact sweeper interval — 10s is a defensible default; planning can tune based on docker daemon load profile.
- Log retention for `workspace_log` events — how long do we keep the rebuild output history, and where (in-memory ring buffer, activity table)?
- Frontend animation timing for the fade-out — design polish.
