---
title: "feat: DevPod Workspace Logs Panel"
type: feat
status: active
date: 2026-05-08
---

# DevPod Workspace Logs Panel

## Overview

Add a logs icon on the far right of the center panel tab bar that opens a log viewer showing real-time DevPod/devcontainer build output. When a workspace is spinning up (which can take minutes), users can watch the progress instead of staring at a spinner.

## Proposed Solution

Three changes:

1. **Backend**: Stream `devpod up` output line-by-line via WebSocket instead of capturing it all at once
2. **WebSocket**: New `workspace_log` event type that delivers log lines to the frontend
3. **Frontend**: Logs icon in the tab bar (right-aligned, separate from the main tabs) that opens a `LogsView` panel showing streaming log output

## Implementation

### Backend: Stream DevPod Output

**`server/internal/workspace/manager.go`** — Change `Create()` to stream output via a callback instead of `CombinedOutput()`:

```go
// Create now accepts a logFn callback that receives each line of output
func (m *Manager) Create(ctx context.Context, workspaceID, repoURL string, logFn func(line string)) error {
    cmd := exec.CommandContext(ctx, m.bin, args...)
    stdout, _ := cmd.StdoutPipe()
    cmd.Stderr = cmd.Stdout // merge stderr into stdout
    cmd.Start()
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        line := scanner.Text()
        if logFn != nil {
            logFn(line)
        }
    }
    return cmd.Wait()
}
```

**`server/internal/handler/sessions.go`** — Update `startWorkspace()` to pass a log callback that broadcasts each line:

```go
func (h *Handler) startWorkspace(sessionID uuid.UUID, workspaceID, repoURL string) {
    h.workspaces.Create(ctx, workspaceID, repoURL, func(line string) {
        msg, _ := ws.NewServerMessage(ws.TypeWorkspaceLog, sessionID.String(), map[string]string{
            "line": line,
        })
        h.hub.BroadcastToSession(sessionID.String(), msg, nil)
    })
    // ... update status to ready/failed as before
}
```

### WebSocket: New Event Type

**`server/internal/ws/events.go`** — Add:

```go
const TypeWorkspaceLog = "workspace_log"
```

Payload: `{ "line": "Step 3/8 : RUN apt-get install..." }`

### Frontend: Logs Store

**`src/stores/session-store.ts`** — Add:

```typescript
workspaceLogs: Record<string, string[]>  // sessionId -> log lines
appendWorkspaceLog: (sessionId: string, line: string) => void
```

**`src/hooks/use-websocket.ts`** — Handle new event:

```typescript
case "workspace_log": {
    const { line } = msg.payload;
    appendWorkspaceLog(msg.sessionId, line);
    break;
}
```

### Frontend: Logs Icon + LogsView

**`src/components/layout/CenterPanel.tsx`** — Add a `ScrollText` (Lucide) icon button on the far right of the tab bar, separated from the main tabs with `ml-auto`. Clicking it toggles a `LogsView` panel.

Layout of the tab bar:

```
[Chat] [Plan] [Files] [Terminal]              [Logs icon]
```

The logs icon shows a small indicator dot when there's an active workspace build (workspaceStatus === "starting").

**`src/components/logs/LogsView.tsx`** — New component:

- Full height panel (replaces tab content when open, or slides in as a side panel)
- Monospace text, terminal-style dark background (`bg-background-inset`)
- Auto-scrolls to bottom as new lines arrive
- Shows "No logs" empty state when no workspace activity
- Shows all accumulated log lines for the current session
- Clear logs button

### Tab Type

Add `"logs"` to the `TabType` union — or treat it as a separate toggle (not a regular tab) since it's right-aligned and visually distinct. Recommend: separate boolean `showLogs` state, not a tab, so users can flip to logs and back to their previous tab.

## Acceptance Criteria

- [ ] `ScrollText` icon appears on far right of tab bar for every session
- [ ] Icon shows activity indicator when workspaceStatus is "starting"
- [ ] Clicking icon toggles log viewer panel
- [ ] DevPod build output streams line-by-line in real-time via WebSocket
- [ ] Log viewer auto-scrolls, shows monospace text on dark background
- [ ] Logs persist per session (switching sessions shows that session's logs)
- [ ] Empty state when no logs exist: "No workspace logs"
- [ ] `devpod up` errors appear in the log stream (stderr merged with stdout)

## Key Files

| File | Change |
|------|--------|
| `server/internal/workspace/manager.go` | Stream output via callback |
| `server/internal/handler/sessions.go` | Pass log callback to startWorkspace |
| `server/internal/ws/events.go` | Add `TypeWorkspaceLog` constant |
| `src/stores/session-store.ts` | Add `workspaceLogs` state + `appendWorkspaceLog` action |
| `src/hooks/use-websocket.ts` | Handle `workspace_log` event |
| `src/components/layout/CenterPanel.tsx` | Add logs icon to tab bar |
| `src/components/logs/LogsView.tsx` | New log viewer component |
| `src/types/index.ts` | No change needed (logs icon is not a regular tab) |

## Sources

- Workspace manager: `server/internal/workspace/manager.go`
- Tab bar: `src/components/layout/CenterPanel.tsx:66-87`
- WebSocket events: `server/internal/ws/events.go`
- WebSocket hook: `src/hooks/use-websocket.ts`
