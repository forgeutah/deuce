---
title: "feat: Render markdown in agent replies and chat messages"
type: feat
status: completed
date: 2026-07-28
depth: standard
---

# feat: Render markdown in agent replies and chat messages

## Summary

Deuce's agent replies and chat messages currently render as raw, unformatted
text. When `@deuce` returns a response full of markdown (`##` headers, `**bold**`,
`###`, numbered lists, code fences), the user sees it all crammed together —
`…what this repo is.## This is the **Unify Agent Workspace**…` — because the
reply is dropped into a plain `<div>`/`<p>` with no markdown parsing and no
whitespace preservation.

The fix wires up `react-markdown` + `remark-gfm` — **already in `package.json`
since the initial commit but never referenced in `src/`** — into a single shared
`<Markdown>` renderer, adds fenced-code syntax highlighting, adds scoped prose
CSS using existing Primer tokens, and applies the renderer across all message
surfaces (agent thread-drawer replies, chat bubbles for human + system messages).
The compact task-card summary stays a single truncated line via a pure
`toPlainText()` helper, since block markdown would break that layout.

This is a **frontend-only rendering change**. No backend, message-storage, or Pi
agent-output changes.

---

## Problem Frame

**Reported symptom:** the agent's reply "looks pretty raw, but it seems like it
should have some formatting." The example is a full `@deuce` answer where every
markdown construct is inert and newlines have collapsed into a wall of text.

**Root cause (confirmed by code read):**

- The agent's reply text (`task.reply`) is rendered as a plain string in
  `src/components/super-threads/AgentThreadDrawer.tsx:184` (the full reply, inside
  `.q-resp .bd`) and `src/components/super-threads/AgentTaskCard.tsx:127` (a
  one-line card summary).
- `.q-resp .bd` in `src/styles/globals.css:622` sets `line-height` but **no
  `white-space` preservation** — so even literal `\n` between a sentence and a
  `## ` header collapse to a single space, producing the run-together output.
- Chat bubbles in `src/components/chat/ChatView.tsx:249` render
  `message.content` inside `<p className="whitespace-pre-wrap">` — newlines
  survive, but no markdown is parsed, so `**bold**` and lists stay literal.
- `react-markdown@^10.1.0` and `remark-gfm@^4.0.1` are dependencies but a repo
  grep finds **zero references** in `src/`. There is even a leftover
  `.q-resp code { … }` rule (`src/styles/globals.css:634`) that anticipated
  markdown code spans. The wiring was simply never done.

**Scope decisions (confirmed with requester):**

- Render markdown across **all message text** via one shared component — agent
  replies, system notices, and human-typed chat messages.
- Include **syntax highlighting** for fenced code blocks.

---

## Requirements

- **R1** — Agent replies in the thread drawer render markdown: headers, ordered
  and unordered lists, bold/italic, inline code, fenced code blocks, blockquotes,
  tables, and links display as formatted output, not literal syntax.
- **R2** — Human-typed chat messages and system notices render through the same
  shared renderer, so formatting is consistent everywhere a message body appears.
- **R3** — Fenced code blocks are syntax-highlighted with a dark-mode theme that
  fits the existing Primer palette; an unknown or missing language degrades
  gracefully to an unhighlighted styled block.
- **R4** — Rendering is XSS-safe: model- and user-generated markdown cannot
  inject executable HTML or `javascript:` URLs. Links open in a new tab with safe
  `rel` attributes.
- **R5** — The compact task-card summary
  (`AgentTaskCard.tsx`, terminal state) stays a single line — markdown is stripped
  to readable plain text so headers/lists don't blow out the card layout.
- **R6** — No regression to intentionally-verbatim surfaces: tool output
  (`.q-out` / Bash stdout / diffs) and the terminal/logs panels keep rendering as
  raw monospace, untouched.

---

## Key Technical Decisions

- **KTD1 — One shared `<Markdown>` component, reusing installed deps.** Build a
  single presentational `src/components/ui/markdown.tsx` wrapping `react-markdown`
  with `remark-gfm`. Every message surface routes through it. No new
  markdown-core dependency is added (the two are already present). This keeps
  formatting behavior identical across chat and the agent drawer and gives one
  place to enforce safety.

