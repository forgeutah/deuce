---
title: "fix: Render ask_user tool calls as friendly question rows, never raw JSON"
type: fix
status: completed
date: 2026-06-10
origin: docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md
---

# fix: Render ask_user tool calls as friendly question rows, never raw JSON

## Summary

When an agent calls the `ask_user` tool, the interactive question prompt renders
correctly — but the tool call itself also appears in the agent-thread action log
(and on the task card's live "working" line) as raw JSON:
`Ask_user({"question":"What would you like to name the file…"})`. This is the
one display surface the interactive-questions work missed: `normalizeTool`
title-cases unknown tool names and `headlineArg` has no `question` key, so it
falls back to dumping the whole args object as compact JSON. The origin
requirement that "a question is never displayed to the user as raw JSON, on any
path" is violated on the action-log path (see origin: `docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md`, R9).

Fix the display at two layers: server-side at decode time, so new persisted
rows and live broadcasts carry a friendly label and the question text; and
client-side at render time, so historical rows already persisted with the raw
JSON arg also render clean. The row stays visible while the prompt is active
and pairs the question with the user's answer in task history.

## Requirements

- R1. An `ask_user` tool call renders in the thread action log as a friendly
  question row showing the question text — never raw JSON or a literal
  tool-call string (closes origin R9 on the action-log path).
- R2. The task card's live "working" line shows the same friendly form for an
  in-flight `ask_user` call, never raw JSON.
- R3. Historical action rows already persisted with tool `Ask_user` and a raw
  JSON arg render friendly too — on first load, after a snapshot refetch, and
  in a terminal task's collapsed action log.
- R4. The action row remains visible while the interactive prompt is showing;
  once answered, the completed row pairs the question with the user's answer in
  task history.
- R5. Rendering of all other tools is unchanged, and an unparseable or
  unexpected arg never crashes the renderer — it degrades to readable text.

## Key Technical Decisions

- **Fix at decode time AND render time.** The server-side fix (decoder/
  `headlineArg`) makes the persisted row and the live broadcast identical, so a
  snapshot refetch can never flip a friendly row back to raw JSON. But rows
  persisted before the fix carry the raw arg forever, so the frontend must also
  special-case rendering. Neither layer alone covers everything.
- **Friendly vocabulary: normalized tool name `Ask`, arg = the question text.**
  `normalizeTool("ask_user")` gets an explicit case (today it falls to the
  generic title-case branch producing `Ask_user`); the headline arg for this
  tool is the extracted `question` value, not the args JSON. The frontend
  special-case must match both the new `Ask` name and the legacy persisted
  `Ask_user` name.
- **Server fallback floor is readable text, not JSON.** If the `question` key
  is missing or unparseable at decode time, the arg degrades to a short
  readable placeholder — under R1, falling back to the compact-JSON dump is no
  longer acceptable for this tool.
- **Legacy rows: extract the question client-side, accept losing kind/options
  from history.** The frontend parses the legacy JSON arg (`JSON.parse` in a
  try/catch) to pull `question` for display. A select question's options exist
  in history only inside that raw arg; they were never rendered historically
  and are deliberately not resurrected — the question text alone is the
  historical record. (The live prompt's options are unaffected; they ride
  `pendingQuestionOptions`.)
- **Keep the row while the prompt is active.** The friendly row and the
  "needs your input" prompt coexist; after the answer, the row is the durable
  question-and-answer record in the collapsed action history.

## Implementation Units

### U1. Server: decode ask_user tool events into a friendly action

**Goal:** `tool_execution_start` / `tool_execution_end` for `ask_user` produce
an action with tool name `Ask` and the question text as the arg, in both the
persisted row and the live broadcast.

**Requirements:** R1, R2 (data side), R5.

**Dependencies:** none.

**Files:**
- `server/internal/agent/pirun/protocol.go` (`normalizeTool`, `headlineArg` /
  a tool-aware variant)
- `server/internal/agent/pirun/decoder.go` (`decodeToolStart` — the only place
  with access to both the raw tool name and the args map)
- `server/internal/agent/pirun/decoder_test.go`

**Approach:** Add an explicit `ask_user` case to `normalizeTool` returning
`Ask`. Make headline-arg extraction tool-aware for this one tool: pull the
`question` string from the args map; when absent or not a string, use a short
readable placeholder instead of the compact-JSON fallback. `decodeToolEnd`
needs only the name normalization (its output is the tool result, which for
`ask_user` is the user's answer — already plain text). Match the raw tool name
case-insensitively, consistent with `normalizeTool`'s existing behavior.

**Patterns to follow:** the existing switch cases in `normalizeTool` and the
`argKeys` priority lookup in `headlineArg` (`server/internal/agent/pirun/protocol.go`);
existing fixtures in `decoder_test.go` (`TestNormalizeTool`, the
tool-event decode tests).

**Test scenarios:**
- An `ask_user` `tool_execution_start` with `{"question":"Which file?"}`
  decodes to `Tool: "Ask"`, `Arg: "Which file?"`.
- An `ask_user` start with `kind` and `options` alongside `question` still
  yields just the question text as the arg.
- An `ask_user` start with no `question` key (or a non-string value) yields the
  readable placeholder — never the JSON dump.
- An `ask_user` `tool_execution_end` decodes with `Tool: "Ask"` and the
  result text (the user's answer) as output.
- A question whose text contains braces/quotes/JSON-ish content passes through
  verbatim.
- Other unknown tools still title-case and still fall back to compact JSON
  (`custom_tool` → `Custom_tool`, args dump) — the generic path is unchanged.

**Verification:** Decoder tests pass; in a live session, a new `ask_user` call
shows `Ask(Which file?)`-shaped data in the `action_started` WS frame and the
persisted row.

### U2. Frontend: friendly question row in the action log and task card

**Goal:** Render `ask_user` actions — both new (`Ask`) and legacy persisted
(`Ask_user` with raw JSON arg) — as a question-styled row, mirroring the
existing `Think` special case; the task card live line follows suit.

**Requirements:** R1, R2, R3, R4, R5.

**Dependencies:** U1 (pins the new tool name the frontend must also match).

**Files:**
- `src/components/super-threads/atoms.tsx` (`ActionItem`, `TOOL_ICON`)
- `src/components/super-threads/AgentTaskCard.tsx` (live line special case)
- `src/components/super-threads/utils.ts` (pure question-extraction helper)
- `src/components/super-threads/question-action.test.ts` (helper spec,
  following the repo's existing runner-less spec convention — see
  `src/stores/agent-runs.test.ts`)

**Approach:** Add a pure helper that, given an action's tool + arg, answers
"is this an ask_user action, and what question text should display?" — matching
tool names `Ask` and `Ask_user`, parsing a JSON-shaped arg for `question` in a
try/catch, passing a plain-text arg through, and degrading to a readable label
when neither works. In `ActionItem`, branch on it beside the `Think` case:
render a question icon (e.g. a message/help Lucide icon added to `TOOL_ICON`
or used directly) with "Asked — {question}" phrasing instead of `tool(arg)`
parens; keep the completed row's output block (the user's answer) as-is so
history reads question → answer. In `AgentTaskCard`'s live line, apply the
same helper so the in-flight display shows the question, not the arg.

**Patterns to follow:** the `Think` branch in `ActionItem`
(`src/components/super-threads/atoms.tsx`) and the `Think` ternary in
`AgentTaskCard`'s live line; `stripMention`-style pure helpers in
`src/components/super-threads/utils.ts`.

**Test scenarios:**
- Legacy row `{tool: "Ask_user", arg: '{"question":"Which file?"}'}` extracts
  "Which file?".
- Legacy select-kind arg (question + kind + options JSON) extracts just the
  question.
- New row `{tool: "Ask", arg: "Which file?"}` passes the arg through.
- Unparseable arg (truncated JSON, empty string) degrades to a readable label —
  the helper never throws.
- A non-ask_user tool with a JSON-looking arg is not treated as a question row.
- An escaped question (`\"` / `\n` inside the JSON string) is unescaped by the
  parse, and a question containing an `@mention` renders as plain text.

**Verification:** `npx tsc --noEmit` and `npm run lint` clean. Manual pass in a
live session: while the agent waits, the thread shows the friendly row above
the "needs your input" prompt and the task card live line shows the question;
after answering, the collapsed history pairs question and answer; a session
with pre-fix history renders its old rows clean, including after a reconnect
(snapshot refetch).

## Scope Boundaries

**Deferred to Follow-Up Work**

Surfaced by research on this bug but outside the confirmed display-fix scope:

- Observability hardening for the structured-question pipeline: the decoder
  silently ignores `extension_error` events and logs unknown event types at
  Debug (`server/internal/agent/pirun/decoder.go`) — both invisible at default
  log levels if the pipeline ever does break.
- A watchdog for an `ask_user` call that never produces `extension_ui_request`
  (including the hang variant where no `tool_execution_end` arrives either),
  with a session-visible notice.
- Pi version pinning or version logging — Pi floats per-container, frozen at
  whatever pi.dev served at first provision; the event protocol was only ever
  verified against 0.74.2.
- `ask_user` behavior inside pi-subagents child agents (child Pi instances are
  invisible to Deuce's event channel — documented open design question in
  `docs/solutions/architecture-patterns/pi-loads-agent-skills-standard-in-rpc-mode.md`).
- Started-but-never-completed action rows spinning forever in terminal tasks
  (pre-existing for all tools when a process is torn down mid-call).
- Establishing a frontend test runner (vitest) so the accumulated spec files
  actually run.

## Sources & Research

- Origin requirements: `docs/brainstorms/2026-06-08-interactive-agent-questions-requirements.md`
  (R9 is the violated guarantee; this plan closes its last display path).
- Prior implementation plan: `docs/plans/2026-06-08-001-feat-interactive-agent-questions-plan.md`
  (shipped the prompt UI and the narrated-text backstop; the action log sat
  outside all three of its defense tiers).
- Raw-render mechanics: `server/internal/agent/pirun/protocol.go`
  (`normalizeTool` title-case default, `headlineArg` JSON fallback);
  render sites `src/components/super-threads/atoms.tsx` (`ActionItem`),
  `src/components/super-threads/AgentTaskCard.tsx` (live line).
- Persistence/replay constraint: action rows are stored verbatim and replayed
  via snapshot (`server/internal/agent/dbstore.go`, `src/stores/agent-runs.ts`),
  which is why the fix needs both layers.
