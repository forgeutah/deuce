---
date: 2026-05-14
topic: changes-tab-vs-editor
focus: Replace the full file editor with a Changes tab; handle inline-edit ergonomics, chat+agent integration, and active-PR surface area
mode: repo-grounded
---

# Ideation: Changes Tab vs. Full File Editor

## Grounding Context

**Codebase context:**
- React 19 + Vite frontend, Go + Postgres + sqlc + chi backend, single Zustand store, WebSocket hub with per-session pub/sub, DevPod-per-session workspaces.
- Session tabs today: `chat | plan | files | terminal`. No Changes / diff / PR tab.
- `FilesView` (`src/components/FilesView.*`) is a read-only file tree with git status (`M | U | A | D`); no editor surface yet.
- `Message.ExpandableContent[]` in `src/types/index.ts` declares an unused `"diff"` variant — structural hook for inlining diffs into chat already present.
- `server/internal/handler/github.go` lists orgs/repos but has no PR or diff endpoints. No diff/PR types defined in the type system.
- WS hub already broadcasts `new_message`, `agent_status`, `typing_indicator`, `activity_update`, `session_update`, `unread_update`.
- Workspace files read directly from host FS at `${HOME}/.devpod/agent/contexts/<context>/workspaces/<workspace-id>/content/` (per `docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md`).
- STRATEGY.md Track 3 (Coding & Preview) explicitly lists in the backlog: "Diff viewer with inline chat on hunks", "GitHub PR integration (open, review, comment from inside)", "Agent-callable APIs for every human action". Agent-native parity is a **hard constraint**, not a stretch goal.
- Prior `docs/ideation/2026-05-13-vscode-workspace-entry-ideation.md` settled on Path B (Deuce-owned Monaco + Yjs collaborative editor pane). This ideation reconsiders that direction entirely.

**Past learnings (`docs/solutions/`):** Essentially greenfield on UI patterns — only one entry, on DevPod docker-workspace bind-mount, which is tangentially relevant (data-plane convention: prefer host-FS access over `devpod ssh` for any future diff endpoints).