- **KTD2 — XSS-safe by default; treat all message content as untrusted.** Do
  **not** add `rehype-raw` and do **not** disable escaping. `react-markdown`
  escapes raw HTML out of the box, so embedded `<script>`/`<img onerror=…>` in a
  reply renders as inert text. Override the `a` renderer to force
  `target="_blank"` + `rel="noopener noreferrer nofollow"` and to drop
  non-`http(s)`/`mailto` protocols (defeats `javascript:` links). Agent output is
  model-generated and human input is arbitrary — both are untrusted for
  rendering purposes.

- **KTD3 — Syntax highlighting via `react-syntax-highlighter` (Prism async).**
  Wire a custom `code` renderer that hands fenced blocks to
  `react-syntax-highlighter`'s `PrismAsyncLight` build with a curated set of
  registered languages (js/ts/tsx, go, json, bash/shell, sql, yaml, diff,
  markdown, plus a sensible default) and a GitHub-dark-aligned theme mapped onto
  Primer variables. Async/light build keeps bundle weight down; inline code (no
  language, single backtick) skips the highlighter and uses the existing
  `.q-resp code` styling. *Alternative considered:* Shiki (VS Code-grade fidelity)
  — richer output but ships a WASM/oniguruma runtime and async theme loading that
  is heavier than warranted for short chat code blocks; deferred.

- **KTD4 — Compact card stays single-line via a pure `toPlainText()` helper.**
  A dependency-free `toPlainText(markdown: string): string` strips common
  markdown syntax (heading markers, emphasis, list bullets, code fences/backticks,
  link syntax → link text) and collapses whitespace to single spaces. This is the
  one genuinely pure, unit-testable piece and is the safest way to keep the card
  layout intact when replies contain block markdown.

- **KTD5 — Add a minimal jsdom render-test setup (deliberate test-scope
  extension).** The repo's Vitest suite is pure-logic only (`npm test` — reducer,
  visibility). The security-critical claims in KTD2 (raw HTML escaped, unsafe link
  protocols dropped) are behavioral and deserve an actual assertion, not just a
  config claim. Add `jsdom` + `@testing-library/react` + `@testing-library/jest-dom`
  and a `vitest.config.ts` with a jsdom environment for `*.tsx` tests, so the
  `<Markdown>` safety behavior is tested. This is a small, bounded extension of the
  existing test philosophy, taken because the content being rendered is untrusted.

- **KTD6 — Prose CSS scoped to a container class, dark-mode only.** Add a single
  `.md` prose block in `src/styles/globals.css` styling `h1–h4`, `p`, `ul/ol/li`,
  `blockquote`, `pre`, `code`, `table`, `hr`, and links, using existing
  `--color-*` Primer variables. Scoping under `.md` prevents prose spacing from
  leaking into the surrounding chat/card chrome.

---

## Rendering Surfaces

| Surface | File | Today | After |
| --- | --- | --- | --- |
| Agent reply (drawer) | `AgentThreadDrawer.tsx:184` `.q-resp .bd` | raw string, newlines collapsed | `<Markdown>` |
| Chat bubble (human + system) | `ChatView.tsx:249` | plain `whitespace-pre-wrap` | `<Markdown>` |
| Compact card summary | `AgentTaskCard.tsx:127` `.tc-d .l` | raw `task.reply` inline | `toPlainText(reply)`, single line |
| Drawer prompt echo (`.q-req .bd`) | `AgentThreadDrawer.tsx:132` | `<Mentioned>` (mention highlight) | unchanged (see Deferred) |
| Tool output / logs | `atoms.tsx` `.q-out`, logs panel | verbatim mono | unchanged (R6) |

---

## Scope Boundaries

### In scope
- Shared `<Markdown>` component + safe link handling + XSS-safe defaults
- Syntax highlighting for fenced code blocks
- Scoped prose CSS (dark Primer tokens)
- Wiring markdown into agent drawer replies and chat bubbles (human + system)
- `toPlainText()` helper for the compact card summary
- Minimal jsdom/testing-library harness for the safety-relevant tests

