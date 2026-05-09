---
title: "feat: Connect terminal tab to DevPod container via WebSocket + PTY"
type: feat
status: active
date: 2026-05-08
origin: docs/brainstorms/2026-05-08-terminal-devpod-connection-brainstorm.md
---

# feat: Connect terminal tab to DevPod container via WebSocket + PTY

## Summary

Replace the mock terminal UI with a real interactive shell connected to the DevPod container. The Go backend spawns `devpod ssh` attached to a PTY, bridges it over a dedicated binary WebSocket endpoint (`/ws/terminal/:sessionId`), and the frontend renders via xterm.js. One shared terminal per session.

---

## Problem Frame

The terminal tab currently renders a fake shell with canned responses. Developers need a real interactive terminal to run commands, debug, and explore the container workspace directly from the Deuce UI.

---

## Requirements

- R1. Developers can type commands in the terminal tab and see real output from the DevPod container
- R2. Terminal supports full ANSI escape codes, colors, cursor movement, and interactive programs (vim, htop, etc.)
- R3. Terminal auto-resizes when the browser panel is resized
- R4. Terminal only connects when workspace status is `"ready"`
- R5. All users in a session share one terminal (one PTY process per session)
- R6. Terminal gracefully handles disconnection and workspace shutdown

---

## Scope Boundaries

- Multiple independent shells per user
- Agent command execution via terminal
- Terminal session persistence/scrollback across page refreshes
- Recording/playback of terminal sessions
- Authentication beyond the existing v0 default-user middleware

---

## Context & Research

### Relevant Code and Patterns

- `server/internal/ws/client.go` — `coder/websocket` usage, `websocket.Accept` with origin patterns, binary message support via `websocket.MessageBinary`
- `server/internal/workspace/manager.go` — DevPod CLI wrapper, `exec.CommandContext` pattern, `bin` field for binary path
- `server/internal/handler/websocket.go` — existing WebSocket handler, single-method pattern
- `server/internal/server/server.go` — chi routing, `r.Get("/ws", ...)` at root level, auth middleware applied globally
- `server/internal/handler/sessions.go:329` — `startWorkspace` uses session **name** (not UUID) as DevPod workspace ID
- `src/components/terminal/TerminalView.tsx` — mock terminal reading `workspaceStatus` from store
- `src/hooks/use-websocket.ts` — native browser WebSocket with auto-reconnect pattern
- `vite.config.ts` — `/ws` proxy rule already covers `/ws/terminal/*` via prefix matching

### Institutional Learnings

No existing learnings in `docs/solutions/` — this is a greenfield capability.

---

## Key Technical Decisions

- **`github.com/creack/pty` for PTY management**: Standard Go PTY library. Provides `pty.Start(cmd)` returning an `*os.File` for the PTY master. Handles resize via `pty.Setsize()`. No PTY library exists in go.mod currently
- **Binary message framing with control prefix**: Use a 1-byte prefix protocol over the WebSocket — `0x00` prefix for terminal data, `0x01` prefix for control messages (resize). This avoids needing separate WebSocket connections or switching between text/binary modes
- **Session name as workspace ID**: DevPod workspaces are created with the session name (not UUID) as the workspace ID. The terminal handler must query the DB to map session UUID → session name for the `devpod ssh` command
- **New handler file `terminal.go`**: Follows the one-file-per-entity pattern. Terminal WebSocket is fundamentally different from the JSON hub — direct PTY bridge, not pub/sub
- **Keep PTY alive on disconnect**: When all WebSocket clients disconnect, the PTY process stays running. This allows reconnection without losing shell state. PTY is killed only when the workspace is stopped/deleted
- **No Vite config changes needed**: The existing `/ws` proxy rule uses prefix matching, so `/ws/terminal/{sessionId}` is already proxied

---

## Open Questions

### Resolved During Planning

- **How to handle resize?** Use binary prefix protocol (`0x01` + JSON `{cols, rows}`) over the same WebSocket. `pty.Setsize()` applies the change to the PTY
- **Where does the terminal session manager live?** New package `server/internal/terminal/` with a `Manager` struct that maps session IDs to PTY file descriptors. Injected into Handler alongside workspace manager

