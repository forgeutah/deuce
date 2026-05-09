# Terminal-to-DevPod Container Connection

**Date:** 2026-05-08
**Status:** Brainstorm complete

## What We're Building

A live interactive terminal in the Deuce UI that connects to the DevPod container workspace for each session. Developers get a real shell (not a mock) — they can type commands, see output, and interact with the container environment directly from the terminal tab.

## Why This Approach

### Connection: `devpod ssh`

Use `devpod ssh <workspace-id>` on the backend to spawn a shell session. This leverages DevPod's built-in SSH tunneling — no need to manage SSH keys, know container IDs, or tie ourselves to Docker specifically. The workspace manager already wraps the DevPod CLI, so adding an `SSH()` method is a natural extension.

### Transport: Dedicated WebSocket endpoint

A new `/ws/terminal/:sessionId` endpoint carries raw terminal I/O (binary). This cleanly separates terminal data from the JSON chat/event WebSocket at `/ws`. Benefits:
- xterm.js attach addon works naturally with a dedicated WS
- No base64 encoding overhead
- Simpler lifecycle management (connect when terminal tab opens, disconnect when it closes)
- No routing complexity in the existing hub

### Frontend: xterm.js

Replace the current mock terminal with xterm.js — the standard browser terminal emulator. It handles ANSI escape codes, cursor movement, colors, resize events, and everything a real terminal needs. The `@xterm/addon-attach` addon connects directly to a WebSocket.

### Multi-user: One shared terminal per session

All users in a session see the same shell. One person types, others watch. Keeps the initial implementation simple and matches the collaborative session model. The backend spawns one `devpod ssh` process per session (not per user).

## Key Decisions

1. **`devpod ssh`** over docker exec or raw SSH — stays provider-agnostic, reuses DevPod's infrastructure
2. **Dedicated WebSocket** over multiplexing on existing `/ws` — clean separation of binary terminal data from JSON events
3. **xterm.js** for terminal emulation — industry standard, handles all terminal complexity
4. **One shared terminal per session** — simpler, collaborative, matches the session model
5. **Human interactive shell** as primary use case — full terminal emulation, not just command output viewing

## Architecture Sketch

```
┌─────────────┐     WebSocket (binary)      ┌──────────────┐     stdin/stdout     ┌─────────────┐
│  Browser     │  /ws/terminal/:sessionId    │  Go Backend  │   devpod ssh <id>   │  DevPod     │
│  xterm.js    │ ◄─────────────────────────► │  PTY bridge  │ ◄──────────────────► │  Container  │
└─────────────┘                              └──────────────┘                      └─────────────┘
```

**Data flow:**
1. User opens terminal tab → frontend opens WebSocket to `/ws/terminal/:sessionId`
2. Backend receives connection → spawns `devpod ssh <workspace-id>` with a PTY
3. User keystrokes → WS → stdin of ssh process
4. SSH process stdout → WS → xterm.js renders output
5. Terminal resize → WS control message → PTY resize (`SIGWINCH`)

## Components to Build

### Backend
- `workspace.Manager.SSH()` — spawns `devpod ssh <workspace-id>` attached to a PTY
- Terminal handler — new WebSocket endpoint that bridges WS ↔ PTY stdin/stdout
- Terminal session manager — tracks one PTY process per session, handles cleanup on disconnect
- Resize support — parse resize control messages, apply to PTY via `ioctl`

### Frontend
- Install `@xterm/xterm` + `@xterm/addon-attach` + `@xterm/addon-fit`
- Replace `TerminalView.tsx` mock with real xterm.js instance
- Connect to `/ws/terminal/:sessionId` when workspace is ready
- Handle resize via `addon-fit` → send dimensions over WS
- Show loading/error states based on workspace status (already exists)

### Vite Config
- Add proxy rule for `/ws/terminal` → backend

## Open Questions

None — all key decisions resolved.

## Out of Scope (for now)
- Multiple independent shells per user
- Agent command execution via terminal
- Terminal session persistence/scrollback across reconnects
- Recording/playback of terminal sessions
