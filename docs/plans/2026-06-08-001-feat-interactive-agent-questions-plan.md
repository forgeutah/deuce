---
title: "feat: Interactive typed agent questions with no-JSON guarantee"
type: feat
status: completed
date: 2026-06-08
origin: docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md
---

# feat: Interactive typed agent questions with no-JSON guarantee

## Summary

Render agent questions as interactive typed prompts in the agent thread — a
text field for open questions, selectable buttons when the agent offers
choices, a confirm control for yes/no, each with a free-text "Other" fallback —
and guarantee a question is never shown as raw JSON. Scoped entirely to the Pi
harness; the legacy `claude` executor is being removed and gets no work here.

---

## Problem Frame

Today an agent question can reach the user as a literal tool-call string in the
thread, e.g. `Ask_user({"question":"What file would you like me to create?"})`.
That is the structured question path *failing*: the intended pipeline is
`extension_ui_request` → `KindAwaitingInput` → `task.pendingQuestion`, which the
Super Thread drawer/card already consume. The JSON appears when that structured
event never fires — the Pi `ask_user` extension install silently failed
(`InstallPiExtension` returns `nil` on error), or the model narrated the call as
assistant prose (which streams into the reply buffer and posts as a chat
message).

Two quality gaps stack: (1) even on the happy path the question renders as plain
text with no input affordance and no way for the agent to offer choices
(`AgentTaskCard.tsx` / `AgentThreadDrawer.tsx` print `task.pendingQuestion`
verbatim), and (2) on the failure path the user sees raw JSON, which reads as a
broken product. See origin: `docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md`.

---

## Requirements Traceability

Carried from the origin requirements doc (R1–R12, A1–A2, F1–F2, AE1–AE5):

- **Typed prompts (R1–R5, R6–R8):** interactive prompt per kind (free-text /
  pick-one / confirm) with "Other" fallback; `ask_user` extended additively to
  carry `kind` + `options`; kind/options survive end to end without flattening.
- **No-JSON guarantee (R9–R12):** never display raw JSON; harden the silent
  install failure (R10); backstop narrated tool-call text into a clean question
  (R11) with a readable-prose floor when unparseable (R12).
- **Actors:** A1 agent (asks), A2 human (answers in place).
- **Flows:** F1 structured typed question; F2 leaked question → backstop.

**Origin note — AE3 reframed.** The origin's AE3 described a *legacy-harness*
leak. Since the `claude` harness is being removed, AE3 is covered here as a
**Pi-path narration leak** (the model writes the call into its assistant text
instead of invoking the tool). The "Outside this product's identity" origin
boundary about legacy-harness typed prompts is moot — that harness is going
away, not being supported at a lower tier.

---

## Key Technical Decisions

- **Plumb `kind` + `options` through the existing `pendingQuestion` channel, not
  a new event.** `task_awaiting_input` is part of the append-only, seq-ordered
  AgentRunEvent family (KTD6) and the decoder already extracts `RequestKind`
  (`decoder.go` `Event.RequestKind`). The gap is that `ws.TaskEventPayload` drops
  it. Add `pendingQuestionKind` + `pendingQuestionOptions` fields mirroring
  `pendingQuestion` exactly, additive and backward-compatible (a question with no
  kind behaves as free-text, R7).

- **Answers keep flowing through the existing steer → `ExtensionUIResponse`
  path.** Clicking a choice or confirm sends the chosen value as the steer
  message; `RouteOrEnqueue` already routes it to Pi as
  `ExtensionUIResponse{id, response}` keyed by the tracked request id (KTD15),
  under the per-`(session,agent)` lock (KTD9), clearing the awaiting-input
  ceiling (KTD8). No new answer transport. `ExtensionUIResponse.Response` is
  already `any`, so a string value needs no protocol change.