### Deferred to Follow-Up Work
- Unifying `@mention` highlighting into the markdown pipeline. The drawer prompt
  echo keeps using `Mentioned` for now; chat bubbles do not currently highlight
  mentions, so switching them to `<Markdown>` is not a regression. A small remark
  plugin to wrap `@\w+` tokens inside rendered markdown is a clean later addition.
- Streaming/incremental markdown rendering while a run is live (replies render on
  completion today).
- Copy-to-clipboard button on code blocks.
- Per-message "view raw markdown" toggle.

### Out of scope
- Any backend, message-storage, or WebSocket-payload change.
- Pi agent output format — the agent already emits markdown; this plan only
  renders it.
- Terminal panel and workspace-logs rendering (intentionally verbatim; R6).

---

## Implementation Units

### U1. Add jsdom render-test harness

**Goal:** Enable DOM-level component tests so the safety-critical `<Markdown>`
behavior in later units can be asserted, without disturbing the existing
pure-logic suites.

**Requirements:** Supports R4 verification.

**Dependencies:** none.

**Files:**
- `package.json` (add dev deps: `jsdom`, `@testing-library/react`,
  `@testing-library/jest-dom`)
- `vitest.config.ts` (new — jsdom environment, setup file, keep existing
  pure-logic suites green)
- `src/test/setup-dom.ts` (new — imports `@testing-library/jest-dom`)
- `src/test/smoke.test.tsx` (new — trivial render smoke test)

**Approach:** Add a `vitest.config.ts` (none exists today) configuring the jsdom
environment and a setup file. Confirm the existing pure-logic `*.test.ts` suites
still pass under the new config. Keep the config minimal — `environment: 'jsdom'`,
`setupFiles`, and the `@/` alias already used by Vite/tsconfig.

**Patterns to follow:** existing `@/` alias config in `vite.config.ts` and
`tsconfig.app.json`.

**Test scenarios:**
- Happy path: a trivial component renders into jsdom and a `@testing-library`
  query finds its text — proves the harness works.
- Regression guard: existing pure-logic suites (`agent-runs.test.ts`,
  `message-visibility.test.ts`, `question-action.test.ts`, `repo.test.ts`,
  `session-store.test.ts`) still pass under the new config.

**Verification:** `npm test` runs both the new `.tsx` test and all existing
suites green.

---

### U2. Shared `<Markdown>` component with safe defaults

**Goal:** One presentational renderer that parses GFM markdown and is safe against
HTML/script injection and unsafe link protocols.

**Requirements:** R1, R2, R4.

**Dependencies:** U1 (for its tests).

**Files:**
- `src/components/ui/markdown.tsx` (new)
- `src/components/ui/markdown.test.tsx` (new)

**Approach:** Wrap `react-markdown` with `remark-gfm`. Wrap output in a
`<div className="md">` (styled in U4). Override the `a` component to add
`target="_blank" rel="noopener noreferrer nofollow"` and to render as plain text
(or drop the href) when the protocol is not `http(s)`/`mailto`. Do **not** add
`rehype-raw`; rely on react-markdown's default HTML escaping. Leave a `code`
render seam that U3 fills (default styled `<code>`/`<pre>` until then). Accept a
`className` passthrough so callers can add surface-specific spacing.

**Patterns to follow:** shadcn/ui component conventions in `src/components/ui/`;
`cn()` from `src/lib/utils`.

**Test scenarios:**
- Happy path: `## Heading` renders an `<h2>`; `- a\n- b` renders a `<ul>` with two
  `<li>`; `**bold**` renders `<strong>`; `` `x` `` renders inline `<code>`.
- Happy path: GFM table and task-list syntax render as `<table>` / checkbox list
  (proves `remark-gfm` is active).
- Error/safety: input containing `<img src=x onerror="alert(1)">` renders the tag
  as inert escaped text — no `<img>` element is created in the DOM.
- Error/safety: a `[click](javascript:alert(1))` link does not produce an anchor
  with a `javascript:` href.
- Edge: an `http(s)` link renders an `<a>` with `target="_blank"` and
  `rel` containing `noopener` and `noreferrer`.
- Edge: empty string and whitespace-only input render nothing/blank without
  throwing.

**Verification:** All `markdown.test.tsx` scenarios pass; `npx tsc -b --noEmit`
clean.

