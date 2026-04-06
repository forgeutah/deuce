---
title: "feat: Deuce Frontend Prototype with Mock Data"
type: feat
status: active
date: 2026-05-08
origin: docs/brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md
---

# Deuce Frontend Prototype with Mock Data

## Overview

Build the complete Deuce frontend as a Tauri v2 desktop application with React, using fake data everywhere. No backend, no real agents, no DevPod integration — just a fully interactive UI prototype that demonstrates the shared agent session experience. Design system blends Slack's layout patterns with GitHub's visual language (Primer dark theme colors, monospace typography, code rendering).

This is the foundation all real functionality will be wired into later.

## Problem Statement / Motivation

Deuce needs a working frontend to validate the UX decisions from brainstorming, attract open source contributors, and serve as the shell for backend integration. Building frontend-first with mock data lets us iterate on the experience without waiting for Go server, DevPod, or LLM integration work.

(See brainstorm: `docs/brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md` for all UX decisions.)

## Proposed Solution

A Tauri v2 + React 19 + TypeScript application with:
- Three-panel resizable layout (session sidebar, tabbed center, summary panel)
- Four center tabs: Chat, Plan, Files, Terminal
- Mock data layer using MSW + Faker.js that simulates the full backend API
- Dark-mode-only theme using GitHub Primer color tokens via shadcn/ui + Tailwind v4
- Simulated agent behavior (canned responses with realistic delays)

## Tech Stack

| Layer | Library | Version | Why |
|-------|---------|---------|-----|
| Desktop | Tauri v2 | 2.11.x | Native desktop shell, small binary, web frontend |
| Bundler | Vite | 6.x | Tauri's recommended bundler, fast HMR |
| UI Framework | React + TypeScript | 19.x | Brainstorm decision (see architecture brainstorm) |
| Components | shadcn/ui | CLI 3.5.x | Copy-into-project components, Radix primitives, zero runtime |
| Styling | Tailwind CSS v4 | 4.x | CSS-first config, OKLCH colors, Oxide engine |
| Color Tokens | GitHub Primer primitives | Reference only | Extract dark-mode color scale for the GitHub aesthetic |
| Routing | TanStack Router | Latest | Memory history mode — no URL bar in Tauri |
| Panels | react-resizable-panels | 4.10.x | shadcn wraps this as `Resizable` component |
| Chat UI | prompt-kit | Latest | Lightweight chat components built on shadcn/ui |
| Markdown | react-markdown + remark-gfm | Latest | GFM rendering in chat messages |
| Code Highlighting | react-shiki | Latest | GitHub-identical syntax highlighting (TextMate grammars) |
| @mentions | react-textarea-autocomplete | Latest | Trigger-based autocomplete for @agent and @user mentions |
| Terminal | @xterm/xterm + @xterm/addon-fit + @xterm/addon-webgl | 6.x | Terminal emulator (mock data piped in for prototype) |
| Plan Editor | CodeMirror 6 via @uiw/react-codemirror | 4.x | Markdown editing + syntax highlighting, lightweight |
| File Tree | react-arborist | Latest | Virtualized tree, customizable nodes, keyboard nav |
| Code Viewer | react-shiki | Latest | Same as chat code blocks — consistent rendering |
| Mock Data | msw + @faker-js/faker + @mswjs/data | 2.x / 9.x | Intercept fetch, generate realistic data, WebSocket mocking |
| State | Zustand | Latest | Lightweight store for UI state (active session, tabs, panels) |

## Design System & Style Guide

### Philosophy

Deuce blends **Slack's layout DNA** (sidebar + channel-based navigation, chat-first interaction, message threading) with **GitHub's visual language** (Primer dark theme colors, monospace typography for code, diff rendering, badge styles). The result should feel like "GitHub built a Slack for AI agents."

### Color Palette — GitHub Primer Dark Theme

