# Handoff: Super Threads — @agent threads with an anchored, auto-promoting queue

## Overview

**Super Threads** is a new interaction model for Deuce sessions. When a human `@mentions` an
AI agent in a session channel, that mention spawns an **inline task card anchored right where the
message was posted**. The card shows the agent working in real time. All of an agent's tasks belong
to **one continuous, global thread** for that agent in that session — opening any card opens the same
thread, scrolled through the full history.

Two behaviors are the heart of the feature:

1. **Anchored, per-mention cards.** Each `@mention` of an agent creates its own card, placed
   immediately after the triggering message. A card moves through three states — **running →
   done**, or **queued → running → done**.

2. **Per-agent serial queue with auto-promotion.** An agent runs **one task at a time**. If the
   agent is busy when a new `@mention` arrives, that mention's card shows a **Queued · #N** state.
   When the running task finishes, its card **shrinks and fades to a compact "done" chip** and the
   next queued card **expands into the active (running) card** automatically — no user action.

3. **Claude Code–style action log.** Inside the thread, each task streams the concrete actions the
   agent takes — `Read`, `Grep`, `Edit`, `Write`, `Bash`, plus `Think` steps — with inline diffs and
   command output, revealed live as the task runs. Completed tasks collapse to an "N actions"
   summary that expands on click.

---

## About the design files

The files in this bundle are **design references created in HTML/CSS/React-via-Babel** — a working,
clickable prototype that demonstrates the intended look and behavior. **They are not production code
to copy directly.**

Your task is to **recreate this design in Deuce's real codebase** (`forgeutah/deuce` — React +
TypeScript + Tailwind + shadcn/ui), using its established components, hooks, and data layer. The
prototype hard-codes a simulated agent and a `setTimeout`-driven state machine purely to make the
interaction visible; in the real app, the same states should be driven by your actual agent-run
lifecycle (run started / tool call / run completed) over your existing transport (WebSocket, SSE,
polling — whatever the session channel already uses).

The prototype is built on the **real Deuce design tokens** (Primer-dark), so colors, type, spacing,
and radii in the files are accurate and can be used verbatim via the existing CSS variables.

## Fidelity

**High-fidelity.** Colors, typography, spacing, border treatments, badges, and motion are all final
and grounded in the Deuce design system. Recreate the UI to match, using Deuce's existing Tailwind
tokens / `globals.css` variables and shadcn primitives. Where the prototype uses a raw class, map it
to the equivalent existing component (e.g. the channel message row, the right-hand drawer/sheet,
badges, the textarea composer).

---

## Core data model

The prototype's state shape (translate to your real types):

```ts
type AgentId = 'coder' | 'reviewer' | /* …whatever roles exist */;
type TaskState = 'queued' | 'running' | 'done';   // (add 'failed' / 'cancelled' for production)

interface AgentTask {
  id: string;
  agentId: AgentId;
  requestedBy: User;        // who @mentioned
  anchorMessageId: string;  // the channel message this card is pinned under
  prompt: string;           // the full message text (the @mention is highlighted, not stripped)
  state: TaskState;
  ts: string;               // display time; set when state last changed
  startedAt?: number;       // epoch ms when it entered 'running' — drives the live action reveal
  actions: AgentAction[];   // the action stream (see below)
  reply?: string;           // agent's final summary line, shown when done
  work?: { file: string; stat: string } | null; // headline diff, e.g. { file:'validate.go', stat:'+12 −3' }
}

interface AgentAction {
  tool: 'Read' | 'Grep' | 'Edit' | 'Write' | 'Bash' | 'Think';
  arg?: string;             // path or command, e.g. 'internal/auth/validate.go'
  note?: string;            // optional trailing note, e.g. '2 matches'
  text?: string;            // for Think steps — the reasoning line
  stat?: string;            // for Edit/Write — diff stat
  diff?: DiffLine[];        // for Edit/Write — shown when the action completes
  out?: OutputLine[];       // for Bash — command output, shown when complete
  ms: number;               // prototype-only: simulated duration; real app uses live events
}
```

**Key relationships**
- A **session** has N **agents**; each agent owns an **ordered list of tasks** (its global thread).
- Each task is **anchored** to the channel message that created it (`anchorMessageId`).
- The channel renders, after each message, any task cards whose `anchorMessageId` matches.
- Queue position is derived: walk an agent's task list in order, numbering only `queued` tasks.