---

### U3. Fenced-code syntax highlighting

**Goal:** Fenced code blocks render highlighted; inline code and unknown
languages degrade gracefully.

**Requirements:** R3.

**Dependencies:** U2.

**Files:**
- `package.json` (add `react-syntax-highlighter` + its types)
- `src/components/ui/markdown-code.tsx` (new — the `code` renderer + language
  registration + theme mapping)
- `src/components/ui/markdown.tsx` (wire the `code` override)
- `src/components/ui/markdown-code.test.tsx` (new)

**Approach:** Implement the `code` component override: when the node is a fenced
block with a ``` ```lang ``` info string, render via `PrismAsyncLight` with a
curated set of registered languages and a GitHub-dark theme mapped to Primer
variables; inline code (no language) falls through to the styled `<code>` from
U4. Unknown languages render as an unhighlighted but styled `<pre>`. Lazy/async
language loading to keep bundle weight down.

**Patterns to follow:** dark-mode-only design tokens in `src/styles/globals.css`;
`DEUCE`/`--ac` accent usage for consistency where a highlight accent is needed.

**Test scenarios:**
- Happy path: a ```` ```ts ```` block renders a highlighted `<pre>` containing the
  code text (token `<span>`s present).
- Edge: a fenced block with no language renders a styled `<pre>` with the raw text
  and no crash.
- Edge: a fenced block with an unregistered language (e.g. `brainfuck`) falls back
  to an unhighlighted styled block without throwing.
- Happy path: inline `` `code` `` does **not** route through the highlighter
  (stays a simple inline `<code>`).

**Verification:** `markdown-code.test.tsx` passes; a manual reply containing a
fenced `ts` block shows highlighted output in the drawer.

---

### U4. Prose CSS for rendered markdown

**Goal:** Rendered markdown reads as clean prose that fits the dark Primer theme,
without leaking spacing into surrounding chrome.

**Requirements:** R1, R3 (code block container styling).

**Dependencies:** U2 (targets the `.md` container).

**Files:**
- `src/styles/globals.css` (add a scoped `.md { … }` prose block; reconcile with
  the existing `.q-resp code` rule at line 634 so inline code styling isn't
  duplicated or conflicting)

**Approach:** Add `.md` rules for `h1–h4`, `p`, `ul/ol/li`, `blockquote`, `pre`,
`code`, `table`/`th`/`td`, `hr`, and `a`, using existing `--color-*` variables and
sensible vertical rhythm. Ensure `pre` scrolls horizontally rather than expanding
the drawer/bubble width. Verify the drawer reply (`.q-resp .bd`) and chat bubble
paddings still line up once the inner `<p>` no longer carries `whitespace-pre-wrap`.

**Patterns to follow:** existing Primer variable usage and spacing conventions
throughout `src/styles/globals.css`.

**Test scenarios:** Test expectation: none -- pure styling; verified visually
against a reply that exercises headers, lists, blockquote, table, and a code
block in the drawer and a chat bubble.

**Verification:** Visual check in the running app (`npm run dev`): the reported
example reply renders with headers on their own lines, formatted lists, and a
highlighted code block; no layout overflow in the drawer or chat column.

---

### U5. Wire `<Markdown>` into drawer replies and chat bubbles

**Goal:** Route the actual message surfaces through the shared renderer.

**Requirements:** R1, R2, R6.

**Dependencies:** U2, U3, U4.

**Files:**
- `src/components/super-threads/AgentThreadDrawer.tsx` (replace the raw
  `task.reply` render in `.q-resp .bd` with `<Markdown>`; keep the failed/
  cancelled/Done fallback strings as plain text)
- `src/components/chat/ChatView.tsx` (replace `<p className="whitespace-pre-wrap">
  {message.content}</p>` in `MessageBubble` with `<Markdown>` for both human and
  system-notice authors)
- `src/components/chat/ChatView.test.tsx` (new)
- `src/components/super-threads/AgentThreadDrawer.test.tsx` (new)

**Approach:** Swap the two render sites. Leave `expandableContent` and `.q-out`
tool-output blocks untouched (R6). Leave the drawer prompt echo's `<Mentioned>`
untouched (Deferred). Confirm the terminal fallback strings ("Run failed.", etc.)
still render when `task.reply` is absent.