- **The backstop lives in the Pi reply-finalize path, not a shared post path.**
  With the legacy harness removed, the only remaining leak source is Pi-path
  assistant-text narration, which accumulates via `appendReply` and is taken at
  `takeReply` before `replyPoster` fires in `finalizeLocked`. Intercepting there
  sanitizes a tool-call-shaped reply into a clean question before it ever posts.
  Floor is clean readable text (the user answers via the normal composer), not a
  synthesized interactive prompt — a narrated question means the agent is not
  actually blocked on a structured request.

- **Install-failure hardening is loud, not self-healing.** `InstallPiExtension`
  escalates a failure to error-level logging and surfaces a session-visible
  notice that the agent cannot ask questions, rather than retrying or disabling
  the agent. Preserve the deliberate base64-over-the-wire encoding.

- **Rich choices depend on verifying the live Pi `ctx.ui` API.** The
  `@earendil-works/pi-coding-agent` types are not vendored locally, so whether
  `ctx.ui.select` / `ctx.ui.confirm` exist is unverified. The extension verifies
  at implementation time and falls back to `ctx.ui.input` with options rendered
  into the prompt text when the richer primitives are absent — still no JSON,
  still answerable. Keep the `ctx.hasUI === false` headless branch intact.

---

## High-Level Technical Design

Two layers, one shared prompt surface. The happy path (F1) carries structured
kind/options end to end; the backstop (F2) sanitizes a narrated leak before it
reaches chat.

```mermaid
flowchart TD
  subgraph Pi["Pi container"]
    EXT["ask_user extension<br/>kind + options (U1)"]
  end
  EXT -->|extension_ui_request| DEC["decoder: RequestKind + options (U2)"]
  DEC --> RT["runtime: SetAwaitingInput + setPending"]
  RT -->|task_awaiting_input<br/>+kind +options| WS["ws.TaskEventPayload (U2)"]
  WS --> RED["agent-runs reducer<br/>+kind +options (U3)"]
  RED --> UI["drawer / card typed controls (U4)"]
  UI -->|steer: chosen value| ROE["RouteOrEnqueue"]
  ROE -->|extension_ui_response{id,response}| EXT

  subgraph Leak["F2 — narration leak (Pi only)"]
    TXT["assistant text narrates Ask_user(...)"] --> AR["appendReply → takeReply"]
    AR --> BS{"backstop:<br/>tool-call-shaped? (U6)"}
    BS -->|yes, parseable| CLEAN["post clean question text"]
    BS -->|yes, unparseable| PROSE["post readable prose (floor)"]
    BS -->|no| NORMAL["post reply unchanged"]
  end

  INST["InstallPiExtension fails → loud notice (U5)"] -.prevents.-> TXT
```

Diagram is authoritative alongside the prose below.

---

## Implementation Units

### U1. Extend the `ask_user` Pi extension to carry kind + options

**Goal:** Let the agent attach a question kind (free-text / pick-one / confirm)
and, for choice kinds, a list of options — rendering the matching Pi UI
primitive, with a graceful fallback when the richer primitive is unavailable.

**Requirements:** R6, R7, R8. Advances F1.

**Dependencies:** none.

**Files:**
- `server/internal/agent/pirun/extension/ask-user.ts` (modify)
- (embed is automatic via `server/internal/agent/pirun/extension/embed.go` — no change unless a new file is added)

**Approach:**
- Add optional `kind` (`"input" | "select" | "confirm"`, default `input`) and
  `options: string[]` parameters to the tool's typebox schema. Keep `question`
  required. Additive — omitting kind/options preserves today's free-text
  behavior (R7).
- Dispatch on kind: `select` → `ctx.ui.select` (or equivalent) with options;
  `confirm` → `ctx.ui.confirm`; `input`/default → existing `ctx.ui.input`.
- **Verify the real `ctx.ui` surface against the live Pi API first** (see
  Verification). If `select`/`confirm` are not exposed, fall back to
  `ctx.ui.input` with the options enumerated in the prompt string and return the
  raw typed answer — never emit JSON.
