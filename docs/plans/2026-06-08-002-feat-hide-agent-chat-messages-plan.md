---
title: "feat: Hide agent replies from main chat, surface them only in super thread cards"
type: feat
status: completed
date: 2026-06-08
depth: lightweight
---

# feat: Hide agent replies from main chat, surface them only in super thread cards

## Summary

Agent task replies currently render **twice**: as a `MessageBubble` in the main chat list *and* as the reply text in the inline super-thread card (`AgentTaskCard`). Both come from the same `reply` string emitted at task finalization. This plan hides the chat-bubble copy so agent replies live only on the thread surfaces (the inline card and the agent thread drawer), while leaving everything else in chat — human messages and system/operational notices — untouched.

This is a **display-only** change. Agent messages remain persisted (they feed the agent's own conversation-history context) and keep flowing over WebSocket; they are simply filtered out of the rendered chat list.

---

## Problem Frame

When a user @mentions an agent, the agent's final reply is surfaced in two places at once:

1. **Inline super-thread card** — `task.reply` shown in [`AgentTaskCard`](src/components/super-threads/AgentTaskCard.tsx#L100-L125) under the triggering message, and in the drawer `Turn` ([AgentThreadDrawer.tsx:108-138](src/components/super-threads/AgentThreadDrawer.tsx#L108-L138)).
2. **A full chat `MessageBubble`** — posted as a normal `authorType: "agent"` chat message and rendered in [`ChatView`](src/components/chat/ChatView.tsx#L422-L467).

Both originate from the *same* `reply` string in [`runtime.go:406-419`](server/internal/agent/runtime.go#L406-L419): the `task_completed` event carries it into `task.reply`, and `replyPoster` posts it as the chat message. The duplication clutters the chat and undercuts the super-thread surface as the place to follow agent work.

**Desired behavior:** agent replies do not appear in the chat outside of threads. The last reply is shown in the super-thread card (which it already is). Human messages and system notices stay in chat.

---

## Scope Boundaries

**In scope**
- Filter real agent replies out of the rendered main chat list in `ChatView`.
- Keep human messages and system/operational notices (e.g. "Workspace is still starting", "Agent queue is full") visible.
- Preserve inline `AgentTaskCard` rendering, message-header grouping, empty-state, and auto-scroll behavior.

**Out of scope / non-goals**
- No change to message persistence, the WebSocket broadcast, or the backend reply-posting path. Agent messages must remain in the DB and the store because `buildChatHistory` ([messages.go](server/internal/handler/messages.go)) uses them for agent context continuity.
- No change to `AgentTaskCard` / `AgentThreadDrawer` reply rendering — they already show `task.reply`.

### Deferred to Follow-Up Work
- **Unread-count behavior for hidden agent replies.** `addMessage` ([session-store.ts:202-225](src/stores/session-store.ts#L202-L225)) still bumps the session unread count when a (now hidden) agent reply arrives. This is arguably correct — the card *is* new content — but if the team wants unread to track only visible chat, that's a separate, intentional change.

---

## Key Technical Decisions

**KTD1 — Frontend display filter, not a backend/store change.**
The agent reply and the card's `task.reply` are the *same string* posted together at finalization, so hiding the chat copy loses no information. Backend or store-level removal would break `buildChatHistory` context continuity and the empty-reply fallback ("(The agent finished without a text response.)"). A render-time filter in `ChatView` is the minimal, reversible change. *(see [runtime.go:406-419](server/internal/agent/runtime.go#L406-L419))*

**KTD2 — Distinguish agent replies from system notices by author identity, not just `authorType`.**
`postSystemMessage` ([messages.go:297-313](server/internal/handler/messages.go#L297-L313)) posts operational notices with `authorType: "agent"` but a **nil author ID** that matches no session agent. Per the product decision, those notices stay visible. The filter therefore hides a message only when it is `authorType === "agent"` **and** its `authorId` matches a real agent in `session.agents`. Nil-author system notices fall through and remain in chat. This reuses the same author-resolution that `ChatView` already performs.

**KTD3 — Extract the predicate as a pure, tested function.**
Per the repo convention ([repo.test.ts](src/lib/repo.test.ts) — Vitest-style specs that type-check today and run once a runner is wired), the visibility decision lives in a small pure module with a colocated spec, rather than as an inline `.filter` arrow that can't be tested in isolation.

---

## Implementation Units

### U1. Pure chat-message visibility predicate

**Goal:** A pure, testable function that decides whether a message renders in the main chat list — hiding real agent replies while keeping human messages and system notices.

**Requirements:** Core behavior of the feature; backs KTD2 and KTD3.

**Dependencies:** none.

**Files:**
- `src/components/chat/message-visibility.ts` (new)
- `src/components/chat/message-visibility.test.ts` (new)

**Approach:**
- Export a predicate, e.g. `isVisibleInChat(message, agentIds: Set<string>): boolean`, plus a thin `visibleChatMessages(messages, agentIds)` helper that filters while preserving order.
- A message is hidden iff `message.authorType === "agent" && agentIds.has(message.authorId)`. Everything else (human messages, and `agent`-typed messages whose author is not a session agent — i.e. nil-author system notices) is visible.
- `agentIds` is derived by the caller from `session.agents` (a `Set` of agent IDs). Keep the module free of store/session imports so it stays pure.

**Patterns to follow:** Pure selector style of [`src/stores/agent-runs.ts`](src/stores/agent-runs.ts#L198-L227); spec style and the "no runner yet" note from [`src/lib/repo.test.ts`](src/lib/repo.test.ts).

**Test scenarios:**
- Human message (`authorType: "human"`) → visible, regardless of `agentIds` contents.
- Agent reply whose `authorId` is in `agentIds` → hidden.
- System notice (`authorType: "agent"`, `authorId` = nil UUID, not in `agentIds`) → visible.
- Agent-typed message whose `authorId` is not in `agentIds` (defensive: agent removed from session) → visible.
- Empty `agentIds` set → no agent messages are hidden (all visible).
- `visibleChatMessages` on a mixed array → returns only visible messages in original order, drops hidden ones, does not mutate the input.

**Verification:** Spec cases above pass under `vitest` when a runner is present; `npx tsc --noEmit` clean today.

---

### U2. Apply the filter in ChatView

**Goal:** Render the chat list from the filtered message set so agent replies no longer appear as bubbles, while inline task cards, header grouping, empty-state, and scrolling stay correct.

**Requirements:** Delivers the user-visible outcome.

**Dependencies:** U1.

**Files:**
- `src/components/chat/ChatView.tsx`

**Approach:**
- Build `agentIds` once from `session.agents` and derive `visibleMessages = visibleChatMessages(sessionMessages, agentIds)`.
- Drive the message `.map` and the `prevMsg`/`showHeader` grouping ([ChatView.tsx:422-467](src/components/chat/ChatView.tsx#L422-L467)) off `visibleMessages`, so author-change/time-gap headers are computed against the *visible* sequence (no phantom gaps from removed agent bubbles).
- **Keep `cardsByAnchor` and inline `AgentTaskCard` rendering unchanged** — cards are keyed on the *human* anchor message (`anchorMessageId`), which is never filtered, so cards remain anchored under the @mention that spawned them.
- Empty-state check ([ChatView.tsx:400](src/components/chat/ChatView.tsx#L400)) uses `visibleMessages.length`.
- **Auto-scroll:** keep the scroll effect ([ChatView.tsx:335-339](src/components/chat/ChatView.tsx#L335-L339)) triggered on the *raw* `sessionMessages.length` (not `visibleMessages.length`) so an arriving agent reply still scrolls the freshly-updated card into view even though the bubble is hidden.

**Patterns to follow:** existing `sessionMessages` derivation and the author-lookup already in [ChatView.tsx:431-436](src/components/chat/ChatView.tsx#L431-L436).

**Test expectation:** none at the unit level — `ChatView` has no component-test harness. Covered by U1's pure-function tests plus the manual verification below.

**Verification (manual / visual):**
- @mention an agent → the agent's reply no longer appears as a chat bubble; the inline super-thread card under the @mention shows the reply text in its done state.
- A system notice path (e.g. send an agent request while the workspace is starting) → the "Workspace is still starting" notice **still appears** in chat.
- Consecutive human messages still collapse headers correctly (no stray avatars/timestamps from removed agent bubbles).
- A session whose only agent output is hidden still renders its human messages and cards; empty-state shows only when there are genuinely no visible messages.
- A drawer-initiated (anchorless) task still shows its reply in the agent thread drawer `Turn` — this is the intended "inside the thread" surface for tasks with no inline card.
- `npm run lint` and `npx tsc --noEmit` clean.

---

## Verification Strategy

1. `npx tsc --noEmit` — type safety across the new module and `ChatView` edits.
2. `npm run lint`.
3. U1 spec cases (run under vitest if/when the runner is wired; type-checked now).
4. Manual walkthrough of the U2 verification scenarios against a running dev server (`npm run dev` + backend `make dev`).
