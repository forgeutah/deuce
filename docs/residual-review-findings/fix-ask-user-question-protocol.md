# Residual Review Findings — fix/ask-user-question-protocol

Source run: `ce-code-review` `20260806-230814-6e360099`, nine reviewers (correctness,
adversarial, reliability, testing, api-contract, maintainability, project-standards,
agent-native, learnings) against `origin/main...HEAD`.

Plan: `docs/plans/2026-08-06-001-fix-ask-user-question-protocol-plan.md`

Four findings were applied in `2a2a77e`. No tracker is configured for this repo, so the
items below are inlined verbatim rather than filed as tickets — this file is their durable
record.

## Residual Review Findings

### Demoted at the confidence gate

- **P2 — `server/internal/agent/runtime.go:731` — A ceiling timer that already fired still
  kills the just-answered task.** The awaiting-input `AfterFunc` is stopped on answer, but a
  timer already past its deadline and waiting on `r.mu` will still call `failTaskAsync` after
  the answer is delivered. Adversarial reviewer, anchor 50, routed `manual`. Suggested fix: add
  a generation counter to `taskTimers`, bumped under `r.mu` by `startActive`/`enterAwaiting`/
  `exitAwaiting`, captured by the closure and re-checked before failing. Not applied: single
  reviewer, below the actionable anchor, and the fix touches timer lifecycle beyond this
  change's scope.

### Concurrency and lifecycle (residual risks)

- **Concurrent blocking dialogs on one task overwrite each other.** `pendingReq` and the
  drawer are keyed by task id and replaced unconditionally. `npm:pi-subagents` is installed in
  every workspace; if it opens its own blocking dialog while `ask_user`'s is open, the first is
  orphaned and blocks its tool until the extension's deadline. Not confirmed that pi-subagents
  opens blocking dialogs — the package is not vendored here.
- **`translate`'s `KindAwaitingInput` branch runs without the per-key lock** and writes DB
  state before `setPending`. A reply in that window sees `state=awaiting_input` with
  `tracked=false` and is delivered as a steer behind the blocked tool. Ordering `setPending`
  first would remove it. Pre-existing shape; the window is one map write wide.
- **The same unlocked branch can resurrect timers and `pendingReq` entries** for a task
  `finalizeLocked` just tore down, leaking one entry each per occurrence. Pre-existing.
- **The 30-second gap between the extension's no-answer deadline and Pi's dialog timeout** is
  the only guard against Pi resolving a confirm to `false` without the no-answer flag set. Thin
  under event-loop starvation, and no test covers Pi's own timer firing — the test harness
  models the abort path but not `createDialogPromise`'s internal `setTimeout`.

### Observability

- **Ignored UI methods drop with no trace.** The five fire-and-forget methods now decode to
  `KindIgnore` and `DecodeStream` continues with no log, unlike the unknown-event branch which
  logs at debug. A subagent's `notify`/`setWidget` content is therefore unreachable anywhere in
  Deuce — correctly not a question, but silently unsurfaceable. Raised by the agent-native
  reviewer as an observation; worth an explicit product decision about whether that information
  should ever reach the user.
- **`decodeUIOptions` degrades silently** when `options` is neither array nor string, unlike
  the sibling paths this change made loud. No concrete trigger under Pi's real contract.
- **The new warn-level logs have no rate limiting.** If an installed extension throws
  repeatedly, every occurrence logs at warn. Worth a log-volume check during live verification.

### Testing gaps

- No test drives two blocking dialogs pending concurrently on one task.
- `ask-user.test.ts`'s Pi mock omits `createDialogPromise`'s own `setTimeout`, so the
  fabricated-answer race is unreachable from the suite.
- `decodeUIOptions`' third branch (options neither array nor string) has no direct test.
- No completeness check for Pi's three-arm *response* union, mirroring the request-union check.
  `pirun.UIResponseCancelled` has no caller and no coverage — it is staged ahead of the
  deferred "send cancelled on stop" work in the plan's Scope Boundaries.
- `leadingWord` matches ASCII letters only, so a non-English affirmative falls to the negative
  default. Undocumented.
- U5's overflow fix has no automated coverage; the repo has no visual-regression harness.

### Documentation drift

- `CLAUDE.md` describes `npm test` as covering "pure-logic suites: reducer, visibility". The new
  `ask-user.test.ts` is a Node-oriented suite under `server/`, picked up by Vitest's default
  glob. Accurate-but-stale description, not a rule violation.
- `ask-user.test.ts` runs under the root Vitest config's global jsdom environment rather than a
  Node environment. Harmless today; worth attention if the suite grows.

### Recommended follow-up learning

The `learnings-researcher` recommends capturing this bug's shape in `docs/solutions/`, since no
existing entry covers the testing discipline involved:

> A test fixture invented alongside the decoder it tests can only confirm that decoder's
> assumptions, never catch them — derive wire-protocol fixtures from the other side's own
> published contract (vendored types, docs, source), never from what the code expects to see.

It should cross-link `docs/solutions/architecture-patterns/pi-loads-agent-skills-standard-in-rpc-mode.md`,
which already records that Pi vendors its own docs and types inside the npm package — the
pointer that would have short-circuited the original guess.

### Deferred from the plan (not review findings)

Carried here so they stay visible alongside the residuals:

- Letting the main chat composer answer a pending question. An `@deuce` message sent while a
  question is pending is enqueued *behind* the blocked task, so it cannot run until the question
  resolves or the ceiling fires — the originally reported symptom, from the surface a user is
  most likely to reach first. The plan accepts this and proposes a session-notice mitigation.
- Sending `cancelled: true` when a run is stopped while a question is pending.
- Timeout tuning.

## Verification still outstanding

The plan's Verification Contract requires live verification against a real Pi, which a green
suite cannot substitute for: rebuild the workspace (not restart — the extension is baked into
the prebuild image), then confirm each of the three question styles end to end, that clicking
No is received as a negative, and that the server log holds no `skipping malformed event line`
warnings for the session.
