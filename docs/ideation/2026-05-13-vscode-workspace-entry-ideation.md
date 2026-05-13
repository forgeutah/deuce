---
date: 2026-05-13
topic: vscode-workspace-entry
focus: VS Code button to open workspace remotely in local VS Code; another button for hosted browser VS Code; ultimately a shared live-code session link with everyone's cursors
mode: repo-grounded
---

# Ideation: Multi-Modal Editor Entry for Deuce Sessions

## Grounding Context

**Codebase context:**
- Sessions in Deuce are 1:1 with DevPod workspaces. `server/internal/workspace/manager.go` shells out `devpod up <repo> --id <sessionID> --ide none` today. DevPod also supports `--ide vscode` (writes SSH config, hands off to Remote-SSH) and `--ide openvscode` (starts OpenVSCode Server on port 3000 inside the container).
- Session `workspace_status`: starting / ready / failed / suspended. Tabs today: chat, plan, files, terminal. There is no editor entry button yet.
- WebSocket hub (`server/internal/ws/hub.go`) broadcasts per-session subscriptions: `new_message`, `agent_status`, `typing_indicator`, `activity_update`, `session_update`, `unread_update`, `workspace_log`. Adding new event types is a clean extension.
- Frontend: React 19 + Vite, Zustand single store, shadcn/ui + Tailwind v4, dark-mode-only.
- API pattern: SQL → `make generate` → handler → register route → wrap in `src/lib/api.ts`.
- Auth (v0): no real auth — middleware injects default user ID from `DEUCE_USER_ID` env var.

**Product strategy:**
- STRATEGY.md Track 3 ("Coding & Preview") lists **"Collaborative VS Code" unchecked** — this ideation feeds directly into that track.
- Agent-native parity is mandated: every human-callable action must also be agent-callable.

