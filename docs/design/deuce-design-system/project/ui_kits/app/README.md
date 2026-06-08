# Deuce UI Kit — Workspace

A high-fidelity, interactive recreation of the **Deuce** workspace — the one product surface in the codebase. Open `index.html` and you land in a live session: switch sessions, read the human + agent chat, @mention an agent and watch it "work" and reply, browse the plan/files/terminal tabs, and spin up a new session.

It is a **cosmetic recreation**, not the real app: state is in-memory React, agent replies are canned with a delay (mirroring the real server's simulated-agent behavior), and there's no backend, websocket, or DevPod. Visuals and interaction patterns are lifted from the real source (`forgeutah/deuce`, `src/components/*` + `src/styles/globals.css`).

## Run
Just open `index.html`. It loads React 18 + Babel standalone (inline JSX) and Lucide icons from CDN — no build step.

## Files
- **`index.html`** — entry. Loads React/Babel/Lucide, then the scripts below, and mounts `<App>`.
- **`styles.css`** — the whole design system as plain CSS (Primer-dark tokens, layout, component styles). Mirrors the app's Tailwind-v4 token set.
- **`data.js`** — seed data (`window.DEUCE`): users, agents, projects, sessions, messages, activities, file tree + contents, terminal/log lines. Mirrors `src/mocks/data/seed.ts`.
- **`icon.jsx`** — `<Icon name>` (renders Lucide inline SVG) + `relTime`/`clockTime`/`renderContent` helpers.
- **`panels.jsx`** — `Sidebar` (session list, project groups, search, footer nav), `SummaryPanel` (participants + activity rail), `CreateSessionDialog`.
- **`center.jsx`** — `CenterPanel` shell + the four tabs: `ChatView` (messages, expandable diffs, typing indicator, composer), `PlanView` (editor/split/preview markdown), `FilesView` (tree + syntax-highlighted viewer), `TerminalView`, `LogsView`.
- **`app.jsx`** — `App` root: state, session switching, message send + simulated agent reply, plan editing, session creation.

## Components worth reusing
`Sidebar`, `SessionCard`, `ProjectGroup`, `SummaryPanel`, `AgentRow`, `UserRow`, `CreateSessionDialog`, `CenterPanel`, `ChatView`, `Message`, `Expandable`, `TypingIndicator`, `PlanView`, `FilesView`, `TerminalView`, `LogsView`, `Icon`.

## Layout
Three panes: **sidebar** (`bg-subtle`, session list) · **center** (`bg`, session header + tab bar + tab body) · **summary** (`bg-subtle`, participants + activity). The summary rail hides under ~1100px and the sidebar under ~820px — preview at ≥1280px wide to see all three.

## Fidelity notes
- Faithful to the real product: dark-only Primer palette, blue accent, agent role colors, the `#`-prefixed session names, agent-colored message left-borders + tint, expandable diffs/test-results, status dots, unread pills.
- The sidebar wordmark pairs the brand logo mark with the "Deuce" text — a light brand touch over the code's text-only header.
- Anything not present in the source (real auth, teams UI, SSH-key dialogs, VS Code handoff) is omitted or left as an inert button.
