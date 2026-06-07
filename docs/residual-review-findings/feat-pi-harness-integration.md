# Residual code-review findings — Pi harness integration

Branch: `feat/pi-harness-integration`
Review: Tier 2 `ce-code-review` (12 personas), run `20260606-203815-pireview`.
Status: the P1 correctness findings and several P2s were **fixed** in commit
`5aa6b3f`. The items below are the residual actionable findings deferred for
follow-up (judgment calls, feature additions, test coverage, and moderate
refactors). Recorded here because this checkout has no reachable git remote or
issue tracker.

## Fixed in 5aa6b3f
- P1 — `task_completed` broadcast before promotion (AE4/KTD12 idle-with-queued window).
- P1 — `/stop` (Cancel) promoting the next task onto the process it then kills.
- P2 — KTD6a: failed/cancelled/timed-out task now tears down its live Pi process.
- P2 — `ResolveAwaitingInput` failure leaving the await ceiling armed.
- P2 — idle-reaper (35m) vs awaiting-input ceiling (30m) conflict.
- P2 — dead `Process.exitedCh`/`exitErr` fields removed.
- P2 — new env vars documented in CLAUDE.md.
- Added a cancel-with-queue regression test.

## Residual — backend

- P1 (frontend, see below) — covered in frontend section.
- P1 — `promoteLocked` runs the Pi `Ensure` readiness handshake (up to 15s) while
  holding the per-(session,agent) keyed mutex, blocking `handleExit` and other
  transitions for that key. `server/internal/agent/runtime.go` (promoteLocked).
  Fix: move `Ensure` + first prompt out of the keyed critical section.
- P2 — Reused Pi process has no per-task event fencing: a late event from task N
  on a reused process can be attributed to task N+1. `runtime.go` (translate /
  attachConsumer). Flagged by correctness, adversarial, maintainability. Fix:
  stamp a run-epoch per (process, task) and drop post-terminal events.
- P2 — No per-agent queue depth bound (the legacy `ErrQueueFull` protection was
  dropped). `runtime.go` (Enqueue/createQueued). Fix: bound queued count and
  surface the "queue is full" system message.
- P2 — Boot-recovery failure aborts via `panic()` in `Server.Router()` rather
  than a structured fatal exit. `server/internal/server/server.go`. Fix: return
  an error / use the `slog.Error`+`os.Exit(1)` pattern from main.go.
- P2 — `handleSteer` always posts the channel message before `RouteOrEnqueue`
  decides; an idle reply double-counts and a failed route leaves an orphan
  message. `server/internal/handler/websocket.go`.
- P2 — Snapshot `agentTaskResp` omits `position`, so queued tasks lose their
  `#N` on every snapshot refetch. `server/internal/handler/agent_run.go`. Fix:
  derive a queue rank, or have the client preserve prior position for still-
  queued tasks.
- P2 — KTD6a mid-run: `translate()` logs-and-returns on a store error for an
  action/awaiting event without failing the task or tearing down the process
  (only run-completion/promote errors reach finalize). `runtime.go`.
- P3 — `RunningTask`/`NextQueuedTask`/`CreateQueuedTask` load full per-agent task
  history per transition. `server/internal/agent/dbstore.go`. Fix: targeted
  `state`-filtered queries served by `idx_tasks_session_agent_state`.
- P2 (maintainability) — `InstallPi` is a near-verbatim copy of `InstallTools`.
  `server/internal/workspace/manager.go`. Fix: extract a shared installer.

## Residual — frontend

- P1 — `src/stores/agent-runs.ts` reducer (seq gap/dedupe/snapshot reconcile) is
  entirely untested and no frontend test runner exists. Fix: add vitest + cover
  applyEvent dedupe, the forward-gap boundary, applySnapshot reset, and
  reduceActions upsert.
- P1 — Snapshot refetch overwrites already-applied newer events (the U10
  pending-snapshot buffer was not implemented; current design self-heals via a
  second gap→resync but can flicker/lose an event in the window).
  `src/stores/session-store.ts` (fetchAgentRuns) + `agent-runs.ts`.
- P2 — First-event gap is undetectable (`needsResync` gated on `lastSeq>0`).
  `src/stores/agent-runs.ts`.

## Residual — parity / coverage / ops

- P2 (agent-native) — Steering a live run and answering the `ask_user` question
  are WebSocket-only; there is no REST equivalent (`POST /messages` always
  enqueues a new task, never resolves a pending question). Add a membership-gated
  `POST /sessions/{id}/agents/{agentId}/steer` calling `runtime.RouteOrEnqueue`.
- P2 (security, pre-existing) — `ANTHROPIC_API_KEY` is passed as a `--set-env`
  argv element to the host `devpod` process (readable via `/proc`/`ps` to local
  host users). Same pattern as the existing `executor.go`. Fix needs a DevPod
  env-by-reference mechanism or host isolation.
- Testing gaps — `RecoverStuckTasks` ordering/retry, snapshot endpoint 403,
  `dbstore` in-tx-seq integration, `devpod_launcher` signal/reap, and
  error-injectable `fakeStore`/`tLauncher` to reach the sad paths.
- Verification — confirm `signalGroup` (SIGKILL to the local pgid) reaches the
  in-container `pi` across the `devpod ssh` hop and leaves no orphan (R5),
  end-to-end. The KTD4 reaping + Setpgid patterns are correct in code (learnings
  reviewer confirmed) but the ssh-hop orphan case is unverified at runtime.
- Follow-up — promote the `cmd.Process.Wait()`+`Setpgid` pattern (now in sshproxy
  and pirun) to a shared `docs/solutions/` entry via `/ce-compound`.