**External context:**
- `vscode://vscode-remote/ssh-remote+<host>/<path>` is NOT a stable deep-link scheme (vscode-remote-release issue #4779). Gitpod, Coder, and DevPod all rely on a local binary + companion extension instead.
- VS Code Live Share is desktop-only, gated on a Microsoft/GitHub account, capped at 30 guests, and explicitly unavailable in code-server / OpenVSCode Server / Cursor because Microsoft began enforcing extension licensing in April 2025. It is a non-starter for any browser-based shared-editor path.
- Yjs + Monaco (and Yjs + `y-codemirror.next`) is the proven CRDT path for browser multi-cursor; Liveblocks Yjs is a managed relay. Replit and CodeSandbox ship every project as multiplayer by default.
- Cursor has no multiplayer in 2026; Zed has native multiplayer but isn't VS Code.

**Past learnings:** `docs/solutions/` does not yet exist — no institutional learnings to apply. Any solutions landed here become net-new institutional knowledge worth capturing with `/ce-compound`.

## Topic Axes

1. Editor entry mechanics — buttons, URIs, deep links, tunnel URLs, fallbacks
2. Workspace readiness & lifecycle — DevPod IDE flag selection, port plumbing, pre-warm, hibernation/wake
3. Co-presence & live collaboration — multi-cursor, awareness, Live Share vs Yjs, who's-in-the-editor
4. Identity, auth, and security — signed link semantics, access scope, SSH agent forwarding, share-link revocation
5. Agent-native parity & integration — agents in the editor; bridges between chat/plan tabs and the editor

## Architecture Decision (2026-05-13)

After examining the tradeoff between embedding Yjs inside hosted VS Code vs shipping a custom Monaco pane, **Path B was chosen**: Deuce ships its own Monaco-based (or CodeMirror with `y-codemirror.next`) collaborative editor pane wired directly to Yjs. The full hosted-VS-Code-in-browser surface is dropped as the *primary* live-editing target because Monaco inside VS Code is owned by the workbench and can't be directly bound to a Yjs document with the clean `y-monaco` library — the workaround (custom VS Code extension that programmatically syncs selections via the editor API) is significantly harder and gives a worse result.

This makes the surface map:

- **Local VS Code** (heavy users with their own setup): `deuce join <url>` CLI handoff using DevPod's SSH config writer. Real VS Code with all extensions. Live cursors of others render via a Deuce companion VS Code extension as **presence-only decorations** (no CRDT editing across this boundary).
- **Deuce collaborative pane** (in-app, anyone): Monaco + Yjs in a Deuce session tab. Real multi-cursor CRDT editing. The agent participates as a Yjs client. This is where idea #7 (agent-first presence) gets its strongest expression.
- **Hosted browser VS Code** (presence-only fallback): OpenVSCode Server via `devpod up --ide openvscode` for users who want a fuller IDE in-browser but don't need live editing. Same presence-decoration extension as the local-VS-Code path.

## Ranked Ideas

### 1. Capability-detected single editor button
**Description:** Replace three competing buttons with one "Open editor" affordance that probes the client and remembers the choice per-device. Detects local DevPod CLI + VS Code (or Cursor / Zed / JetBrains) → hand off via `deuce join <url>` CLI (avoids the unstable `vscode://ssh-remote+host` scheme). Detects browser-only / mobile / locked-down corp laptop → open Deuce's collaborative pane in-app. Already a teammate live in this session → join the same surface they're on. Show a presence-aware sheet ("Alice and the Coder agent are in the Deuce pane — join them, or open in your desktop VS Code").
**Axis:** 1
**Basis:** `external:` vscode-remote-release issue #4779 — no stable SSH deep-link scheme; Gitpod/Coder/Codespaces all use a companion CLI/extension. `direct:` Zustand store already tracks session membership.
**Rationale:** Three peer buttons forces users to know which mode they want before they understand what the modes mean — and the brittle `vscode://` path makes "Open in Local VS Code" fail silently on a meaningful fraction of machines. One probed, remembered choice plus a stable CLI handoff routes everyone to a working editor.
**Downsides:** First-launch capability probe adds a UX moment. Per-device cache invalidation is its own minor problem. Adds the `deuce` CLI as a new shipping artifact.
**Confidence:** 80%
**Complexity:** Medium
**Status:** Unexplored

### 2. Deuce-native Monaco + Yjs collaborative pane (Path B)
**Description:** Ship a Monaco-based code editor pane inside Deuce sessions, wired to Yjs via `y-monaco` (or CodeMirror with `y-codemirror.next`). Awareness layer (cursors, selection, names) keys to Deuce identity. Uses Liveblocks-managed Yjs for v1 to skip running a `y-websocket` server, with a path to self-hosted later. File sync between the Yjs document and the DevPod container filesystem happens via a server-side watcher that reconciles edits both directions.
**Axis:** 3
**Basis:** `external:` Microsoft April 2025 extension enforcement makes Live Share unavailable in browser distros — the alternative is to own the editor surface. Yjs+Monaco + Liveblocks is the well-trodden CRDT path (days-to-ship). `direct:` STRATEGY.md Track 3 has "Collaborative VS Code" unchecked.
**Rationale:** The asked feature ("shared live-coding link with everyone's cursors") cannot be reached via Live Share in any browser surface. Owning multiplayer in-house removes the dependency and is a real competitive differentiator (Cursor still has none).
**Downsides:** Heaviest individual lift in this set. Extension parity is capped — no VS Code marketplace, no debugger UI, no Copilot inside the pane. Filesystem ↔ Yjs reconciliation is a real engineering problem (concurrent agent edits to disk vs human edits in Yjs).
**Confidence:** 75%
**Complexity:** High

### 3. Presence & cursor event stream as a shared bus
**Description:** Add a typed `presence_update` WebSocket event (principal, session, file, line range, selection, last_active_at). The Deuce collaborative pane, the local-VS-Code companion extension, the files panel, the terminal, the agent activity feed, and chat all publish to and subscribe to the same stream. Agents emit it when they edit; humans emit it when they move.
**Axis:** 3
**Basis:** `direct:` `server/internal/ws/hub.go` already supports per-session subscriptions and broadcasts `activity_update`. `direct:` STRATEGY.md mandates agent-native parity.
**Rationale:** Decoupling presence from CRDT text sync means features fall out for nearly free once this exists: avatars in the files tree, jump-to-teammate from chat, agent-aware editing warnings, session replay. It's also the only mechanism that bridges the Yjs pane and the local-VS-Code surface, since they can't share a CRDT document but can share presence.
**Downsides:** Event versioning matters up front. Backpressure / throttling at scale is non-trivial. Agents emitting too aggressively can be noisy.
**Confidence:** 85%
**Complexity:** Medium

### 4. Workspace coordinate URLs as the universal "where" primitive
**Description:** Define `deuce://session/<id>/file/<path>#L<line>:<col>` (plus HTTPS equivalent). Every surface that points at code — chat, plan tab, files panel, agent activity, PR review comments, replay timestamps — emits this. A single resolver decides where to open it based on the user's capability profile from idea #1.
**Axis:** 1
**Basis:** `reasoned:` without a unified link grammar, each surface re-invents the bridge. `external:` VS Code's `vscode://file/<path>` proves the pattern.
**Rationale:** The cheap primitive that makes agent-to-human handoff feel obvious — Reviewer agent posts three coordinate links in chat; clicking each opens the right file at the right cursor in whichever editor the user chose.
**Downsides:** URL grammar is sticky once links are in databases or chat history. Path-stability across renames/branches needs design.
**Confidence:** 90%
**Complexity:** Low

### 5. Signed short-link service as a reusable capability
**Description:** Build one signed-link primitive: `/l/<short>` resolves to a coordinate URL plus a scoped auth claim (principal, session, TTL, capability — read-only / single-file / full editor). Use it first to hand the hosted-VS-Code-fallback path an authenticated token (an open OpenVSCode Server port is RCE-as-a-service). Reuse it everywhere a Deuce surface needs scoped time-bounded access.
**Axis:** 4
**Basis:** `direct:` CLAUDE.md states current auth is `DEUCE_USER_ID` env var only — won't survive a second user. `external:` unauth'd code-server is a documented public-internet exposure pattern.
**Rationale:** Without this, the browser-fallback button can't ship outside a single trusted developer. With it, the editor is the first consumer; every later sharing feature inherits a single audit log and revocation surface.
**Downsides:** JWT and revocation infrastructure now in scope (key rotation, blacklist, clock skew). Easy to over-design for v1.
**Confidence:** 85%
**Complexity:** Medium

### 6. Pre-warmed workspaces with `workspaceReady` promise
**Description:** Two halves of the same idea: (a) the moment a session is created, the DevPod workspace begins provisioning — repo cloned, deps installed, OpenVSCode Server bound (for fallback path), Yjs document initialized; (b) the entire frontend awaits a single `session.workspaceReady` observable in the Zustand store instead of inventing per-feature loading states. The Deuce pane, the terminal, the files panel, agent dispatch, and the line-by-line `devpod up` log indicator all gate on it.
**Axis:** 2
**Basis:** `direct:` `server/internal/workspace/manager.go` already shells to DevPod; lifecycle states exist but aren't surfaced uniformly. `direct:` `workspace_log` events already broadcast. `external:` Codespaces, Gitpod, and Replit all moved to prebuild/pre-warm because cold-start is the silent UX killer.
**Rationale:** Cold start is the worst 30-120 seconds of any cloud-IDE product. Pre-warm removes the wait; the unified promise removes a class of tab-specific loading bugs. Together they make the editor button feel "instant" and they make idea #7 viable — the agent has somewhere to live the moment a session opens.
**Downsides:** Pre-warming idle workspaces costs compute that may not be used. Coupling all tabs to one observable means a stuck workspace can paralyze the UI if the loading state isn't well-designed.
**Confidence:** 75%
**Complexity:** Medium

### 7. Agent-first presence — the agent is already in the editor
**Description:** Invert the conventional mental model. When a session is created (and the workspace pre-warms per idea #6), the Coder agent immediately enters the Deuce collaborative pane as a real Yjs client. Its cursor is the first cursor in the room. When a human "opens the editor" via idea #1's button, they are joining an in-progress live session where an agent is already typing. There is no empty-editor state. For humans on local VS Code, the agent's location renders via the companion extension's presence decorations (idea #3) — same cursor logical position, projected into the surface they're attached to.
**Axis:** 5
**Basis:** `direct:` STRATEGY.md mandates agent-native parity: "every human action must be agent-callable." `reasoned:` agent-first presence makes parity structural rather than cosmetic and provides automatic proof that the workspace is warm.
**Rationale:** The framing that makes Deuce's agent-native claim visible the moment a session opens. Collapses three things into one experience: pre-warm becomes visibly useful (the agent is using the workspace), the first human's editor never has an empty state, and "Reviewer is at `api.ts:47` — follow it" is the default UX, not a feature you build later.
**Downsides:** Distinct product opinion that some users may find unsettling ("why is something already typing?"). Requires careful pacing so agent activity isn't background noise. Needs a "calm mode" for solo work.
**Confidence:** 70%
**Complexity:** Medium
**Status:** Explored

## Rejection Summary

| # | Idea | Reason |
|---|------|--------|
| 1 | `deuce join <url>` CLI handoff | Absorbed into #1 as the local-editor path's implementation. |
| 2 | Editor-agnostic entry (Cursor / Zed / JetBrains / Neovim) | Expands scope past the user's "VS Code" framing — fold into #1's capability probe as alternate detection targets later. |
| 3 | Yjs inside hosted browser VS Code | Architecturally infeasible without a heroic custom VS Code extension (Monaco-in-VS-Code isn't directly Yjs-bindable). Replaced by Path B (idea #2). |
| 4 | Browser VS Code as a diff viewer, not a workstation | Reasonable reframe but conflicts with Path B; the hosted VS Code surface becomes a presence-only fallback instead. |
| 5 | Auth determines surface, not just access | Sharp reframe — folded into #5 as capability flags on signed links. |
| 6 | Knock-to-join replaces URL share | Interesting social-access pattern but solved by #5's scoped tokens. Future auth ideation. |
| 7 | Warm-resume indicator (line-by-line `devpod up` logs) | Subsumed by #6 — the `workspaceReady` promise carries the loading-state UI. |
| 8 | Heartbeat-driven tunnel reaper with reconnect toast | Engineering hygiene; below meeting-test floor. Implement as silent infra. |
| 9 | Per-second cost meter with aggressive reaper | Constraint-flip exercise; no current billing model — premature. |
| 10 | Driver / navigator / spectator roles | Yjs awareness already gives most of the value; explicit roles add UI complexity without a forcing function. Revisit at 30+ humans/session. |
| 11 | Attach sessions (tmux for IDEs) | Implementation is a heroic project; key insight covered by #3 + #6. |
| 12 | Persistent workspaces, ephemeral sessions | Architectural session/workspace reframe — too far beyond editor entry; deserves its own ideation. |
| 13 | Agent-callable editor as a first-class MCP tool | Falls out of #4 + #5 + #3 + #7 together; not a separate primitive. |
| 14 | Session black box / replay | Nearly-free consequence of #3's presence stream — downstream feature, not a survivor of this ideation. |
| 15 | Async-first edit queue | Scope overrun — conflicts with the user's stated live-cursor goal. |
| 16 | Single-user local-only mode | Below ambition: a v1 sequencing decision, not an idea. |
| 17 | Chat box and editor are the same surface | Scope overrun; dissolves editor-entry into a broader Deuce UX redesign. |
| 18 | Workspace spectator stream (Twitch + chat overlay) | Solves a different problem (audience at scale, not team collaboration). Park for later. |
