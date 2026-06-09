---
date: 2026-06-08
topic: interactive-agent-questions
---

# Interactive Agent Questions

## Summary

When an agent needs input mid-task, render its question as an interactive
inline prompt in the agent thread — a text field for open questions,
selectable buttons when the agent offers choices, with a free-text fallback —
and guarantee that a question is never shown to the user as raw JSON,
regardless of which path produced it.

## Problem Frame

Today an agent question can reach the user as a literal tool-call string in
the thread, e.g.:

```
Ask_user({"question":"What file would you like me to create? Please provide:\n1. The filename...\n2. The content..."})
```

This is the structured question path *failing*. The intended pipeline is
`extension_ui_request` → `KindAwaitingInput` → `task.pendingQuestion`, which
the Super Thread drawer/card already consumes. The JSON appears when that
structured event never fires — the model emits the call as assistant prose
instead. Known triggers: the Pi `ask_user` extension install silently failed
(the installer is non-fatal and returns `nil` on error), the legacy `claude`
harness has no `ask_user` tool at all, or the model narrates the call in text
even when the tool exists.

Two distinct quality gaps stack here:

1. **Even on the happy path, the question renders as plain text.** The drawer
   and task card print `task.pendingQuestion` verbatim — there is no input
   affordance attached to it, and no way for the agent to offer concrete
   choices.
2. **On the failure path, the user sees raw JSON** — which reads as a broken
   product, not a question.