- Return the chosen option / confirm result as `content: [{type:"text", text}]`,
  matching the current contract.
- Preserve the `!ctx.hasUI` headless branch unchanged.

**Patterns to follow:** the current `registerTool` + typebox shape in the same
file; the existing `ctx.ui.input` call and text-content return.

**Test scenarios:**
- Covers AE2. `ask_user` with no kind/options → behaves as free-text `input`,
  identical request shape to today.
- Covers AE1. `ask_user` with `kind:"select"`, options `[React, Vue, Svelte]` →
  emits a select-style request carrying all three options.
- Covers AE5. `ask_user` with `kind:"confirm"` → emits a confirm-style request.
- Fallback: when the select/confirm primitive is unavailable, the tool still
  returns a typed answer and never returns a JSON blob.
- Headless: `ctx.hasUI === false` returns the "proceed on best judgment" text
  for every kind.

**Execution note:** Verify the Pi `ctx.ui` API shape before committing the
dispatch — the primitive names are unverified locally.

**Verification:** Run an agent in a real session; confirm a `select` question
renders options and a `confirm` question renders yes/no, and that the answer
returns to the agent as text. If primitives are missing, confirm the
input-fallback path produces a clean (non-JSON) prompt.

---

### U2. Propagate kind + options through the Go event pipeline

**Goal:** Carry the question kind and options from the decoded
`extension_ui_request` through the runtime to the `task_awaiting_input` WS
payload, mirroring the existing `pendingQuestion` plumbing.

**Requirements:** R8. Advances F1, R3, R4.

**Dependencies:** U1 (the extension must emit kind/options to decode).

**Files:**
- `server/internal/agent/pirun/decoder.go` (modify — extract options alongside the existing `RequestKind`)
- `server/internal/ws/events.go` (modify — add `PendingQuestionKind`, `PendingQuestionOptions` to `TaskEventPayload`)
- `server/internal/agent/runtime.go` (modify — pass kind/options into the `TypeTaskAwaitingInput` broadcast)
- `server/internal/agent/pirun/decoder_test.go` (modify/add)

**Approach:**
- Extend `decodeUIRequest` to pull an options list from the request params
  (best-effort per KTD2 — tolerate absence; normalize the unverified field names
  during U1 verification). `Event` already carries `RequestKind`; add an
  `Options []string` field.
- Add `PendingQuestionKind string` and `PendingQuestionOptions []string`
  (both `omitempty`) to `ws.TaskEventPayload`.
- In `runtime.go` `translate` (the `KindAwaitingInput` case), include
  `ev.RequestKind` and the options in the existing broadcast. No change to
  `SetAwaitingInput` persistence semantics or the pending-request id tracking.

**Patterns to follow:** the existing `PendingQuestion: ev.Prompt` flow in the
`TypeTaskAwaitingInput` broadcast; `omitempty` JSON tags on `TaskEventPayload`.

**Test scenarios:**
- Decoder: an `extension_ui_request` with kind `select` + options decodes to
  `Event{RequestKind:"select", Options:[...]}`.
- Decoder (KTD2 tolerance): a request missing kind/options decodes to free-text
  with empty options, no error.
- Decoder: malformed/extra fields are tolerated, stream continues.
- Payload: marshaled `TaskEventPayload` includes camelCase
  `pendingQuestionKind` / `pendingQuestionOptions` only when present (omitempty).

**Verification:** Unit tests pass; a live `select` question produces a
`task_awaiting_input` WS frame carrying kind + options.

---

### U3. Carry kind + options in the frontend store and types

**Goal:** Thread the new fields into the client `TaskEventPayload`/`AgentTask`
types and the `agent-runs` reducer so they survive both the live-event and
snapshot-replace paths.

**Requirements:** R8. Advances R3, R4.

**Dependencies:** U2 (wire fields exist).