---

## The state machine (must-match behavior)

This is the part to get exactly right.

1. **New mention → enqueue.** Parse `@mentions` from a sent message. For each mentioned agent,
   append a task in state `queued` to that agent's list, with `anchorMessageId` = the new message.
2. **Promote-in-place.** Immediately after enqueue (and again after any completion), run a promotion
   pass per agent: *if the agent has no `running` task, promote its first `queued` task to `running`*
   (set `startedAt`, attach/begin its action stream). This makes the very first mention start
   instantly, while a mention arriving during a run stays `queued`.
3. **Completion is atomic with promotion.** When a running task finishes, in **one state update**:
   mark it `done` (attach `reply` + `work`) **and** promote the next queued task. This guarantees the
   UI never shows an agent simultaneously **Idle** and with a **Queued** item — a bug we explicitly
   fixed. Do not split these into two updates/renders.
4. **One running task per agent**, always. Multiple agents can run concurrently (Coder and Reviewer
   independently), but within a single agent it is strictly serial.

---

## Screens / Views

There is one screen — the **session Chat tab** — plus a right-hand **agent thread drawer**.

### 1. Session shell (unchanged Deuce three-pane)
- **Left:** session sidebar (existing).
- **Center:** session header (`# auth-module` + one-line description), tab bar (Chat / Plan / Files /
  Terminal), then the scrolling channel, then the composer.
- **Right:** normally the participants/activity rail; when a thread is open, the **agent thread
  drawer** takes this side (see §4). The prototype shows the drawer as a 412px right panel with a
  3px agent-colored top+left accent and a left drop shadow.

### 2. Channel message + anchored cards
- Standard Deuce message row: 24px round DiceBear avatar, name (13px/600), timestamp (11px,
  `--fg-subtle`), body (13px/1.5). `@mentions` in body are rendered in the **mentioned agent's
  color**, weight 600.
- **Directly beneath a message**, render its anchored task card(s), indented (`margin-left: 52px`)
  so they visually hang off the message.

### 3. The anchored task card (`.tc`) — three states

All cards: `border: 1px solid --border-muted`, `border-left: 3px solid <agent color>`,
`border-radius: --radius-md (6px)`, background `color-mix(in srgb, <agent> 7%, --bg)`, clickable
(opens the agent thread). The agent color is passed as a CSS var `--ac`.

**a) Running (full card)**
- Header row: agent avatar chip (22px, agent-colored, white initial), agent name (13/600),
  "· session thread" (11px subtle), right-aligned **Working** badge (uppercase 10px, agent color on
  `<agent> 16%` tint, with a spinning `Loader` icon), and a `ChevronRight`.
- **Live tool line** (mono, 11px): a pulsing 7px agent-colored dot + the current tool name (agent
  color, 600) + its argument (path/command). Updates as the action stream advances.
- **Typing row:** three staggered agent-colored typing dots + "<Agent> is working — open to watch".

**b) Queued (compact, amber)**
- `border-left-color: --warning`; background `color-mix(in srgb, --warning 6%, --bg)`.
- Row: dimmed agent avatar (22px) · two lines — L1 (11px/600, `--warning`) with a `Clock` icon:
  "Queued for <Agent> · waiting for current task"; L2 (12.5px, `--fg`) the request text with the
  `@mention` stripped — truncated with ellipsis · right-aligned **#N** position pill (mono 10px/700,
  warning on `--warning-muted`).

**c) Done (shrunk chip)**
- The card transitions to `opacity: .5; transform: scale(.985)`; background goes transparent;
  left border dims to `color-mix(in srgb, <agent> 45%, --border)`. Hover raises opacity to ~.9.
- Single compact row: 18px agent avatar · green `Check` icon · "**<Agent>** <reply line>"
  (12px `--fg-muted`, name in `--fg`) · optional diff stat (mono 10px, `--success`) · `ChevronRight`.