Other harnesses (including Claude Code's own question UI) present questions as
typed, answerable controls. Deuce should meet that bar and additionally
guarantee the JSON failure mode can't surface.

## Key Decisions

- **Typed prompts, not just clean text.** The agent can attach a question
  *kind* (free-text / pick-one / confirm) and, for choice kinds, a set of
  options. The thread renders the matching control: a text field, selectable
  buttons, or a yes/no affirmation. This is a deliberate step beyond "strip the
  JSON and show prose" — chosen because concrete choices lower answer friction
  and let agents ask better questions.

- **A two-layer no-JSON guarantee.** Layer one hardens the structured path so a
  question reliably arrives as a structured event (install failures become
  loud/recoverable rather than silently dropping the tool). Layer two is a
  text-shaped-question backstop: when an agent message *looks* like a tool call
  (`ask_user(...)` / `Ask_user({...})`), it is intercepted, the question is
  extracted, and it renders through the same prompt widget instead of as JSON.

- **The guaranteed floor is "never raw JSON," not "always a rich prompt."**
  When only text leaks and the structured event never fired, the backstop can
  reconstruct a *clean question prompt* but not choices the agent never emitted
  structurally. Degrading to a free-text prompt on those paths is acceptable;
  showing JSON is not.

- **Rich choices are Pi-path only.** The legacy `claude` harness has no
  extension mechanism, so it cannot emit structured options. The backstop keeps
  it from leaking JSON, but its questions stay free-text.

- **Questions stay in the agent thread, not the main chat timeline.** This
  matches the existing `awaiting_input` routing; the prompt is modal to the
  task, answered in place.

## Actors

- A1. **Agent** — running inside a session's DevPod, blocks on a question when
  it needs a decision only the human can make.
- A2. **Human collaborator** — sees the prompt in the agent thread and answers
  it in place; their answer resumes the agent.

## Key Flows

- F1. **Structured typed question (happy path).**
  - **Trigger:** the agent calls `ask_user` with a question and (optionally) a
    kind and choices.
  - The task enters `awaiting_input`; the thread renders the matching control
    inline.
  - The human answers in place (types, picks a button, confirms, or uses the
    free-text fallback on a choice question).
  - The answer is delivered back to the agent and the task resumes.

- F2. **Leaked question (backstop path).**
  - **Trigger:** a question reaches the client as assistant text shaped like a
    tool call (legacy harness, failed extension, or model narration).
  - The text is detected, the question string extracted, and a free-text prompt
    rendered in place of the raw JSON.
  - The human answers; the answer is routed back through the normal reply path.
  - **Floor:** if extraction fails, the surfaced content is still a readable
    question, never a JSON blob.

## Requirements

**Prompt rendering & interaction**

- R1. An agent question in `awaiting_input` renders as an interactive prompt in
  the agent thread, with an affordance to answer it in place (no copy-pasting,
  no separate composer hunt).
- R2. For a free-text question, the prompt presents a text input and a send
  action.
- R3. For a pick-one question, the prompt presents the agent's options as
  selectable buttons, plus a free-text "Other" fallback so the human is never
  trapped by the offered set.
- R4. For a confirm question, the prompt presents an affirm/decline control.
- R5. Selecting or submitting an answer delivers it back to the waiting agent
  and transitions the task out of `awaiting_input`.

**Question data model**

- R6. The `ask_user` capability supports an optional question *kind* (free-text
  / pick-one / confirm) and, for choice kinds, an optional list of options.
- R7. A question with no kind/options behaves as free-text — the change is
  additive and backward-compatible with the current question-only shape.
- R8. The kind and options survive end to end (agent → structured event →
  thread render) without being flattened back into a text string.

**Leak prevention (the no-JSON guarantee)**

- R9. A question is never displayed to the user as raw JSON or as a literal
  tool-call string, on any path.
- R10. The structured-path failure modes that currently cause leaks are
  hardened: a failed `ask_user` extension install is surfaced (loud /
  recoverable), not silently dropped such that the agent narrates the call as
  text.
- R11. A backstop detects agent text shaped like an `ask_user` tool call,
  extracts the question, and renders it as a free-text prompt (R2) instead of
  the raw string.
- R12. When the backstop cannot parse the leaked text into a question, the
  surfaced content is still readable prose, never a JSON blob.

## Acceptance Examples

- AE1. **Covers R3.** Agent asks "Which framework?" with options
  `[React, Vue, Svelte]`. The thread shows three buttons plus an "Other" field.
  Picking "Vue" resumes the agent with "Vue"; typing "Solid" in Other resumes
  it with "Solid".
- AE2. **Covers R2, R7.** Agent asks a question with no kind or options. The
  thread shows a single text field — identical to today's question semantics,
  now interactive.
- AE3. **Covers R9, R11.** A legacy-harness run emits
  `Ask_user({"question":"What file should I create?"})` as text. The user sees
  a free-text prompt asking "What file should I create?", not the JSON.
- AE4. **Covers R12.** A malformed leak (truncated/garbled tool-call text) that
  can't be parsed surfaces as readable text, not a JSON fragment.
- AE5. **Covers R4.** Agent asks a confirm-kind question ("Proceed with the
  force-push?"). The thread shows affirm/decline; declining resumes the agent
  with a negative answer.

## Scope Boundaries

**Deferred for later**

- Multi-question batches (asking several questions in one prompt) — the model
  stays one question per `awaiting_input`.
- Surfacing prompts in the main chat timeline as a distinct message type — they
  remain scoped to the agent thread.

**Outside this product's identity**

- Full typed-prompt support inside the legacy `claude` harness — it has no
  extension channel to carry structured choices. Its only guarantee is the
  no-JSON backstop with free-text prompts. (If/when the legacy harness is
  retired, this boundary dissolves.)

## Dependencies / Assumptions

- The Pi `ask_user` extension is the carrier for structured kind/options; this
  assumes Pi's extension UI channel can convey option sets back through the
  `extension_ui_request` event (the decoder already carries a `RequestKind`).
- Assumes `ctx.hasUI` is true in Deuce's Pi RPC mode (the extension's happy
  path calls `ctx.ui.input` directly); the headless `hasUI === false` branch
  remains the correct behavior for genuinely non-interactive contexts.
- Assumes the agent-thread drawer/card is the right and only surface for the
  prompt (consistent with current `awaiting_input` routing).

## Outstanding Questions

**Deferred to planning**

- Exact shape of the options payload through Pi's extension UI channel
  (whether `ctx.ui` exposes a select/confirm primitive or whether options ride
  inside the input request) — confirm against Pi's extension API during
  planning.
- How "loud / recoverable" extension-install failure should manifest
  operationally (retry, surfaced session warning, or agent-disable) — a
  reliability design choice for planning.
- The precise detection boundary for the text backstop (which patterns count as
  a leaked `ask_user` call without false-positiving on legitimate prose that
  mentions the tool).