**Files:**
- `src/types/index.ts` (modify — `TaskEventPayload` + `AgentTask`)
- `src/stores/agent-runs.ts` (modify — `task_awaiting_input` reduction; keep `AGENT_RUN_EVENT_TYPES` in sync if touched)
- `src/stores/agent-runs.test.ts` (new)
- `package.json` / `vitest.config.ts` (new — establish a vitest runner)

**Approach:**
- Add `pendingQuestionKind?: "input" | "select" | "confirm"` and
  `pendingQuestionOptions?: string[]` to the client `TaskEventPayload` and
  `AgentTask`.
- In the `task_awaiting_input` reducer case, set the new fields next to
  `pendingQuestion`. Ensure the snapshot-apply path (`applySnapshot`) carries
  them too, so a snapshot refetch doesn't clobber them (per the residual-findings
  flicker window).
- Establish a vitest runner — the reducer is currently untested and no frontend
  test runner exists. This is a prerequisite for testing the feature-bearing
  reducer change, not adjacent cleanup.

**Patterns to follow:** the existing `pendingQuestion` field on both types and
its reducer assignment; the existing snapshot vs event reconcile logic.

**Test scenarios:**
- Covers AE1. `task_awaiting_input` with kind `select` + options reduces to an
  `AgentTask` carrying both.
- Covers AE2. `task_awaiting_input` with no kind reduces to free-text (kind
  undefined), backward-compatible.
- Snapshot path: applying a snapshot that contains an awaiting-input task
  preserves kind/options (no clobber after a seq-gap refetch).
- Event ordering: a later `task_started` for the same task clears the pending
  fields as today.

**Execution note:** Establish the vitest runner first, then add the reducer
test (test-first for the new field reduction).

**Verification:** `npx vitest run` passes; `npx tsc --noEmit` clean.

---

### U4. Render typed prompt controls and wire answers back

**Goal:** Replace the plain-text `pendingQuestion` rendering with interactive
controls — text field, choice buttons, or confirm — each with an "Other"
free-text fallback, answered in place and routed through the existing steer
path.

**Requirements:** R1, R2, R3, R4, R5. Advances F1; A1, A2.

**Dependencies:** U3 (store carries kind/options).

**Files:**
- `src/components/super-threads/AgentThreadDrawer.tsx` (modify — render controls; reuse the existing composer as the "Other" / free-text input)
- `src/components/super-threads/AgentTaskCard.tsx` (modify — reflect kind in the inline summary)
- `src/components/super-threads/ThreadDrawerPanel.tsx` (modify if the answer wiring needs the chosen value)
- relevant CSS (e.g. the `q-pending-q` / `q-drawer-composer` styles)
- `src/components/super-threads/AgentThreadDrawer.test.tsx` (new)

**Approach:**
- Branch the awaiting-input render on `pendingQuestionKind`:
  - `input`/undefined → existing text field + send (R2).
  - `select` → a button per option (R3); selecting one sends that option's text
    via `steer` (the existing `onSend` → `steer` → `sendSteer` path). Keep the
    composer visible as the "Other" fallback (R3).
  - `confirm` → affirm / decline controls (R4); each sends its value via the
    same path.
- On submit/selection, reuse `steer(sessionId, agentId, value)` — no new
  transport. The backend `RouteOrEnqueue` already turns it into
  `ExtensionUIResponse` when the task is `awaiting_input` (R5), and the answer is
  also posted as the human's chat message as today.
- Confirm the exact value Pi expects for select/confirm answers during U1
  verification (option text vs index; "yes"/"no" vs boolean) and send that.

**Patterns to follow:** the existing composer (`val`/`send()`/`onSend`) in
`AgentThreadDrawer.tsx`; the `awaiting_input` render branch in both components;
`AgentAvatar` + agent color usage.