**Patterns to follow:** the surrounding component structure in each file; don't
disturb the visibility-filter or super-thread anchoring logic.

**Test scenarios:**
- Happy path: a terminal task whose `reply` contains `## H` + a list renders
  formatted markup (not literal `##`) in the drawer.
- Happy path: a human `message.content` with `**bold**` renders `<strong>` in the
  chat bubble.
- Happy path: a system-notice message (agent-typed, nil author) renders through
  `<Markdown>` too.
- Regression: a terminal task with `reply` undefined still shows the correct
  fallback string ("Run failed." / "Run cancelled." / "Done.").
- Regression (R6): tool-output `.q-out` content and `expandableContent` still
  render verbatim, unaffected by the markdown change.

**Verification:** Tests pass; the reported example reply now renders formatted in
the drawer; a code-fenced human message renders highlighted in chat.

---

### U6. `toPlainText()` helper + compact card summary

**Goal:** Keep the one-line task-card summary readable and single-line when the
reply contains block markdown.

**Requirements:** R5.

**Dependencies:** none (pure helper; wiring is independent of U2–U5).

**Files:**
- `src/lib/markdown-plain.ts` (new — `toPlainText(markdown: string): string`)
- `src/lib/markdown-plain.test.ts` (new)
- `src/components/super-threads/AgentTaskCard.tsx` (use `toPlainText(task.reply)`
  in the terminal `.tc-d .l` summary)

**Approach:** Implement a dependency-free string transform that strips heading
markers, emphasis markers, list bullets/numbers, blockquote markers, inline/fenced
code backticks, and rewrites `[text](url)` → `text`, then collapses all runs of
whitespace (including newlines) to single spaces and trims. Apply it only to the
compact card summary; CSS truncation (existing `.tc-d .l`) handles overflow.

**Patterns to follow:** existing pure string helpers in `src/lib/` (e.g.
`repo.ts`, `src/components/super-threads/utils.ts` `stripMention`).

**Test scenarios:**
- Happy path: `"## Title\n\n- one\n- two"` → `"Title one two"`.
- Happy path: `"See **bold** and `code` here"` → `"See bold and code here"`.
- Edge: link `"[docs](https://x.dev)"` → `"docs"`.
- Edge: fenced code block collapses to its inner text on a single line, no
  backticks.
- Edge: empty string → empty string; whitespace-only → empty string.
- Edge: plain text with no markdown passes through unchanged (modulo whitespace
  collapse).

**Verification:** `markdown-plain.test.ts` passes; a task whose reply is
multi-line markdown shows a clean single-line summary on the card.

---

## Risks & Mitigations

- **XSS via rendered content.** *Mitigation:* KTD2 — no `rehype-raw`, default HTML
  escaping, link-protocol allowlist, and explicit U2 tests asserting inert
  rendering of a script/`onerror` payload and rejection of `javascript:` links.
- **Bundle size from the highlighter.** *Mitigation:* KTD3 uses the Prism *async
  light* build with a curated language set rather than the full bundle; Shiki
  (heavier) is deferred.
- **Layout regressions** where prose spacing or wide `<pre>`/tables overflow the
  drawer or chat column. *Mitigation:* U4 scopes styles under `.md`, sets
  horizontal scroll on `pre`, and is verified visually against a
  construct-exercising reply.
- **Test-scope expansion** (adding jsdom). *Mitigation:* U1 keeps the config
  minimal and gates on all existing pure-logic suites staying green; justified by
  the untrusted-content safety requirement.

---

## Verification Strategy

- `npm test` — new pure-logic (`markdown-plain`) and jsdom component suites pass;
  all pre-existing suites stay green.
- `npx tsc -b --noEmit` — type-check clean.
- `npm run lint` — clean.
- Manual (`npm run dev`): paste the reported example reply into an agent turn and
  confirm headers, lists, bold, and a highlighted code fence render correctly in
  the thread drawer; confirm a `**bold**` / fenced-code human message renders in
  the chat column; confirm the compact card shows a clean single-line summary; and
  confirm tool-output/logs still render verbatim.