### Deferred to Implementation

- **Exact xterm.js theme/styling**: Match to the existing dark theme CSS variables at implementation time
- **Reconnection UX**: Whether to show a "Reconnecting..." overlay or silently reconnect — decide when building the component

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
Browser (xterm.js)                    Go Backend                           DevPod Container
─────────────────                    ──────────────                        ────────────────
                                     
Terminal tab opened ──── WS connect ──►  /ws/terminal/{sessionID}
                                         │
                                         ├─ Look up session → get workspace name
                                         ├─ Check workspace_status == "ready"
                                         ├─ terminalManager.GetOrCreate(sessionID)
                                         │    ├─ If exists: return existing PTY fd
                                         │    └─ If new: spawn `devpod ssh <name>`
                                         │         └─ pty.Start(cmd) → PTY master fd
                                         │
                                         ├─ Goroutine: PTY stdout → WS (0x00 + data)
                                         └─ Loop: WS → parse prefix
                                              ├─ 0x00: write data → PTY stdin
                                              └─ 0x01: parse {cols,rows} → pty.Setsize()

User types ─── WS binary [0x00|keystroke] ──► stdin → PTY → shell
                                                         │
Shell output ◄── WS binary [0x00|output] ◄──── stdout ◄─┘
                                                         
Resize ──── WS binary [0x01|{cols,rows}] ──► pty.Setsize()
```

**Shared terminal**: The `terminalManager` holds a map of `sessionID → *TerminalSession`. Multiple WebSocket clients fan-in to the same PTY stdin and fan-out from the same PTY stdout. When a new client connects to an existing session, it joins the broadcast — it won't see previous output (scrollback), but it will see all future output.

---

## Implementation Units

### U1. Add `creack/pty` dependency and workspace SSH method

**Goal:** Add PTY library to Go module and extend workspace manager with a method that returns an `*exec.Cmd` for `devpod ssh`.

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `server/go.mod`
- Modify: `server/internal/workspace/manager.go`

**Approach:**
- Run `go get github.com/creack/pty` in `server/`
- Add `SSHCommand(ctx, workspaceID string) *exec.Cmd` method to `Manager` that constructs `exec.CommandContext(ctx, m.bin, "ssh", workspaceID)` without starting it. The caller (terminal handler) attaches the PTY
- Follow the same `exec.CommandContext` pattern used by existing methods like `Create` and `Stop`

**Patterns to follow:**
- `server/internal/workspace/manager.go` — existing `exec.CommandContext` usage

**Test scenarios:**
- Happy path: `SSHCommand` returns a Cmd with correct binary path and args (`["devpod", "ssh", "my-workspace"]`)
- Happy path: `SSHCommand` uses the configured `m.bin` path, not hardcoded `"devpod"`
- Edge case: `SSHCommand` with custom provider does not add provider flags (SSH doesn't need them)

**Verification:**
- `go build ./...` succeeds with the new dependency
- `SSHCommand` method is callable from handler code

---

### U2. Create terminal session manager

**Goal:** Build a concurrency-safe manager that maps session IDs to PTY processes, supporting get-or-create semantics and cleanup.

**Requirements:** R1, R5

**Dependencies:** U1

**Files:**
- Create: `server/internal/terminal/manager.go`

**Approach:**
- `Manager` struct with a `sync.Mutex`-protected map of `sessionID → *Session`
- `Session` struct holds: PTY master `*os.File`, underlying `*exec.Cmd`, list of connected WebSocket writers, a `sync.Mutex` for the writer list
- `GetOrCreate(sessionID string, cmdFactory func() *exec.Cmd) (*Session, error)` — returns existing session or spawns new PTY via `pty.Start()`
- `Resize(sessionID string, cols, rows uint16) error` — calls `pty.Setsize()`
- `AddClient(sessionID string, writer io.Writer)` / `RemoveClient(sessionID string, writer io.Writer)` — manage the fan-out list
- `Close(sessionID string)` — kills the process, closes the PTY fd, removes from map
- PTY stdout is read in a goroutine started by `GetOrCreate` that fans out to all connected writers

**Patterns to follow:**
- `server/internal/ws/hub.go` — mutex-protected maps, goroutine lifecycle

**Test scenarios:**
- Happy path: `GetOrCreate` with new session spawns a PTY and returns a session
- Happy path: `GetOrCreate` with existing session returns the same session (no new process)
- Happy path: `Resize` updates PTY dimensions without error
- Happy path: Multiple clients receive the same PTY output (fan-out)
- Edge case: `RemoveClient` on last client does NOT kill the PTY (stays alive for reconnect)
- Edge case: `Close` kills the process and removes from map; subsequent `GetOrCreate` spawns fresh
- Error path: `GetOrCreate` when the command fails to start returns an error
- Integration: `AddClient` then `RemoveClient` leaves the session in a clean state

**Verification:**
- Terminal manager can spawn a process, pipe data, and clean up without leaking goroutines or file descriptors

---

### U3. Create terminal WebSocket handler

**Goal:** New HTTP handler that upgrades to WebSocket, bridges binary I/O between the client and the PTY via the terminal manager.

**Requirements:** R1, R2, R3, R4, R5, R6

**Dependencies:** U2

**Files:**
- Create: `server/internal/handler/terminal.go`
- Modify: `server/internal/handler/handler.go` (add `terminals *terminal.Manager` field)
- Modify: `server/internal/server/server.go` (register route, create terminal manager, inject into handler)

**Approach:**
- `HandleTerminalWebSocket(w, r)` handler method:
  1. Extract `sessionID` from chi URL param, parse as UUID
  2. Query DB for session to get workspace name and verify `workspace_status = 'ready'`
  3. Call `terminalManager.GetOrCreate(sessionID, cmdFactory)` where `cmdFactory` uses `h.workspaces.SSHCommand()`
  4. Accept WebSocket with `websocket.Accept(w, r, opts)` using binary mode
  5. Register client writer with terminal manager
  6. Read loop: parse first byte prefix — `0x00` → write remaining bytes to PTY stdin; `0x01` → parse JSON resize payload → `terminalManager.Resize()`
  7. Write pump: terminal manager fans out PTY stdout to this client's WebSocket with `0x00` prefix
  8. On disconnect: remove client from terminal manager, close WebSocket
- Route: `r.Get("/ws/terminal/{sessionID}", h.HandleTerminalWebSocket)` at root level (same as `/ws`)
- Create `terminal.Manager` in `server.go` and pass to `handler.New()` — this extends the `handler.New()` signature (currently `New(queries, pool, hub, githubToken, wm)`) with an additional `*terminal.Manager` parameter; update the call site in `server.go` accordingly

**Patterns to follow:**
- `server/internal/handler/websocket.go` — WebSocket upgrade pattern
- `server/internal/ws/client.go` — `websocket.Accept` options, origin patterns
- `server/internal/handler/sessions.go` — chi URL param extraction, DB queries

**Test scenarios:**
- Happy path: WebSocket connects, sends keystrokes, receives shell output
- Happy path: Resize message updates PTY dimensions
- Happy path: Second client connecting to same session joins the existing PTY
- Edge case: Connection attempt when workspace is not `"ready"` returns HTTP error (not a WS upgrade)
- Edge case: Connection attempt with invalid session UUID returns 400
- Edge case: Connection attempt with non-existent session returns 404
- Error path: DevPod SSH command fails to start → WebSocket closed with error message
- Error path: PTY process exits unexpectedly → clients notified, session cleaned up
- Integration: Client disconnect → client removed from fan-out list, PTY stays alive
- Integration: Workspace stopped externally → PTY process dies → clients notified

**Verification:**
- Can open a WebSocket to `/ws/terminal/{sessionID}` and exchange binary data
- Auth middleware is applied (same as existing `/ws` endpoint)
- Multiple clients share the same shell session

---

### U4. Install xterm.js and replace mock terminal

**Goal:** Replace the mock `TerminalView.tsx` with a real xterm.js terminal that connects to the backend WebSocket.

**Requirements:** R1, R2, R3, R4, R6

**Dependencies:** U3

**Files:**
- Modify: `package.json` (add xterm dependencies)
- Modify: `src/components/terminal/TerminalView.tsx` (full rewrite)

**Approach:**
- Install `@xterm/xterm`, `@xterm/addon-fit`, `@xterm/addon-web-links`
- Do NOT use `@xterm/addon-attach` — it doesn't support the binary prefix protocol. Instead, manually wire the WebSocket:
  - `terminal.onData(data)` → prepend `0x00` → send as binary WS message
  - WS `onmessage` → strip `0x00` prefix → `terminal.write(data)`
- Use `addon-fit` to auto-size the terminal to its container. On resize, send `0x01` + JSON `{cols, rows}` over WS
- Observe `ResizeObserver` on the terminal container to trigger `fitAddon.fit()` and send resize messages
- Preserve existing loading/error states from workspace status
- Connect WebSocket only when workspace status is `"ready"` and terminal tab is active
- On unmount or tab switch away: close WebSocket connection (PTY stays alive on backend)
- On tab switch back: reconnect WebSocket (rejoins existing PTY session)
- Import xterm.js CSS in the component

**Patterns to follow:**
- `src/hooks/use-websocket.ts` — WebSocket URL construction (`${protocol}//${host}/ws/terminal/${sessionId}`)
- `src/components/terminal/TerminalView.tsx` — existing workspace status checks