All colors sourced from [primer/primitives](https://github.com/primer/primitives) dark theme. Mapped to shadcn/ui CSS variables via Tailwind v4's `@theme` directive.

#### Neutral Scale (Backgrounds & Borders)

| Token | Hex | Usage |
|-------|-----|-------|
| `neutral-0` | `#010409` | Deepest background (inset areas, terminal) |
| `neutral-1` | `#0D1117` | **App background** — the primary canvas color |
| `neutral-2` | `#151B23` | Sidebar background, card backgrounds |
| `neutral-3` | `#212830` | Hover states, elevated surfaces |
| `neutral-4` | `#262C36` | Active/selected states |
| `neutral-5` | `#2A313C` | Input field backgrounds |
| `neutral-6` | `#2F3742` | Subtle borders, dividers |
| `neutral-7` | `#3D444D` | **Default border** color |
| `neutral-8` | `#656C76` | Muted text, placeholder text |
| `neutral-9` | `#9198A1` | Secondary text |
| `neutral-10` | `#B7BDC8` | Tertiary text, timestamps |
| `neutral-11` | `#D1D7E0` | **Primary text** |
| `neutral-12` | `#F0F6FC` | Emphasis text, headings |

#### Semantic Mapping to shadcn/ui Variables

```css
/* src/styles/globals.css */
@import "tailwindcss";

@custom-variant dark (&:is(.dark *));

@theme inline {
  /* Backgrounds */
  --color-background: #0D1117;          /* neutral-1: app canvas */
  --color-background-subtle: #151B23;   /* neutral-2: sidebar, cards */
  --color-background-inset: #010409;    /* neutral-0: terminal, deep inset */
  --color-background-emphasis: #262C36; /* neutral-4: selected items */
  --color-background-hover: #212830;    /* neutral-3: hover states */
  --color-background-input: #2A313C;    /* neutral-5: input fields */
  --color-background-overlay: #2F3742;  /* neutral-6: modals, dropdowns */

  /* Foreground (text) */
  --color-foreground: #D1D7E0;          /* neutral-11: primary text */
  --color-foreground-emphasis: #F0F6FC; /* neutral-12: headings, bold */
  --color-foreground-muted: #9198A1;    /* neutral-9: secondary text */
  --color-foreground-subtle: #656C76;   /* neutral-8: placeholders, timestamps */
  --color-foreground-on-emphasis: #FFFFFF; /* white: text on accent backgrounds */

  /* Borders */
  --color-border: #3D444D;             /* neutral-7: default borders */
  --color-border-muted: #2F3742;       /* neutral-6: subtle dividers */
  --color-border-emphasis: #656C76;    /* neutral-8: prominent borders */

  /* Accent (blue — primary actions, links) */
  --color-accent: #58a6ff;             /* blue-3: links, active states */
  --color-accent-emphasis: #1f6feb;    /* blue-5: buttons, strong accent */
  --color-accent-muted: #0c2d6b;      /* blue-8: accent backgrounds */

  /* Success (green) */
  --color-success: #3fb950;            /* green-3: pass, success states */
  --color-success-emphasis: #238636;   /* green-5: success buttons */
  --color-success-muted: #033a16;      /* green-8: success backgrounds */

  /* Danger (red) */
  --color-danger: #f85149;             /* red-4: errors, destructive actions */
  --color-danger-emphasis: #da3633;    /* red-5: danger buttons */
  --color-danger-muted: #67060c;       /* red-8: danger backgrounds */

  /* Warning (yellow) */
  --color-warning: #d29922;            /* yellow-3: warnings, attention */
  --color-warning-emphasis: #9e6a03;   /* yellow-5: warning buttons */
  --color-warning-muted: #4b2900;      /* yellow-8: warning backgrounds */

  /* Purple */
  --color-purple: #BE8FFF;             /* purple-3: reviewer agent */
  --color-purple-emphasis: #8957e5;    /* purple-5 */

  /* Pink */
  --color-pink: #f778ba;              /* pink-3: designer agent */

  /* Orange */
  --color-orange: #f0883e;            /* orange-3: tester agent */

  /* Coral */
  --color-coral: #f78166;             /* coral-3 */
}
```

#### Agent Color Assignments

Each agent role has a distinct color from the Primer palette, used for avatar background, left border on messages, and status indicators:

| Agent | Primary Color | Hex | Muted Background |
|-------|--------------|-----|-----------------|
| Coder | Blue | `#58a6ff` | `#0c2d6b` |
| Reviewer | Purple | `#BE8FFF` | `#3c1e70` |
| Planner | Green | `#3fb950` | `#033a16` |
| Tester | Orange/Yellow | `#d29922` | `#4b2900` |
| Designer | Pink | `#f778ba` | `#5e103e` |

### Typography

Follow GitHub's system font stacks for consistency with the GitHub aesthetic:

```css
@theme inline {
  /* UI text — system font stack (same as GitHub) */
  --font-sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans",
    Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji";

  /* Code and terminal — monospace stack */
  --font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas,
    "Liberation Mono", monospace;
}
```

#### Type Scale

| Use | Size | Weight | Font |
|-----|------|--------|------|
| App title / header | 18px / 1.125rem | 600 (semibold) | Sans |
| Section headers (Participants, Activity) | 12px / 0.75rem | 600 (semibold) | Sans, uppercase, letter-spacing 0.05em |
| Session name in sidebar | 14px / 0.875rem | 500 (medium) | Sans |
| Chat message body | 14px / 0.875rem | 400 (regular) | Sans |
| Chat author name | 14px / 0.875rem | 600 (semibold) | Sans |
| Timestamp | 12px / 0.75rem | 400 (regular) | Sans, `foreground-subtle` color |
| Code blocks in chat | 13px / 0.8125rem | 400 (regular) | Mono |
| Terminal | 13px / 0.8125rem | 400 (regular) | Mono |
| Plan editor | 14px / 0.875rem | 400 (regular) | Mono |
| Input field text | 14px / 0.875rem | 400 (regular) | Sans |
| Badge/label text | 11px / 0.6875rem | 500 (medium) | Sans |
| Activity feed items | 12px / 0.75rem | 400 (regular) | Sans |

### Spacing & Layout

Use Tailwind's default 4px grid. Key spacing values:

| Element | Value |
|---------|-------|
| Panel padding | 12px (`p-3`) |
| Between messages | 16px (`gap-4`) |
| Between sidebar items | 2px (`gap-0.5`) |
| Section header margin-bottom | 8px (`mb-2`) |
| Input padding | 10px 12px (`px-3 py-2.5`) |
| Card padding | 12px (`p-3`) |
| Border radius (cards, inputs) | 6px (`rounded-md`) — matches GitHub's `--borderRadius-medium` |
| Border radius (buttons) | 6px (`rounded-md`) |
| Border radius (avatars) | Full circle for humans, rounded square (4px) for agents |
| Icon size (sidebar) | 16px |
| Avatar size (chat) | 28px |
| Avatar size (participant list) | 24px |
| Badge size (unread count) | 18px min-width, 10px height |

### Component Visual Patterns

#### Chat Messages (Slack-style)
- Messages grouped by author: avatar + name + timestamp on first message, subsequent messages from same author have no header (just indented under the avatar)
- Human messages: no background, just text on canvas
- Agent messages: subtle left border (2px) in the agent's color, very slight background tint (`agent-color` at 5% opacity)
- Expandable content: rounded card with `neutral-2` background, `neutral-7` border. "Show changes" link styled like GitHub's blue links (`#58a6ff`)

#### Session Sidebar (Slack-style)
- Each session item: `#` prefix in `foreground-subtle`, session name in `foreground`, unread count badge (small red/accent pill)
- Active session: `background-emphasis` with left border accent (2px, `accent` blue)
- Hover: `background-hover`
- Project group headers: uppercase, `foreground-subtle`, 12px, semibold, with collapse chevron

#### Tabs (GitHub-style)
- Tab bar: bottom border in `border-muted`. Active tab has `accent` blue bottom border (2px) and `foreground-emphasis` text. Inactive tabs: `foreground-muted` text, no bottom border
- Tab content area: full height of center panel minus tab bar and input area

#### Badges & Status Indicators
- Unread badge: small pill, `danger` red background, white text, positioned top-right of session name
- Agent status dot: 8px circle — idle: `neutral-8`, working: `success` green with pulse animation, warming-up: `warning` yellow, error: `danger` red
- Human online dot: 8px circle — online: `success` green, offline: `neutral-7`
- File change badges: `+12` in green, `-3` in red (GitHub diff style)

#### Buttons
- Primary: `accent-emphasis` background, white text, `rounded-md`
- Secondary/ghost: transparent, `foreground-muted` text, `border` on hover
- Destructive: `danger-emphasis` background, white text

#### Dialogs/Modals
- Backdrop: black at 50% opacity
- Dialog: `background-overlay` background, `border` border, `rounded-lg` (8px), max-width 480px
- Header: `foreground-emphasis` text, 16px semibold
- Close button: top-right, ghost style

#### Code Diffs (GitHub-style)
- Unified diff format
- Added lines: `success-muted` background with `success` text prefix (`+`)
- Removed lines: `danger-muted` background with `danger` text prefix (`-`)
- Context lines: `neutral-0` background
- File header: `neutral-2` background, `foreground` text, filename in `foreground-emphasis`
- Line numbers: `foreground-subtle`, right-aligned, monospace

#### Terminal
- Background: `neutral-0` (deepest black)
- Text: `neutral-11` (primary text)
- Cursor: `accent` blue, blinking
- Selection: `accent-muted` at 40% opacity
- Bold text: `foreground-emphasis`
- Prompt: `success` green for user@host, `accent` blue for working directory

### Iconography

Use [Lucide React](https://lucide.dev/) icons (already a shadcn/ui dependency). Key icons:

| Element | Icon |
|---------|------|
| Session | `Hash` (#) |
| New session | `Plus` |
| Chat tab | `MessageSquare` |
| Plan tab | `FileText` |
| Files tab | `FolderTree` |
| Terminal tab | `Terminal` |
| Settings | `Settings` |
| Team | `Users` |
| Search | `Search` |
| Agent (generic) | `Bot` |
| Expand/collapse | `ChevronDown` / `ChevronRight` |
| File | `File` |
| Folder | `Folder` / `FolderOpen` |
| Git commit | `GitCommit` |
| Check/pass | `Check` or `CircleCheck` |
| Error/fail | `CircleX` |
| Warning | `AlertTriangle` |
| Close | `X` |
| Send message | `SendHorizontal` |

### Animations & Transitions

Keep animations subtle and functional — not decorative:

| Element | Animation | Duration |
|---------|-----------|----------|
| Tab switch | None (instant swap) | 0ms |
| Panel resize | CSS `resize` with `will-change: width` | Real-time |
| Panel collapse/expand | Width transition | 200ms ease-out |
| Message appear | Fade-in + subtle slide-up | 150ms |
| Dialog open | Fade-in + scale from 0.95 | 150ms |
| Dialog close | Fade-out + scale to 0.95 | 100ms |
| Collapsible expand | Height animation (shadcn default) | 200ms |
| Typing indicator dots | Three dots, sequential opacity pulse | 1.4s loop |
| Agent status "working" | Green dot with radial pulse | 2s loop |
| Unread badge appear | Scale bounce from 0 to 1 | 200ms spring |
| Hover state | Background color transition | 100ms |
| Skeleton loading | Shimmer gradient sweep | 1.5s loop |

## Technical Approach

### Architecture

```
src/
├── app/                    # App shell, routing, providers
│   ├── App.tsx
│   ├── router.tsx          # TanStack Router with memory history
│   └── providers.tsx       # Theme, store, MSW providers
├── components/
│   ├── ui/                 # shadcn/ui components (generated)
│   ├── layout/
│   │   ├── AppShell.tsx           # Three-panel layout container
│   │   ├── SessionSidebar.tsx     # Left panel
│   │   ├── CenterPanel.tsx        # Tabbed workspace
│   │   └── SummaryPanel.tsx       # Right panel
│   ├── chat/
│   │   ├── ChatView.tsx           # Chat tab container
│   │   ├── MessageList.tsx        # Virtualized message list
│   │   ├── MessageBubble.tsx      # Single message (human or agent)
│   │   ├── AgentResponse.tsx      # Expandable agent output
│   │   ├── ChatInput.tsx          # Input with @mention autocomplete
│   │   └── TypingIndicator.tsx    # "Agent is thinking..." state
│   ├── plan/
│   │   ├── PlanView.tsx           # Plan tab container
│   │   └── MarkdownEditor.tsx     # CodeMirror markdown editor + preview
│   ├── files/
│   │   ├── FilesView.tsx          # Files tab container
│   │   ├── FileTree.tsx           # react-arborist tree
│   │   └── CodeViewer.tsx         # react-shiki read-only viewer
│   ├── terminal/
│   │   ├── TerminalView.tsx       # Terminal tab container
│   │   └── TerminalEmulator.tsx   # xterm.js wrapper
│   ├── session/
│   │   ├── CreateSessionDialog.tsx # Quick create modal
│   │   └── SessionCard.tsx         # Sidebar session item
│   ├── participants/
│   │   ├── ParticipantList.tsx     # Right panel top section
│   │   ├── ParticipantRow.tsx      # User/agent row with status
│   │   └── EditAgentsDialog.tsx    # Add/remove agents modal
│   ├── activity/
│   │   └── ActivityFeed.tsx        # Right panel bottom section
│   └── onboarding/
│       ├── OnboardingWizard.tsx    # Multi-step wizard container
│       ├── OAuthStep.tsx           # Step 1: Login
│       ├── TeamStep.tsx            # Step 2: Join/create team
│       └── ApiKeyStep.tsx          # Step 3: Configure keys (skippable)
├── mocks/
│   ├── browser.ts          # MSW browser worker setup
│   ├── handlers/
│   │   ├── sessions.ts     # Session CRUD handlers
│   │   ├── messages.ts     # Chat message handlers
│   │   ├── agents.ts       # Agent response simulation
│   │   └── files.ts        # File tree + content handlers
│   ├── data/
│   │   ├── factory.ts      # @mswjs/data model definitions
│   │   ├── seed.ts         # Seed realistic initial data
│   │   └── agent-responses.ts # Canned agent responses with code diffs
│   └── ws/
│       └── chat-socket.ts  # WebSocket mock for real-time chat simulation
├── stores/
│   ├── session-store.ts    # Active session, tab state per session
│   ├── ui-store.ts         # Panel sizes, sidebar collapsed state
│   └── onboarding-store.ts # Onboarding completion state
├── hooks/
│   ├── use-session.ts      # Current session data + actions
│   ├── use-messages.ts     # Messages for active session
│   └── use-participants.ts # Participants + agent status
├── types/
│   └── index.ts            # Session, Message, Agent, User, Team types
└── styles/
    └── globals.css         # Tailwind v4 imports + Primer dark tokens
```

### Implementation Phases

#### Phase 1: Project Scaffolding + Design System

Set up the project skeleton and establish the visual language.

**Tasks:**
- [ ] `package.json` — Scaffold Tauri v2 + React 19 + TypeScript with `create-tauri-app`
- [ ] `vite.config.ts` — Configure Vite with Tailwind v4 plugin
- [ ] `src/styles/globals.css` — Initialize shadcn/ui with Tailwind v4, set up dark-mode-only theme using GitHub Primer color tokens (dark neutral scale, semantic colors, accent blues in OKLCH)
- [ ] `components.json` — Run `npx shadcn@latest init` and configure for dark mode
- [ ] `src/app/router.tsx` — Set up TanStack Router with `createMemoryHistory`
- [ ] `src/app/providers.tsx` — Create provider wrapper (router, theme, store)
- [ ] `src/app/App.tsx` — App shell that renders providers + router outlet
- [ ] Generate base shadcn/ui components: `Button`, `Dialog`, `Tabs`, `Tooltip`, `Avatar`, `Badge`, `Input`, `Collapsible`, `Resizable`, `ScrollArea`, `Separator`, `DropdownMenu`
- [ ] Configure Tailwind `fontFamily` — system font stack for UI, monospace for code (matching GitHub)
- [ ] Verify the app opens in Tauri desktop window with dark theme applied

**Success criteria:** Tauri window opens showing a dark-themed "Hello Deuce" placeholder. All dependencies installed. Hot reload works.

**Estimated effort:** Small

#### Phase 2: Three-Panel Layout Shell

Build the resizable three-panel layout with tab navigation.

**Tasks:**
- [ ] `src/components/layout/AppShell.tsx` — Three-panel layout using shadcn `ResizablePanelGroup` + `ResizablePanel` + `ResizableHandle`. Default sizes: left 20%, center 55%, right 25%. Min sizes: left 180px, center 400px, right 180px. Persist panel sizes to localStorage
- [ ] `src/components/layout/SessionSidebar.tsx` — Left panel skeleton: search input at top, project groups (expandable), session items within groups, "Teams" and "Settings" nav items at bottom, "+" new session button
- [ ] `src/components/layout/CenterPanel.tsx` — Tabbed area using shadcn `Tabs`. Four tabs: Chat, Plan, Files, Terminal. Tab state preserved per session (switching sessions remembers which tab was active). Chat input bar pinned at bottom (visible on Chat tab only)
- [ ] `src/components/layout/SummaryPanel.tsx` — Right panel skeleton: "Participants" section at top, "Activity" section at bottom, separated by a divider. Both sections scrollable independently
- [ ] Left and right panels collapsible via toggle button on resize handle
- [ ] Keyboard shortcut: `Cmd+B` to toggle left sidebar, `Cmd+Shift+B` to toggle right panel

**Success criteria:** Three resizable panels render. Tabs switch in the center panel. Panels collapse/expand. Layout persists across app restart via localStorage.

**Estimated effort:** Medium

#### Phase 3: Mock Data Layer

Build the fake backend that drives the entire prototype.

**Tasks:**
- [ ] `src/types/index.ts` — Define TypeScript types:
  ```typescript
  type Team = { id, name, slug, members: User[] }
  type Project = { id, name, repoUrl, teamId }
  type Session = {
    id, name, projectId, status: 'active' | 'paused' | 'archived',
    agents: Agent[], members: User[], unreadCount: number,
    activeTab: TabType, createdAt, lastActivityAt,
    workspaceStatus: 'starting' | 'ready' | 'failed' | 'suspended'
  }
  type Message = {
    id, sessionId, authorId, authorType: 'human' | 'agent',
    content: string, expandableContent?: ExpandableContent[],
    mentions: string[], createdAt, status: 'sent' | 'thinking' | 'error'
  }
  type ExpandableContent = {
    type: 'diff' | 'test-results' | 'terminal-output',
    title: string, summary: string, content: string
  }
  type Agent = {
    id, name, role: string, avatar: string,
    status: 'idle' | 'working' | 'warming-up' | 'error',
    provider: string, model: string
  }
  type User = { id, name, email, avatar, status: 'online' | 'offline' }
  type FileNode = { name, path, type: 'file' | 'directory', children?, modifiedBy?: string }
  type ActivityItem = { id, type, description, timestamp, agentId? }
  ```
- [ ] `src/mocks/data/factory.ts` — Define @mswjs/data models matching the types above
- [ ] `src/mocks/data/seed.ts` — Seed realistic data:
  - 2 teams, 3 projects across them
  - 5-6 sessions across projects (mix of active, paused, archived)
  - 4-5 users with avatars (faker + dicebear)
  - Preset agents: Coder, Reviewer, Planner, Tester (each with a distinct avatar/color)
  - 15-30 messages per active session (mix of human + agent, some with expandable content)
  - Realistic file trees (mirroring a Go + React project structure)
  - Plan documents with markdown content (acceptance criteria, tech notes)
  - Activity items (file changes, commits, test runs)
- [ ] `src/mocks/data/agent-responses.ts` — Canned agent responses:
  - Coder: code diffs with expandable unified diff view
  - Reviewer: review comments with file references
  - Planner: structured plans with task lists
  - Tester: test results with pass/fail expandable output
- [ ] `src/mocks/handlers/sessions.ts` — MSW handlers: GET /sessions, POST /sessions, PATCH /sessions/:id
- [ ] `src/mocks/handlers/messages.ts` — MSW handlers: GET /sessions/:id/messages, POST /sessions/:id/messages (triggers simulated agent response after 1-3s delay)
- [ ] `src/mocks/handlers/agents.ts` — Agent response simulation: when a message @mentions an agent, queue a "thinking" indicator, then deliver a canned response after delay
- [ ] `src/mocks/handlers/files.ts` — MSW handlers: GET /sessions/:id/files (return mock file tree), GET /sessions/:id/files/:path (return mock file content)
- [ ] `src/mocks/browser.ts` — MSW browser worker initialization
- [ ] Wire MSW startup into `src/main.tsx` — start the service worker before rendering the app

**Success criteria:** `fetch('/api/sessions')` returns realistic session data. Posting a message with `@Coder` triggers a delayed agent response. File tree and file content endpoints return realistic data. All data is seeded on app start.

**Estimated effort:** Large

#### Phase 4: Session Sidebar

Build the left panel with session list grouped by project.

**Tasks:**
- [ ] `src/components/session/SessionCard.tsx` — Session list item: session name with `#` prefix, unread badge (red dot + count), workspace status indicator (green dot = ready, yellow = starting, red = failed, gray = suspended), truncated last message preview, relative timestamp. Active session highlighted with accent background
- [ ] `src/components/layout/SessionSidebar.tsx` — Full implementation: project groups as collapsible sections (shadcn `Collapsible`), sessions sorted by lastActivityAt within each group. "+" button opens `CreateSessionDialog`. Search input at top filters sessions by name. "Teams" and "Settings" items at bottom navigate to placeholder views
- [ ] `src/stores/session-store.ts` — Zustand store: active session ID, per-session active tab map, setActiveSession(), setActiveTab(). Fetch sessions from MSW on mount
- [ ] Click session → updates active session in store → center panel loads that session's data → unread badge clears

**Success criteria:** Sidebar shows sessions grouped by project. Clicking a session loads it in the center panel. Unread badges appear and clear correctly. Search filters the list.

**Estimated effort:** Medium

#### Phase 5: Chat Tab

The primary interaction surface — chat with @mention agent invocation.

**Tasks:**
- [ ] `src/components/chat/MessageList.tsx` — Virtualized scrollable message list (use shadcn `ScrollArea`). Auto-scrolls to bottom on new messages. Loads message history for active session. Group consecutive messages from same author
- [ ] `src/components/chat/MessageBubble.tsx` — Message rendering: author avatar + name + timestamp header. Content rendered as GFM markdown (react-markdown + remark-gfm). Agent messages have a subtle left border in the agent's color. Human messages are plain
- [ ] `src/components/chat/AgentResponse.tsx` — Expandable agent content: summary line shown inline, "Show changes" / "Show tests" / "Show output" buttons. Clicking expands a `Collapsible` with the full content. Code diffs rendered with react-shiki using `github-dark` theme. Collapse/expand with smooth animation
- [ ] `src/components/chat/ChatInput.tsx` — Text input at bottom of chat tab. Plain text, renders markdown on send. @mention autocomplete: typing `@` shows a dropdown of agents + participants in the session (react-textarea-autocomplete). Enter to send, Shift+Enter for newline. Disabled state when no session is selected
- [ ] `src/components/chat/TypingIndicator.tsx` — "Coder is thinking..." indicator with pulsing dots. Shown below the last message when an agent is processing. Displays agent avatar + name + animated dots
- [ ] Agent simulation flow: user sends message with @mention → message appears in chat → TypingIndicator shows → 1-3s delay → agent response appears with expandable content → TypingIndicator disappears → agent status in right panel updates (idle → working → idle)
- [ ] Empty state: when a session has no messages, show a centered prompt: "Start a conversation. @mention an agent to get started." with a list of available agents in the session

**Success criteria:** Messages render with proper markdown. @mentioning an agent triggers a simulated response with expandable content. Typing indicator shows during simulated "thinking" time. Chat auto-scrolls. Empty state is friendly and informative.

**Estimated effort:** Large

#### Phase 6: Plan Tab

Shared markdown editor with live preview.

**Tasks:**
- [ ] `src/components/plan/PlanView.tsx` — Split view container: editor on left, rendered preview on right. Toggle button to switch between split/editor-only/preview-only
- [ ] `src/components/plan/MarkdownEditor.tsx` — CodeMirror 6 editor with markdown language support (`@codemirror/lang-markdown`). Dark theme matching the app (use `@uiw/codemirror-theme-github` dark variant or custom theme with Primer tokens). Preview rendered with react-markdown + remark-gfm. Changes saved to mock store (simulating persistence)
- [ ] Pre-populated plan content for seeded sessions — realistic markdown with headings, acceptance criteria checklists, code blocks, and discussion notes
- [ ] Empty state for new sessions: "No plan yet. Start writing to define what this session should accomplish."

**Success criteria:** Editor and preview render side by side. Markdown changes reflect in preview in real-time. Content persists per session (in memory via Zustand/MSW). Dark theme is consistent.

**Estimated effort:** Medium

#### Phase 7: Files Tab

File tree browser with read-only code viewer.

**Tasks:**
- [ ] `src/components/files/FileTree.tsx` — react-arborist tree component. Custom node renderer: file-type icons (use react-icons `VscFile`, `VscFolder`), file names, agent-modified indicator (small colored dot matching the agent's color). Expand/collapse directories. Click file to view content. Keyboard navigation
- [ ] `src/components/files/CodeViewer.tsx` — Read-only code display using react-shiki with `github-dark` theme. File path shown as breadcrumb above the viewer. Line numbers. Language auto-detected from file extension
- [ ] `src/components/files/FilesView.tsx` — Split layout: file tree on left (30%), code viewer on right (70%). Resizable divider between them
- [ ] Mock file tree: realistic project structure (Go backend + React frontend). 3-5 files marked as "modified by agent" with change indicators
- [ ] Empty state when workspace is "starting": "Workspace warming up..." with a spinner. Empty state when no file selected: "Select a file to view its contents"
- [ ] "Workspace failed" state: error message with a mock "Retry" button

**Success criteria:** File tree renders with icons and expand/collapse. Clicking a file shows syntax-highlighted content. Agent-modified files have visual indicators. Loading/error states display correctly.

**Estimated effort:** Medium

#### Phase 8: Terminal Tab

Mock terminal emulator.

**Tasks:**
- [ ] `src/components/terminal/TerminalEmulator.tsx` — xterm.js wrapper using @xterm/xterm. Dark theme matching the app. FitAddon for responsive sizing. WebGL addon for performance (with canvas fallback)
- [ ] `src/components/terminal/TerminalView.tsx` — Container that manages the xterm instance lifecycle. Terminal stays mounted (hidden) when switching to other tabs — preserves scrollback and state
- [ ] Mock terminal behavior: on mount, simulate a shell prompt (`user@deuce:~/project $`). User can type commands. Canned responses for a few commands: `ls` shows the mock file tree, `git status` shows modified files, `go test` shows passing tests, anything else echoes "command simulated". This demonstrates the terminal works without a real PTY
- [ ] "Workspace warming up" state: terminal area shows a loading animation with "Connecting to workspace..." until mock workspace status is "ready"
- [ ] "Workspace failed" state: error message overlay with "Retry" button

**Success criteria:** Terminal renders with a working prompt. Users can type and see mock responses. Terminal state persists across tab switches. Loading/error states work.

**Estimated effort:** Medium

#### Phase 9: Summary Panel (Right)

Participants list and activity feed.

**Tasks:**
- [ ] `src/components/participants/ParticipantList.tsx` — "Participants" header with member count. Agents section: each agent row shows avatar (colored circle with first letter), name, role label, status badge (idle=gray, working=green pulse, warming-up=yellow, error=red). Human section: avatar, name, online/offline indicator. "Edit agents" button opens dialog
- [ ] `src/components/participants/EditAgentsDialog.tsx` — Dialog with list of available agent roles (preset + custom). Checkboxes for which agents are in this session. Add/remove agents. Changes update the participant list immediately
- [ ] `src/components/activity/ActivityFeed.tsx` — Scrollable timeline. Activity types: file change (icon + filename + "+X -Y"), test run (pass/fail icon + "4/4 passing"), commit (git icon + short SHA + message), agent action (agent avatar + "started working" / "completed task"). Relative timestamps. Grouped by time (Today, Yesterday, etc.)
- [ ] Activity items link to relevant content: clicking a file change switches to Files tab and selects that file. Clicking a commit is a no-op for v0

**Success criteria:** Participants show with correct status indicators. Agent statuses update during simulated agent work. Activity feed shows a realistic timeline. Edit agents dialog works.

**Estimated effort:** Medium

#### Phase 10: Session Creation Dialog

Quick-create flow for new sessions.

**Tasks:**
- [ ] `src/components/session/CreateSessionDialog.tsx` — shadcn Dialog triggered by "+" button in sidebar. Fields:
  - Session name (text input, required, auto-slugified for the `#` display)
  - Project (dropdown of existing projects, or "New project" which shows repo URL input)
  - Agents (checkboxes: Coder, Reviewer, Planner, Tester, plus any custom agents)
  - Members (multi-select of team members, pre-selected with current user)
  - "Create Session" button
- [ ] On submit: create session in MSW data store, add to sidebar, switch to the new session, show workspace "starting" state, after 3-5s delay transition to "ready" state (simulating DevPod startup)
- [ ] Validation: session name required, at least one project selected. Allow zero agents (valid — just a team chat with a workspace)
- [ ] Chat and Plan tabs available immediately after creation. Files and Terminal show "warming up" state during the simulated startup delay

**Success criteria:** Dialog opens, validates input, creates a session. New session appears in sidebar under correct project group. Workspace warming-up → ready transition is visible across all tabs and the right panel.

**Estimated effort:** Small

#### Phase 11: Onboarding Flow

First-launch wizard for new users.

**Tasks:**
- [ ] `src/components/onboarding/OnboardingWizard.tsx` — Full-screen multi-step wizard. Step indicator at top. "Skip" option on non-critical steps. Wizard state in Zustand, persisted to localStorage (so it doesn't show again after completion)
- [ ] `src/components/onboarding/OAuthStep.tsx` — Mock OAuth step: "Sign in with GitHub" and "Sign in with Google" buttons. Clicking either simulates a successful login (sets mock user in store)
- [ ] `src/components/onboarding/TeamStep.tsx` — Two options: "Join a team" (paste invite link input) or "Create a team" (team name input). Both simulate success
- [ ] `src/components/onboarding/ApiKeyStep.tsx` — Form fields for OpenAI, Anthropic, or custom API key entry. "Skip for now" button. Simulates saving keys
- [ ] After wizard completion: transition to main UI. If no sessions exist, show the post-onboarding empty state

**Success criteria:** Wizard appears on first launch only. Each step works with mock data. Completing the wizard lands on the main UI. Skipping API keys still works. Wizard doesn't reappear on subsequent launches.

**Estimated effort:** Small

#### Phase 12: Empty States + Loading States + Polish

Fill all the gaps identified by SpecFlow analysis.

**Tasks:**
- [ ] Post-onboarding empty state: center panel shows "Welcome to Deuce" with a "Create your first session" CTA button. Right panel shows a getting-started checklist. Left sidebar shows empty state with "No sessions yet"
- [ ] Session with no messages: centered prompt with available agents listed as clickable chips that insert `@AgentName ` into the input
- [ ] Agent error state in chat: if mock simulates a failure, show an error message inline: "Coder encountered an error. [Retry]" with the agent's color border
- [ ] "No API key configured" state: when @mentioning an agent without a configured key, show inline: "No API key configured for Anthropic. [Configure in Settings]"
- [ ] Session states in sidebar: active = normal, paused = dimmed with "Paused" label, archived = dimmed with "Archived" label and lock icon
- [ ] Paused session: chat is read-only (input disabled with "Session paused" message). "Resume" button in the center panel header
- [ ] Archived session: chat is read-only. All tabs are read-only. "This session is archived" banner
- [ ] Loading skeleton for center panel when switching sessions (brief shimmer animation)
- [ ] Smooth transitions: tab switching, panel collapse, dialog open/close, message appear animations
- [ ] @mention autocomplete dropdown styling: dark theme, agent avatars + role labels, keyboard navigable
- [ ] Responsive min-width handling: if window is too narrow for three panels, auto-collapse right panel. If still too narrow, collapse left panel too

**Success criteria:** No blank states anywhere in the app. Every async operation has a loading state. Error states are informative with actions. Transitions feel polished.

**Estimated effort:** Medium

## Alternative Approaches Considered

1. **Web-only (no Tauri)** — Simpler to develop and test, but the brainstorm decided on a desktop app. We can still develop as a web app and wrap in Tauri for the final build — Vite serves both.

2. **Storybook-first** — Build every component in Storybook isolation before assembling. More thorough but slower. Decision: build assembled, add Storybook stories for complex components only (MessageBubble, AgentResponse, TerminalEmulator).

3. **Next.js instead of Vite** — Unnecessary for a Tauri app with no SSR needs. Vite is lighter and Tauri's recommended bundler.

## System-Wide Impact

### State Lifecycle

The mock data layer (MSW + @mswjs/data) manages all application state in memory. Page refresh resets to seed data. This is intentional for the prototype. When the real backend is integrated:
- MSW handlers get replaced with real API calls (component code doesn't change since it uses `fetch()`)
- Zustand stores may need to sync with WebSocket events for real-time updates
- LocalStorage for panel sizes and onboarding state remains as-is

### Integration Points for Future Backend

| Frontend Feature | Future Backend Integration |
|-----------------|---------------------------|
| Chat messages | WebSocket → Go server → LLM APIs |
| Terminal | @xterm/addon-attach → WebSocket → tauri-plugin-pty → DevPod shell |
| File tree | REST API → Go server → DevPod filesystem |
| Plan editor | Y.js CRDT → WebSocket → Go server → Postgres |
| Agent simulation | Real LLM API calls through Go server agent orchestrator |
| OAuth | Real OAuth flow through Go server (Tauri deep links) |
| Session CRUD | REST API → Go server → Postgres |

## Acceptance Criteria

### Functional Requirements

- [ ] App opens as a Tauri desktop window with dark theme
- [ ] Three resizable panels: sidebar, center (tabbed), summary
- [ ] Left sidebar lists sessions grouped by project with unread badges
- [ ] Center panel has four tabs: Chat, Plan, Files, Terminal
- [ ] Active tab is preserved per session when switching between sessions
- [ ] Chat renders markdown messages from humans and agents
- [ ] @mention autocomplete works for agents and participants
- [ ] Agent responses appear after a simulated delay with expandable content
- [ ] "Agent is thinking..." indicator shows during simulated processing
- [ ] Plan tab has a working markdown editor with live preview
- [ ] Files tab shows a file tree with syntax-highlighted code viewer
- [ ] Terminal tab shows an xterm.js terminal with mock command responses
- [ ] Right panel shows participants with status indicators and activity feed
- [ ] Session creation dialog creates a session with simulated DevPod startup
- [ ] Onboarding wizard appears on first launch only
- [ ] Agent management: add/remove agents from session via right panel
- [ ] All empty states, loading states, and error states are implemented
- [ ] Session states (active, paused, archived) display correctly with appropriate restrictions

### Non-Functional Requirements

- [ ] Initial load under 2 seconds in Tauri window
- [ ] Tab switching feels instant (< 100ms perceived)
- [ ] Chat can display 1000+ messages without jank (virtualized list)
- [ ] File tree can display 500+ files without jank (react-arborist virtualization)
- [ ] Terminal scrollback handles 5000+ lines
- [ ] Panel resizing is 60fps smooth
- [ ] All text is readable at default system font size
- [ ] Monospace font renders consistently across macOS, Windows, Linux

### Quality Gates

- [ ] TypeScript strict mode — no `any` types
- [ ] All components have proper TypeScript interfaces for props
- [ ] ESLint + Prettier configured and passing
- [ ] Tauri build produces a working binary for macOS (minimum — Linux/Windows are stretch goals)

## Dependencies & Prerequisites

- Node.js 20+
- Rust toolchain (for Tauri)
- macOS with Xcode CLT (for development) — or Linux with required system deps

## Mock Data Specification

### Seed Data

**Teams:**
- "Forge Utah" (3 members)
- "Acme Corp" (2 members)

**Projects:**
- "forge-api" (Go backend, Forge Utah team)
- "forge-web" (React frontend, Forge Utah team)  
- "acme-dashboard" (Full stack, Acme Corp team)

**Sessions (6 total):**
1. `#auth-module` (forge-api, active, 3 agents: Coder + Reviewer + Tester, 25 messages)
2. `#api-rate-limiting` (forge-api, active, 2 agents: Coder + Planner, 12 messages)
3. `#homepage-redesign` (forge-web, active, 2 agents: Coder + Designer, 18 messages)
4. `#ci-pipeline` (forge-api, paused, 1 agent: Coder, 8 messages)
5. `#onboarding-flow` (forge-web, archived, 2 agents: Coder + Planner, 30 messages)
6. `#dashboard-charts` (acme-dashboard, active, 3 agents: Coder + Reviewer + Tester, 15 messages)

**Agent Presets:**
| Agent | Avatar Color | Role Label | Mock Provider |
|-------|-------------|------------|---------------|
| Coder | Blue (#58a6ff) | "Writes and modifies code" | Anthropic / Claude |
| Reviewer | Purple (#bc8cff) | "Reviews code changes" | Anthropic / Claude |
| Planner | Green (#3fb950) | "Creates implementation plans" | OpenAI / GPT-4 |
| Tester | Orange (#d29922) | "Writes and runs tests" | Anthropic / Claude |
| Designer | Pink (#f778ba) | "UI/UX suggestions" | OpenAI / GPT-4 |

### Mock Agent Response Behavior

When a user sends `@Coder fix the token validation`:
1. Message appears in chat (instant)
2. Coder status changes to "working" in right panel (instant)
3. TypingIndicator shows "Coder is thinking..." (instant)
4. After 2-3s delay: Coder responds with summary + expandable diff
5. Coder status returns to "idle"
6. Activity feed adds "Coder modified auth.go (+12 -3)"

## Sources & References

### Origin

- **Architecture brainstorm:** [docs/brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md](../brainstorms/2026-05-08-deuce-shared-agent-sessions-brainstorm.md) — Tech stack (Go + React + Tauri), session-centric monolith, DevPod workspaces, hybrid agent model
- **UX brainstorm:** [docs/brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md](../brainstorms/2026-05-08-deuce-v0-ux-brainstorm.md) — Three-panel layout, four tabs, @mention interaction, dark mode only, same UI for all roles

### External References

- [Tauri v2 + React setup](https://v2.tauri.app/start/create-project/)
- [shadcn/ui Tailwind v4](https://ui.shadcn.com/docs/tailwind-v4)
- [GitHub Primer color primitives](https://github.com/primer/primitives)
- [react-resizable-panels](https://github.com/bvaughn/react-resizable-panels)
- [prompt-kit chat UI](https://www.prompt-kit.com/)
- [react-shiki](https://github.com/AVGVSTVS96/react-shiki)
- [@xterm/xterm](https://www.npmjs.com/package/@xterm/xterm)
- [@uiw/react-codemirror](https://github.com/uiwjs/react-codemirror)
- [react-arborist](https://github.com/brimdata/react-arborist)
- [MSW (Mock Service Worker)](https://mswjs.io/)
- [@faker-js/faker](https://fakerjs.dev/)
- [TanStack Router memory history](https://tanstack.com/router/latest/docs/guide/history-types)