**External context (prior art and signals):**
- **Cursor 3** (April 2026) ships dedicated Changes / Reviews / Commits tabs with the diff treated as "a mini PR review" alongside chat history. Kept full IDE as escape hatch. Direct competitive benchmark.
- **Devin / Cognition** ("Devin Review") reorganizes diff hunks by logical grouping rather than file order, color-codes severity, and embeds a live Ask-Devin chat inline within the PR review view.
- **Aider** treats the diff as the primary artifact: accept / reject / re-prompt, no inline edit.
- **Replit Agent** uses temporary diff panes that open from chat and auto-close on accept; chat doubles as the to-do list and audit log.
- **Sourcegraph Cody Smart Apply** renders one-click diffs in the editor gutter from chat code blocks — no separate diff tab.
- **ChatGPT Canvas / Claude Artifacts** treat the artifact as a first-class persistent object with version history; chat is the editorial layer.
- **Linear ↔ GitHub** surfaces PR status (CI, review state) on the work item without replacing GitHub's PR review UI — the right abstraction level for "we don't want to compete with IDEs."
- **Graphite** treats Changes as file-tree-based with stacked-diff awareness.
- **Hunk-level accept/reject** is the #1 missing-feature request across Claude Code, opencode, and Kilo Code (2025-2026 GitHub issues). Validated pain point.
- **Anti-patterns observed:** silent full-file rewrites (Copilot — trust erosion); discarding human edits on accept (Claude Code #6735); diff-tab-becoming-IDE scope creep; PR surface scope creep (GitHub Mobile); coordination tools that became editors (Replit).
- **Sourcegraph public stance (2025):** "the editor-sidebar agent is dead." Industry acknowledgment that the right coordination surface is not a sidebar in VS Code.

## Topic Axes

1. Diff presentation surface
2. Edit-the-diff ergonomics
3. Chat ↔ Diff coupling
4. PR surface area
5. Agent-action parity

## Ranked Ideas

### 1. Patch-stream as the universal session primitive
**Description:** A server-emitted `patch_event` becomes the single source of truth for every change in a session — agent edits, human edits, refactors, generated migrations. Each patch carries workspace SHA, parent patch ID, files touched, hunks, severity/grouping tags computed once at patch time, and the originating message ID. The Changes view, plan tab, PR composer, agent replay, time-travel scrubber, and notifications all subscribe to the same stream.
**Axis:** Diff presentation surface
**Basis:** `direct:` `server/internal/ws/hub.go` already does per-session pub/sub for `new_message`, `agent_status`, `activity_update`; `Message.ExpandableContent[]` already declares an unused `"diff"` variant in `src/types/index.ts`.
**Rationale:** Once a patch is a first-class event, Changes display, PR review, plan-step audit, undo, agent replay, multi-PR stacks, stale-diff detection (patches reference workspace SHA), and reviewer-attention routing all derive from it. Without this primitive, every new feature re-implements "what changed and when." It is also the cleanest answer to "how do agents and humans coordinate around code" — they coordinate around patch events.
**Downsides:** Heavier upfront design than a one-off Changes endpoint. Needs careful event-schema versioning. Requires a Postgres table for patches (small) and a new WS event type.
**Confidence:** 85%
**Complexity:** Medium-High
**Status:** Unexplored

### 2. The "Changes view" is a filtered chat view, not a sibling tab
**Description:** Diffs are first-class chat messages rendered via the existing `ExpandableContent` "diff" variant. The session timeline becomes the audit log: discussion, diff, decision, next diff — in order. A "Changes" entry in the tab bar exists, but it is a *saved filter* of the chat (patches + their threads), not a UI surface with its own state machine. The Files tab stays for "what does the repo look like now"; the Changes filter shows "what changed since branch base, with the conversation that produced it."
**Axis:** Diff presentation surface
**Basis:** `direct:` `Message.ExpandableContent[]` already declares an unused `"diff"` variant; the WebSocket hub already broadcasts per-session messages. `external:` Cody Smart Apply and Replit Agent both eschew dedicated diff tabs in favor of inline diffs in chat that close on accept.
**Rationale:** Directly answers "do we need a Changes tab?" — yes-ish, but not as a tab in the IDE-creep sense. Removes a whole surface from the editor-creep slippery slope, makes diffs reviewable in the same scroll as the reasoning that produced them, and reuses a type already declared in the codebase.
**Downsides:** Pure chat order can be hard to scan when many diffs land in a burst — the filter view needs grouping (by file, by intent, by hunk thread). Bursty agent work without good summarization will overwhelm the channel.
**Confidence:** 80%
**Complexity:** Medium
**Status:** Unexplored

### 3. Intent-only changes — humans never type into a diff
**Description:** Deuce never gives humans a textarea inside a diff. Two paths only: (a) reply to the hunk in chat with intent ("revert this hunk", "tighten this loop", "rename `foo` → `bar`") which spawns a follow-up agent patch that supersedes the original; or (b) click an "Open in VS Code / Cursor" deeplink (`vscode://file/{path}:{line}`) to edit locally — that edit becomes a patch event when picked up. Iteration history is visible in chat as `v1 → v2 → v3` supersession chains.
**Axis:** Edit-the-diff ergonomics
**Basis:** `direct:` user constraint "we don't want to compete with IDEs anyway"; `external:` Aider's diff-first re-prompt model; Sourcegraph's "the editor-sidebar agent is dead" (2025); Replit's slide from cloud editor → agent → trying to be a full IDE again.
**Rationale:** This is the load-bearing architectural refusal that defines Deuce as not-an-IDE. Every "just let me fix this small thing" capitulation is how coordination tools slide into being editors. Backing the refusal with a one-click escape hatch (local IDE) plus a productive alternative (re-prompt) makes it usable, not draconian. Also resolves "agents discard human edits on accept" (Claude Code #6735) by construction — there is no human-edited intermediate state to discard.
**Downsides:** Trivial polish ("just fix this typo") feels like more friction than it should. Re-prompt latency adds up. Some users resent the rule until they internalize the model.
**Confidence:** 75%
**Complexity:** Low (it is mostly *not* building something)
**Status:** Unexplored

### 4. Hunks own stable identity; anchored threads survive rebase
**Description:** Each hunk gets a stable hunk ID derived from `(file path, anchor lines, patch ID)`. Chat threads, agent comments, severity tags, and resolution status attach to that ID. When a later patch rewrites the hunk (re-prompt produces v2, rebase, force-push), the thread migrates by anchor with a visible "rebased onto patch N" marker. Hovering a hunk in the Changes filter scrolls chat to the thread; clicking a hunk reference in chat scrolls Changes to the hunk.
**Axis:** Chat ↔ Diff coupling
**Basis:** `direct:` per-session WS message stream + `ExpandableContent[]` make hunk-anchored threads a trivial extension. `external:` Devin Review and Cursor 3 both anchor inline chat to hunks; Graphite proves stacked-diff comments require this primitive to survive rebase.
**Rationale:** Directly answers "how do chat and diffs cohabit?" The unit of conversation is the hunk; the unit of mutation is the patch; threads survive across iterations so the reasoning trail compounds instead of being thrown away each rewrite. This is what makes re-prompting (idea #3) productive rather than amnesic.
**Downsides:** Anchor migration is a known-hard problem when hunks are heavily rewritten (the anchor lines no longer exist verbatim). Needs a graceful fallback to "orphaned thread, last known location."
**Confidence:** 75%
**Complexity:** Medium
**Status:** Unexplored

### 5. PRs as derived views — pinned session header + bidirectional review sync
**Description:** A session's PR is a derived projection of its patch stream onto a remote branch — pick a contiguous range of patches, push as commits, open or update a PR. Active PR state surfaces as a thin **pinned strip at the top of the session** showing title, draft/ready, CI status, review state, mergeable — clickable through to GitHub. PR review comments stream back as native chat messages in the same session (via webhook); replies from chat post back to GitHub. There is no separate "PR tab" — there is the session, and the session has a PR header.
**Axis:** PR surface area
**Basis:** `external:` Linear ↔ GitHub surfaces PR status on the work item without replacing GitHub's PR UI; Graphite shows stacked-diff value when commits are first-class derivations of patches. `direct:` user constraint "we don't want to compete with IDEs" extends naturally to "we don't want to compete with GitHub" for PR review mechanics.
**Rationale:** Answers "where do active PRs live?" without rebuilding GitHub. Reviewers on GitHub don't need a Deuce account; humans in Deuce don't need to leave to participate in the review. The "PR-as-derived" framing also makes multi-PR stacks and PR-per-step natural rather than special-cased.
**Downsides:** Webhook reliability matters — missed events mean stale strip state. Bidirectional comment attribution (`@bot` vs. real user) needs care. Approvals from GitHub need a clear visual on the strip without becoming GitHub Mobile.
**Confidence:** 80%
**Complexity:** Medium-High
**Status:** Unexplored

### 6. Every UI gesture is a tool call on the same event log (parity by build, not discipline)
**Description:** Define a small operation vocabulary — `propose_patch`, `apply_patch`, `revert_patch`, `comment_on_hunk`, `resolve_hunk`, `open_pr_from_range`, `push_to_pr`, `mark_pr_ready` — and make these the *only* way the UI mutates state. The UI is a thin client over the same event stream agents emit via MCP tools. A CI check fails the build if a new React mutation handler is added without a registered agent tool of the same name.
**Axis:** Agent-action parity
**Basis:** `direct:` STRATEGY.md Track 3: "Agent-callable APIs for every human action" and Track 3's "every collaborative surface here must be agent-callable" mandate.
**Rationale:** Parity-by-discipline always drifts. The pain isn't today — it's six months in when half the UI is agent-callable and half isn't, and nobody knows which. Baking the contract into the build makes "an agent reviewed the PR" and "a human reviewed the PR" structurally indistinguishable in form. Also unlocks agent-to-agent review loops without bespoke wiring.
**Downsides:** Up-front operation-schema design is real work. The CI lint requires AST-level static analysis or a simpler registry-file convention. Bigger commitment than just exposing endpoints ad-hoc.
**Confidence:** 80%
**Complexity:** Medium
**Status:** Unexplored

### 7. Plan steps and patches are linked both ways
**Description:** Each plan step declares an expected outcome — files, tests, behavior. When a patch event lands, it links back to the plan step that produced it; when all linked patches are applied and tests pass, the step auto-resolves. Humans see plan progress as diffs accumulate; the Changes filter can show "everything for step 3"; reverting a plan step reverts its patch group; PRs can be opened per-step for stacked-diff workflows.
**Axis:** Agent-action parity (compounding lever; also touches Diff-presentation)
**Basis:** `direct:` sessions already have a plan tab; STRATEGY Track 1 explicitly calls plan-tab the "how agents think and act as teammates" layer. `reasoned:` once patch events exist (#1), wiring plan steps to them is incremental.
**Rationale:** This is the compounding move that turns "agent did stuff" into "agent executed step 3, here are the 4 hunks." Without it, plans and diffs are two parallel records of the same work. With it, a session genuinely *is* one feature, decomposed into reviewable units that map cleanly onto reviewer attention.
**Downsides:** Requires plan steps to declare expected files/outcomes, which is more agent ceremony at plan creation. Some agents (and humans) will write vague plans that don't auto-resolve cleanly.
**Confidence:** 70%
**Complexity:** Medium
**Status:** Unexplored

## How the Survivors Answer the Original Questions

| Question | Answer |
|---|---|
| Replace the editor with a Changes tab? | Yes — but the "tab" is a *filter* of chat, not a separate surface (#2). The architecture beneath is a patch stream (#1). |
| Are changes editable? | **No textarea, ever.** Intent-only edits via re-prompt → supersession; deeplink to local IDE for direct text editing (#3). |
| How do chat + agents + diffs cohabit? | Diffs ARE chat messages with anchored threads on stable hunk IDs that survive rebase (#2 + #4). Refinement = chat reply → new patch. |
| How do active PRs surface? | Pinned header strip on the session showing CI / review / mergeable; PR review comments bidirectionally sync into the same session chat (#5). |

## Rejection Summary

48 raw candidates generated across 6 frames; 41 rejected, 7 survivors.

| # | Idea | Reason rejected |
|---|---|---|
| F1.1 | Hunk accept/reject as unit | Absorbed into #1 + #3 |
| F1.2 | Refuse hand-edit, deeplink to VS Code | Absorbed into #3 (deeplink is a UX detail of "intent-only") |
| F1.3 | Anchored hunk threads mirrored to chat | Absorbed into #4 |
| F1.4 | Active PRs as pinned header strip | Absorbed into #5 |
| F1.5 | Agent-parity gated in CI | Absorbed into #6 |
| F1.6 | Diff grouped by intent, not file | Subsumed by #1 (severity/grouping tags on patch events) |
| F1.7 | Stale-diff detection | Subsumed by #1 (patch references workspace SHA → staleness derivable) |
| F1.8 | "Refine this hunk" produces stacked v2 | Absorbed into #3 + #4 (supersession chain) |
| F2.1 | Kill the Changes tab; diffs are chat messages | IS #2 |
| F2.2 | No "Apply" button — agents apply, humans annotate | Strong inversion competing with #3 (propose-and-supersede). Defer to brainstorm — propose-first vs apply-first is a separate big call. |
| F2.3 | PRs open themselves on first green commit | Tactical; depends on #5 architecture. Defer to brainstorm. |
| F2.4 | Hunks regroup by intent | Subsumed by #1 + render layer |
| F2.5 | "Ask about this hunk" replaces inline editing | Absorbed into #3 |
| F2.6 | PR review thread IS session chat | Absorbed into #5 (bidirectional sync) |
| F2.7 | Every diff surface emits agent transcript | Duplicates #6 |
| F2.8 | Brainstorm/design sessions never produce diffs (session-type) | Session-type filter — useful product decision but separable. Defer. |
| F3.1 | Sessions ARE PRs (collapse two concepts) | Maximalist #5; bigger commitment than the question warrants. Defer. |
| F3.2 | Intent ledger, not diff viewer | Maximalist #2 (no rendered diffs at all). Defer. |
| F3.3 | Predicted diffs before the agent runs | Novel reframe (review as planning). Doesn't compose with the other 7 — worth a separate brainstorm. |
| F3.4 | No edit-the-diff, edit by re-prompt or GitHub | Duplicates #3 |
| F3.5 | Diff isn't text — rendered behavior delta | Design/UI-session-specific. Defer to design-session brainstorm. |
| F3.6 | PR widget as session participant | Duplicates #5 |
| F3.7 | Drop tabs, adopt phases | Scope overrun — touches whole product organization, not the asked surface |
| F3.8 | Every diff interaction is agent-callable | Duplicates #6 |
| F4.1 | Patch-stream as universal session primitive | IS #1 |
| F4.2 | Hunks own a thread (stable hunk IDs) | Absorbed into #4 |
| F4.3 | Proposed vs. Applied two-state Changes tab | UX shape; partially absorbed into #2 + #3. Defer to brainstorm. |
| F4.4 | Plan steps emit patches; patches close plan steps | IS #7 |
| F4.5 | PR as derived view | Absorbed into #5 |
| F4.6 | Every human gesture is a tool call | IS #6 |
| F4.7 | Hunk-level severity/grouping computed once | Subsumed by #1 |
| F4.8 | Time-travel scrubber on patch stream | Derived from #1 — nice-to-have, not foundational. Defer. |
| F5.1 | Andon Cord on the Hunk | Novel; behavior layer on #1. Defer to feature brainstorm. |
| F5.2 | Differential Diagnosis Strip | Adds hypothesis concept; scope-adjacent. Defer. |
| F5.3 | Quilt Stack as Changes Tab (LKML patch series) | Strong alt take on edit-the-diff (reorder patches vs. re-prompt). Defer to brainstorm — competes with #3. |
| F5.4 | Avid Bin for Takes | Preserves rejected variants; below ambition floor for survivors |
| F5.5 | RFI on the Redline | Typed gating questions; below ambition floor. Defer. |
| F5.6 | Ticket Rail Fire/Hold | Manual stage-gating contradicts agent-native auto-flow |
| F5.7 | ECO with Red Tag | Hardware ceremony without clear advantage over supersession chain |
| F5.8 | Slug + Budget Meeting | Solves session self-description, not the asked question |
| F6.1 | Single-Line Diff Reel | Subsumed by #2 + grouping primitive in #1 |
| F6.2 | Intent-Only Changes (no hand-edits) | IS #3 |
| F6.3 | Hunk-Threaded Chat (diffs in messages) | Absorbed into #2 |
| F6.4 | PR is the Session Header | Absorbed into #5 |
| F6.5 | Agent-Reviewed Auto-Merge | Overlaps with #6 + F2.2; not standalone-defining |
| F6.6 | Voice-Readable Change Digest | Scope-adjacent — tangential to core question |
| F6.7 | No-Diff Mode (goal + final state) | Variant of F3.2; defer |
| F6.8 | PR-Per-Hunk Stacked-by-Default | Maximalist #5 (derived view); defer to brainstorm |