- **Transition:** the shrink + fade is a **CSS transition** on `opacity`/`transform`/`background`
  (~.35s ease), triggered by the state class swap. (Note: in the prototype, CSS `@keyframes`
  entrance animations with `both`/`forwards` fill-mode mis-rendered as `opacity:0`; we rely on plain
  transitions instead. Your real components can use whatever your codebase already uses for enter/exit
  — Framer Motion, CSS transitions — just don't ship an entrance keyframe that can settle to hidden.)

### 4. Agent thread drawer (the global thread)
- **Header:** agent avatar (26px), name (14/600), sub-line (11px agent color): a small dot +
  "Working · global thread" when a task is running, else "Idle · global thread". Right: close (X).
- **Body** (scrolls, auto-sticks to bottom): the agent's tasks in order, each rendered as a **turn**:
  - **Request block:** requester avatar + name + time, then the prompt (with `@mention` highlighted).
  - **If running:** a typing line ("<Agent> is working…") followed by the **live action log** —
    actions reveal one at a time; the in-flight action shows a spinning `Loader2`, completed ones a
    green `Check`. `Edit`/`Write` actions expand to a diff card; `Bash` actions expand to an output
    block; `Think` actions render italic with a `Sparkles` icon.
  - **If done:** an agent line (avatar+name+time), a collapsible **"N actions"** summary row
    (mono, with a chevron and the headline `file +x −y`); clicking expands the full action log; then
    the agent's **reply** line.
  - **If queued:** a dashed amber "Queued · position N" card explaining it starts automatically.
- **Footer divider** at the very top of the list: "Start of thread with <Agent>".
- **Composer** at the bottom of the drawer: a textarea ("Reply to <Agent>…") + send button, tinted
  with the agent color. Sending posts a message to the channel **and** enqueues another task for that
  agent (same enqueue/promote path).

### 5. Channel composer
- A "Mention an agent:" row of color-filled chips (one per agent) that insert `@<Agent> ` into the
  input. Below: an auto-growing textarea (Enter sends, Shift+Enter newline) + a send button
  (`--accent-emphasis`, disabled when empty).
- A **"Reset demo"** affordance (top-right of the center pane) re-seeds state — **prototype only**,
  drop it in production.

---

## Interactions & behavior

- **Send in channel:** append message → parse mentions → enqueue a task per mentioned agent →
  promote-in-place → open that agent's thread drawer.
- **Click any card (running/queued/done):** open the owning agent's thread drawer (full history).
- **Send in drawer:** append a channel message + enqueue another task for that agent.
- **Live reveal:** while a task is `running`, the visible action index is derived from
  `now − startedAt` against cumulative action durations (prototype). In production, advance the log
  on real tool-call events instead of a timer.
- **Auto-promotion:** on completion, the finished card animates to the done chip and the next queued
  card becomes the running card in the same update (see state machine §3).
- **Auto-scroll:** channel sticks to bottom on new messages; drawer body sticks to bottom as actions
  stream and on state changes.

## Motion (per Deuce)
- Card state changes: ~0.35s ease transitions on opacity/transform/background.
- Working indicators: `pulse` (2s) on the live dot; staggered `typing-dots` (1.4s); `spin` (1.1s
  linear) on `Loader`/`Loader2`. No bounces, no scale-on-press. Keep it fast and functional.

## State management
- Per session: `threads: Record<AgentId, AgentTask[]>` and the channel message list.
- `openAgentId: AgentId | null` controls the drawer.
- Derived each render: queue positions (number the `queued` tasks per agent) and an
  `anchorMessageId → tasks[]` map for inline placement.
- Production data fetching: subscribe to your agent-run events; map `run.started`→promote,
  `tool.call`/`tool.result`→append/advance action, `run.completed`→complete+promote.

---

## Design tokens (all already in Deuce `globals.css`)

Use the existing CSS variables — do not hard-code. Reference values:

| Token | Value | Use |
| --- | --- | --- |
| `--bg` | `#0D1117` | canvas |
| `--bg-subtle` | `#151B23` | rails, work-block headers |
| `--bg-inset` | `#010409` | diff/output wells |
| `--bg-input` | input bg | composer textarea |
| `--fg` / `--fg-emphasis` / `--fg-muted` / `--fg-subtle` | text scale | body / names / secondary / meta |
| `--border` / `--border-muted` | `#3D444D` / `#2F3742` | separators, card borders |
| `--accent` / `--accent-emphasis` | `#58a6ff` / `#1f6feb` | links, primary send button |
| `--success` / `--success-muted` | green | done checks, Idle badge, passing tests |
| `--warning` / `--warning-muted` | yellow | queued state, #N pill |
| `--agent-coder` | `#58a6ff` | Coder identity (= `--ac` for Coder) |
| `--agent-reviewer` (purple) | `#BE8FFF` | Reviewer identity |
| `--agent-planner` / `--agent-tester` / `--agent-designer` | green / `#d29922` / `#f778ba` | other roles |
| `--radius-sm/md/lg/full` | 4 / 6 / 8 / 9999px | 6px default; full for dots/pills/avatars |
| `--font-sans` | system stack | UI text (no webfont) |
| `--font-mono` | `ui-monospace, SF Mono…` | tool lines, diffs, output, stats |
| `--shadow-overlay` | near-black | the thread drawer's shadow |

**Agent-color pattern:** each agent has a base color; the muted/tint surfaces are
`color-mix(in srgb, <color> X%, …)` — message tint ~6–8%, working-badge ~16%, hover ring ~30%.
The prototype passes the agent color down as `--ac` and derives everything from it; replicate that so
adding a new agent role only needs its base color.

**Type scale used:** 14px root; body 13px; meta 11px; eyebrows/badges 10px (uppercase, letter-spaced);
weights 400/500/600 only. Tool lines/diffs/output in mono at 11–12px.

## Iconography (Lucide, already in Deuce)
`Loader`/`Loader2` (working/spinner), `Clock` (queued), `Check` (done), `ChevronRight`/`ChevronDown`
(disclosure), `Hash`, `MessageSquare`, `FileText`, `FolderTree`, `Terminal`, `SendHorizontal`,
`X`, `RotateCcw` (reset, demo only). Action-log tool icons: `FileText`=Read, `Search`=Grep,
`Pencil`=Edit, `FilePlus`=Write, `SquareTerminal`=Bash, `Sparkles`=Think.

## Assets
- **Deuce logo:** `assets/deuce-logo.png` (sidebar brand). Use the existing app asset in your codebase.
- **Human avatars:** DiceBear `avataaars` by seed (`Clint`, `Sarah`, `Mike`) — placeholders; use real
  user avatars in production.
- No other imagery (per Deuce — the data is the visual interest).

---

## Files in this bundle

| File | What it is |
| --- | --- |
| `Super Threads — Queue.html` | **The main reference prototype.** Open in a browser. The full interaction: anchored cards, queue + auto-promotion, live action log, thread drawer. Try the `@Coder` chip while a task is running. |
| `queue-app.jsx` | The prototype's app logic — state machine, enqueue/promote/complete, action streams, card + drawer + action-log components. **Read this for exact behavior.** |
| `queue.css` | All Super-Threads-specific styling (cards, states, badges, action log). Built on the Deuce tokens. |
| `deuce-shell.jsx` | Shared Deuce shell atoms used by the prototype (sidebar, avatars, icon helper). |
| `super-thread.css` | Earlier shared styles some classes still reference. |
| `deuce-base.css` | A copy of the Deuce app stylesheet (the token definitions) so the prototype runs standalone. In your codebase these tokens already exist — don't re-add them. |
| `Super Threads.html` | The earlier **concept canvas** — collapsed-card treatments and four focused-thread layouts (Drawer / Split / Overlay / Takeover) plus a novel "tune-in" metaphor. Context for why the Drawer layout was chosen; useful if you want to revisit layout. |

**Where to start:** open `Super Threads — Queue.html`, interact with it, then read `queue-app.jsx`
top-to-bottom — the state machine (`enqueue` / `promoteInPlace` / `complete`), the `actionsFor()`
streams, and the `TaskCard` / `Turn` / `ActionLog` components are the implementation spec.

## Notes / open decisions for production
- Add `failed` and `cancelled` task states (the prototype only models the happy path).
- Consider **queue controls** (cancel a queued task, reorder, "run next") — designed-for but not yet
  built.
- Decide persistence: tasks/threads should survive reload and be shared across all session members in
  real time.
- The action log should stream from real agent tool-call events, not a timer.

---

## Backend contract: the agent-run event stream (NOT YET BUILT)

> Deuce does **not** have an agent-run event stream yet. This section specifies what the frontend
> needs the backend to emit so the action log and live states work for real. It's written so the
> frontend can be built **now** against a faked emitter and swapped to the real one later with no UI
> changes. **Build to this interface, not to the prototype's `setTimeout` timers.**

### What the UI needs (and nothing more)
The frontend is a pure function of a task list plus a stream of incremental updates. To drive every
state in this design, the backend needs to tell the client four things over the life of a run:
**a run was requested, a run started, the agent took a step, the run finished.** That's it.

### Event shape
One channel-scoped, ordered, append-only event stream per session (reuse whatever the Chat tab
already uses for live messages — WebSocket / SSE). Every event carries `sessionId`, `taskId`,
`agentId`, and a monotonic `seq` (for ordering + gap detection on reconnect).

```ts
type AgentRunEvent =
  // 1. A mention created a task. Emit immediately on message send so the QUEUED card can render
  //    before the agent ever starts. (Frontend can also create this optimistically; the server
  //    event is the source of truth / reconciles it.)
  | { type: 'task.enqueued'; sessionId; taskId; agentId; seq;
      requestedBy: UserRef; anchorMessageId: string; prompt: string; ts: string }

  // 2. The agent picked the task up. This is the QUEUED → RUNNING transition (your scheduler decides
  //    when, enforcing one-running-per-agent). Frontend flips the card to the working state.
  | { type: 'task.started'; sessionId; taskId; agentId; seq; ts: string }

  // 3. One action. Emit 'started' when a tool call begins (drives the spinner + live tool line),
  //    then 'completed' with the payload (diff/output) when it returns. 'Think' steps can emit a
  //    single event. action.id ties the two halves together.
  | { type: 'action.started';   sessionId; taskId; agentId; seq;
      action: { id: string; tool: ToolName; arg?: string; note?: string; text?: string } }
  | { type: 'action.completed'; sessionId; taskId; agentId; seq;
      actionId: string; stat?: string; diff?: DiffLine[]; out?: OutputLine[] }

  // 4. The run finished. RUNNING → DONE. Triggers the shrink-to-chip + auto-promote of the next
  //    queued task. Use status:'failed' to drive the (to-be-added) failed state.
  | { type: 'task.completed'; sessionId; taskId; agentId; seq;
      status: 'done' | 'failed'; reply: string;
      work?: { file: string; stat: string } | null; ts: string };

type ToolName = 'Read' | 'Grep' | 'Edit' | 'Write' | 'Bash' | 'Think';
```

### How each event maps to the UI
| Event | UI effect |
| --- | --- |
| `task.enqueued` | Insert a task (`state:'queued'`) under `anchorMessageId`; show **Queued · #N** card. |
| `task.started` | `queued → running`; set `startedAt`; card flips to the working card; thread shows typing line. |
| `action.started` | Append an action; spinner on it; update the card's live tool line. |
| `action.completed` | Resolve that action to ✓; render its diff card / output block. |
| `task.completed` | `running → done`; card shrinks/fades to the done chip; **promote next queued task** (the scheduler should also emit that task's `task.started`). |

### Reconnect / late join
A member opening the session mid-run needs the current truth, not just future deltas. Provide a
**snapshot** read (REST or first frame on subscribe) returning each agent's task list **including the
actions already taken** for any in-flight task, plus the latest `seq`. The client renders the
snapshot, then applies events with `seq >` snapshot. This is also what makes threads survive reload.

### Who owns the queue?
**The server scheduler**, not the client. The client may *optimistically* show a card as queued the
instant a message is sent, but the authoritative "one running per agent, promote on completion" logic
must live server-side so every session member — and every agent worker — agrees. The client's
`promoteInPlace` in the prototype is a stand-in for that server scheduler.

### Phased delivery (lets you ship UI before the stream exists)
1. **Phase 1 — UI on a fake emitter.** Build all components + the reducer that consumes
   `AgentRunEvent`. Drive it from a local mock that emits the events on timers (essentially what the
   prototype does, but already shaped as real events). Ships the entire visual feature.
2. **Phase 2 — real lifecycle, coarse.** Wire `task.enqueued` / `task.started` / `task.completed`
   from your actual agent runner. No per-action streaming yet → the thread shows a simple
   "working…" turn that resolves to the reply. Queue + anchoring + auto-promotion are fully real.
3. **Phase 3 — per-action streaming.** Add `action.started` / `action.completed` from the agent's
   tool-call hooks to light up the Claude Code–style log live.

Because all three phases consume the **same event type**, the UI from Phase 1 is the shipping UI —
later phases just replace the event *source*.