**Test scenarios:**
- Covers AE1. A `select` task renders three option buttons plus a free-text
  field; clicking "Vue" calls `steer` with "Vue"; typing "Solid" in Other calls
  `steer` with "Solid".
- Covers AE2. A free-text task renders a single text field; submitting calls
  `steer` with the typed value.
- Covers AE5. A `confirm` task renders affirm/decline; declining calls `steer`
  with the decline value.
- Empty/whitespace input is not sendable (send disabled), matching today.
- The card summary reflects the question across kinds without rendering JSON.

**Verification:** Manual run of a real `select`/`confirm`/free-text question in
a session — each renders the right control, answering resumes the agent, and the
awaiting-input state clears.

---

### U5. Harden the silent extension-install failure

**Goal:** Make a failed `ask_user` extension install loud and visible instead of
silently leaving the agent unable to ask, which is what lets a narrated leak
happen.

**Requirements:** R10. Advances R9.

**Dependencies:** none.

**Files:**
- `server/internal/workspace/manager.go` (`InstallPiExtension`, ~lines 592–616)
- `server/internal/handler/workspace.go` and/or `server/internal/handler/sessions.go` (the `provisionAgentTools` call sites — surface the failure to the session)
- relevant manager/handler test file

**Approach:**
- Escalate the install failure from `slog.Warn` to `slog.Error` and return/
  propagate a signal so the caller can surface it (rather than swallowing to
  `nil`). Keep provisioning non-fatal for the *workspace* (the session still
  comes up) but no longer silent.
- Surface a session-visible notice that the agent cannot ask questions
  (mechanism: a system/agent chat message or an existing notice channel — choose
  the lightest existing surface during implementation).
- Preserve the base64-over-the-wire install command (shell-quoting safety).

**Patterns to follow:** the existing `logFn` warning in `InstallPiExtension`; how
other provisioning failures are surfaced, if any; the residual-findings note
about a shared `InstallPi`/`InstallTools` installer (coordinate but do not
expand scope into that refactor).

**Test scenarios:**
- Install exec failure logs at error level and returns a non-nil signal (not
  swallowed).
- Workspace/session still reaches ready state on install failure (non-fatal
  preserved).
- The session receives a visible notice when the install fails.
- Success path is unchanged and emits no spurious notice.

**Verification:** Simulate an install failure (e.g. force the exec to fail) and
confirm the error log + session notice appear and the agent does not silently
narrate questions.

---

### U6. Backstop: sanitize narrated tool-call text before it posts

**Goal:** Detect a reply that is shaped like an `ask_user` tool call and turn it
into a clean question (or, if unparseable, readable prose) before it reaches
chat — so a narrated question is never shown as raw JSON.

**Requirements:** R9, R11, R12. Advances F2.

**Dependencies:** none (independent of U1–U4).

**Files:**
- `server/internal/agent/runtime.go` (intercept in `finalizeLocked` between `takeReply` and `replyPoster`)
- a small detector/sanitizer helper (new file under `server/internal/agent/`, e.g. `agent/question_backstop.go`)
- corresponding `_test.go`

**Approach:**
- Add a detector that recognizes a reply whose content is dominated by an
  `ask_user(...)` / `Ask_user({...})` tool-call shape (case-insensitive,
  tolerant of whitespace/escaping).
- When matched and the `question` value is extractable, replace the posted reply
  with the clean question text (R11). The user answers via the normal composer;
  this is the clean-text floor, not a synthesized interactive prompt.
- When matched but unparseable, strip to readable prose — never post a JSON
  fragment (R12).
- When not matched, post the reply unchanged.
- Scope the pattern narrowly to avoid false positives on prose that merely
  mentions `ask_user` (e.g. require the call-shape to be the substantive content
  of the reply, not an inline mention).

**Patterns to follow:** the `takeReply` → `replyPoster` call sequence in
`finalizeLocked`; existing helper/test layout in `server/internal/agent/`.

