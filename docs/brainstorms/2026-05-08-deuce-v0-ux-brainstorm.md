# Brainstorm: Deuce v0 User Experience

**Date:** 2026-05-08
**Status:** Draft
**Participants:** Clint Berry, Claude
**Related:** [Deuce Architecture Brainstorm](2026-05-08-deuce-shared-agent-sessions-brainstorm.md)

---

## What We're Building

The complete user experience for Deuce v0 — from first launch through daily use. This covers layout, navigation, session creation, agent interaction, and how different team members (engineers, PMs, designers) use the same interface.

---

## User Journey

### 1. First Launch — Onboarding Wizard

New users go through a guided setup:
1. **OAuth login** — Sign in with GitHub or Google
2. **Team setup** — Join an existing team (via invite link) or create a new one
3. **API keys** — Configure LLM provider keys (can skip and do later)
4. **Done** — Land on the main interface

### 2. Main Layout — Three-Panel Design

```
+----------+------------------------+--------------+
| Sessions |  Center (Tabbed)       | Summary      |
|          |                        |              |
| #auth    |  [Chat] [Plan] [Files] [Terminal]     |
| #feature |                        | Participants |
| #bugfix  |  Chat content or       | @Coder (idle)|
|          |  active tab content    | @Reviewer    |
| -------- |                        | Clint (you)  |
| Teams    |                        | Sarah        |
| Settings |                        |              |
|          |                        | Activity     |
|          |                        | - auth.go +12|
|          |                        | - tests pass |
|          |                        | - commit abc |
|          |  [message input......] |              |
+----------+------------------------+--------------+
```

**Left panel:** Session list, grouped by team/project. Unread badges for activity in other sessions.

**Center panel:** Tabbed workspace with four tabs:
- **Chat** — Primary conversation thread with team members and agents
- **Plan** — Shared markdown document for collaborative planning
- **Files** — File browser/editor to inspect code changes
- **Terminal** — Direct shell access to the DevPod workspace

**Right panel:** Session summary, split into two sections:
- **Top: Participants** — Humans and agents in the session with status indicators (active, idle, working)
- **Bottom: Activity feed** — Condensed timeline of file changes, agent actions, commits

### 3. Creating a New Session

Quick create dialog triggered by a "+" button:
- Session name (required)
- Repository / devcontainer reference (required)
- Agent selection — checkboxes for which agents to include (preset roles + custom)
- Invite team members (optional, can add later)

**Workspace startup behavior:** Chat and Plan tabs are available immediately. Users can start discussing and planning while the DevPod workspace spins up in the background. Terminal and Files tabs show a "workspace warming up" state. Agents that need workspace access show as "warming up" in the right panel.

### 4. Agent Interaction — @mention to Invoke

Users direct messages to agents by @mentioning them:

```
[You] @Coder can you add token expiration checking to the auth module?

[Coder] Updated auth.go with token expiration check. Tests pass.
  [Show changes] [Show tests]
```

**Key patterns:**
- **@mention required** — Agents only respond when explicitly mentioned. Predictable, no surprise responses
- **Minimal + expandable output** — Agent messages show a brief summary. Users click to expand full details (diffs, test output, logs). Keeps chat scannable
- **Multiple agents in one message** — Users can @mention multiple agents: "@Coder implement this, @Reviewer check it when done"
- **Sub-agents are invisible** — When an agent spawns sub-agents (e.g., to run tests), that work happens behind the scenes. Only the result surfaces in chat

### 5. The Plan Tab

A collaborative shared markdown document:
- Any team member or agent can view and edit
- Agents read the plan as context when responding to requests
- No complex editor — just markdown with a live preview
- Changes are saved and visible to all session participants in real-time

Use cases:
- PM writes acceptance criteria, Coder references them
- Team brainstorms approach before asking agents to implement
- Agent drafts a technical plan, team reviews and edits before approving

### 6. The Files Tab

File browser connected to the DevPod workspace:
- Tree view of the project files
- Click to view file contents with syntax highlighting
- See which files agents have changed (visual indicators)
- Read-only in v0 — editing happens through agents or the terminal

### 7. The Terminal Tab

Direct shell access to the DevPod workspace:
- Full terminal emulator (xterm.js or similar)
- Connected to the devcontainer's shell
- Users can run any command: git, build tools, scripts
- Multiple terminal sessions within the tab (optional for v0)

### 8. Notifications — In-App Badges Only

- Unread badges on session names in the left sidebar
- Badge count shows number of unread messages
- No OS-level notifications in v0 — keep it simple
- Badge clears when you view the session

### 9. Same UI for Everyone

No role-based UI differences. Engineers, PMs, designers, and stakeholders all see the same three-panel layout with all four tabs. The chat-first design naturally accommodates different skill levels:
- Non-engineers gravitate to Chat and Plan tabs
- Engineers use all four tabs
- No configuration overhead for permissions or visibility

---

## Key Decisions

1. **Three-panel layout** — Session list (left), tabbed workspace (center), session summary (right). Information-dense but organized.

2. **Four center tabs: Chat, Plan, Files, Terminal** — Chat is the primary interaction surface. Plan enables collaborative alignment before execution. Files and Terminal provide dev capabilities.

3. **@mention to invoke agents** — Explicit, predictable agent interaction. No auto-routing or magic. Users control when agents engage.

4. **Minimal + expandable agent output** — Brief summaries inline, expand for full details. Keeps the chat readable when agents produce verbose output.

5. **Quick create for new sessions** — Name, repo, agents, go. DevPod workspace spins up in background while users start chatting immediately.

6. **Shared markdown plan** — Simple collaborative document, not a structured task tracker. Agents read it as context. Low complexity, high value.

7. **Same UI for all roles** — No role-based tab hiding or simplified views. Everyone gets the full interface. Chat-first design handles skill-level differences naturally.

8. **In-app badges only** — No OS notifications in v0. Sidebar badges for unread activity. Keep notification system simple.

9. **Onboarding wizard for first launch** — Guided OAuth + team + API key setup. Users don't land on a blank screen.

10. **Plain text + markdown chat input** — Type markdown, it renders on send. No rich editor toolbar, no file attachments in v0. Fast and simple like Slack.

11. **Sessions grouped by project/repo** — Left sidebar groups sessions under their associated repository. Expandable groups. Natural for multi-project teams.

12. **Mutable agent roster** — Add/remove agents from the right panel's Participants section after session creation. Click "Edit agents" to modify.

13. **Open session access** — All team members can see and join all sessions. No per-session permissions in v0. Transparent and zero-friction.

14. **Dark mode only** — Ship with a single dark theme. Most dev tools default to dark. Avoids the design overhead of two themes.

---

## Resolved Questions

1. **Chat input UX** — Plain text + markdown. No toolbar, no attachments. Renders markdown on send.

2. **Session organization** — Grouped by project/repo in the left sidebar with expandable sections.

3. **Agent configuration within a session** — Yes, modifiable after creation via "Edit agents" in the right panel's Participants section.

4. **Session sharing/joining** — All team members see all sessions. No per-session permissions for v0.

5. **Dark mode / theming** — Dark mode only for v0.