**Test scenarios:**
- Happy path: Terminal renders when workspace is ready, user can type and see output
- Happy path: Terminal resizes when panel is resized (cols/rows update)
- Happy path: Switching to terminal tab connects WebSocket, switching away disconnects
- Happy path: Interactive programs (colors, cursor movement) render correctly via xterm.js
- Edge case: Workspace not ready shows loading spinner (existing behavior preserved)
- Edge case: Workspace failed shows error state (existing behavior preserved)
- Edge case: WebSocket connection lost shows reconnecting state
- Error path: WebSocket fails to connect displays error in terminal area

**Verification:**
- Developer can open terminal tab, type commands, and see real output from the DevPod container
- Terminal visually fits its container and resizes smoothly
- xterm.js CSS is loaded (no unstyled terminal rendering)

---

## System-Wide Impact

- **Interaction graph:** New WebSocket endpoint is independent of the existing `/ws` hub. No callbacks, middleware, or observers are shared beyond the auth middleware. The terminal manager is a new singleton — created in `server.go`, injected into handler
- **Error propagation:** PTY process death should notify connected WebSocket clients via a close frame with a reason. WebSocket disconnection should clean up the client from the terminal manager but not kill the PTY
- **State lifecycle risks:** PTY processes persist even when all clients disconnect. Need cleanup when workspace is stopped — the workspace stop handler in `sessions.go` should call `terminalManager.Close(sessionID)` to kill orphaned PTYs
- **Unchanged invariants:** The existing `/ws` WebSocket hub, JSON event system, and all chat/activity functionality are untouched. The existing `workspaceLogs` streaming for DevPod creation output remains separate

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `devpod ssh` may not be available in all DevPod versions | Check DevPod CLI docs; `devpod ssh` has been stable since v0.4. Fall back to error message if command fails |
| PTY process leak if server crashes without cleanup | PTY processes are children of the Go server process — OS will SIGKILL them when parent dies. For graceful shutdown, add terminal manager cleanup to server shutdown hook |
| Binary prefix protocol is custom, not a standard | Simple 1-byte prefix is trivial to implement and debug. Could migrate to a standard protocol later if needed |
| xterm.js bundle size (~500KB) | Acceptable for a developer tool. Can lazy-load the terminal component if bundle size becomes a concern |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-05-08-terminal-devpod-connection-brainstorm.md](docs/brainstorms/2026-05-08-terminal-devpod-connection-brainstorm.md)
- `server/internal/ws/` — existing WebSocket infrastructure
- `server/internal/workspace/manager.go` — DevPod CLI wrapper
- `src/components/terminal/TerminalView.tsx` — current mock terminal
- `github.com/creack/pty` — Go PTY library
- `@xterm/xterm` — browser terminal emulator