**Test scenarios:**
- Covers AE3 (reframed). A reply of
  `Ask_user({"question":"What file should I create?"})` posts as the clean
  question "What file should I create?", not the JSON.
- Covers AE4. A truncated/garbled tool-call reply posts as readable prose, never
  a JSON fragment.
- Negative: a normal reply that mentions the words "ask_user" in a sentence is
  posted unchanged (no false positive).
- Negative: an empty reply / fallback "(agent finished without a text
  response.)" path is unaffected.
- A reply that is partly prose and partly a trailing tool-call shape extracts
  the question without dropping meaningful prose (or posts clean text per the
  chosen rule — pin the rule in the test).

**Verification:** Unit tests pass; in a live session, force a narrated
`ask_user` (e.g. before/without the extension) and confirm the chat shows a
clean question, never JSON.

---

## Scope Boundaries

**Deferred for later**
- Multi-question batches (several questions in one prompt) — one question per
  `awaiting_input`.
- Surfacing prompts in the main chat timeline as a distinct message type — they
  stay scoped to the agent thread (the U6 backstop's clean-text floor is the one
  exception, and it is a sanitized chat message, not a thread prompt).

**Outside this product's identity**
- Any typed-prompt or backstop work inside the legacy `claude` executor — that
  harness is being removed (see [[legacy-claude-harness-removal]] / origin). All
  units here target the Pi path only.

**Deferred to Follow-Up Work**
- Extracting the shared `InstallPi` / `InstallTools` / `InstallPiExtension`
  installer (residual-findings P2) — touch-adjacent to U5 but out of scope for
  this plan unless it falls out naturally.

---

## Dependencies / Assumptions

- **Pi `ctx.ui` API shape is unverified locally.** U1 must verify whether
  `select`/`confirm` primitives exist; the fallback path keeps the feature
  shippable either way.
- **No frontend test runner exists yet.** U3 establishes vitest; U3/U4 test
  scenarios depend on it.
- **The answer transport is unchanged.** Choice/confirm answers ride the
  existing `steer` WS path and resolve as `ExtensionUIResponse` keyed by the
  tracked request id (KTD15), under the per-key lock (KTD9), clearing the
  awaiting-input ceiling (KTD8). Verify the exact `response` value Pi expects for
  select/confirm during U1.
- **KTD constraints honored:** KTD2 (decoder tolerates drift), KTD6 (kind/options
  stay in the AgentRunEvent seq family, not `session_update`), KTD14 (the answer
  path stays membership-gated as today).

---

## Outstanding Questions

**Deferred to Planning → resolved**
- Backstop placement → Pi reply-finalize path (legacy harness removed).
- Install-failure contract → loud (error log + session notice), non-fatal.

**Deferred to Implementation**
- Exact Pi `ctx.ui.select`/`confirm` request + response shapes (verify against
  the live API in U1).
- The precise lightest session-visible surface for the U5 install-failure notice.
- The exact extraction rule for a mixed prose+tool-call reply in U6 (pin via
  test).

---

## Sources & Research

- Origin requirements: `docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md`
- Pi harness KTDs: `docs/plans/2026-06-03-002-feat-pi-harness-integration-plan.md` (KTD15 ask-user mechanism, KTD8 ceiling, KTD9 lock, KTD6 event family)
- Residual findings (test gaps, install dedup): `docs/residual-review-findings/feat-pi-harness-integration.md`
- Answer-path trace: `server/internal/handler/websocket.go` (`handleSteer`), `server/internal/agent/runtime.go` (`RouteOrEnqueue`, `setPending`/`pendingRequest`/`clearPending`, `finalizeLocked`), `server/internal/agent/pirun/protocol.go` (`ExtensionUIResponse`)
- Render sites: `src/components/super-threads/AgentThreadDrawer.tsx`, `AgentTaskCard.tsx`; reducer `src/stores/agent-runs.ts`; payload `server/internal/ws/events.go`
