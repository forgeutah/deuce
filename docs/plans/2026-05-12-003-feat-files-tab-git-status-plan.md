---
title: Files tab with git status
type: feat
status: completed
date: 2026-05-12
origin: docs/brainstorms/2026-05-12-files-tab-git-status-requirements.md
---

# Files tab with git status

## Summary

Add a new files handler in `server/internal/handler/files.go` that reads the DevPod workspace's bind-mount path directly from the Deuce host's filesystem (no SSH), walks the tree, discovers nested git repos via `.git/` directories, and runs `git status --porcelain` per repo. Frontend extends the existing Zustand lazy-load pattern, adds a `gitStatus` field to `FileNode`, surfaces four states via the existing GitHub Primer tokens, and wires refresh on tab open, session switch, and (debounced) WebSocket activity events.

---

## Problem Frame

The Files tab in `CenterPanel` is wired but always empty — no backend endpoint populates the Zustand `fileTrees` slice that [src/components/files/FilesView.tsx](../../src/components/files/FilesView.tsx) renders. Today users alt-tab to the Terminal tab and run `git status` / `ls` to see what an agent has done. See origin doc for the full pain narrative.

---

## Requirements

- R1. Opening the Files tab on a session with a `ready` workspace populates the tree within a couple of seconds.
- R2. Tracked-and-dirty files render as **M** (warning/yellow), untracked files as **U** (success/green), staged files as **A** (success-emphasis), deleted files as **D** (danger/red) with single-letter badges and color.
- R3. A workspace containing a top-level repo and nested clones shows both; each file's status comes from its **nearest enclosing repo**, never an ancestor.
- R4. Sub-repo root directories are **always visible** in the tree, even when a parent's `.gitignore` would exclude them, and carry a `[repo]` marker.
- R5. Clicking a file fetches and displays its content in the existing right-hand viewer pane. Text files render; binary files show a placeholder.
- R6. The tree refreshes on tab open, on session switch, on WebSocket `activity_update` events (debounced), and on a manual refresh button.
- R7. The existing `modifiedBy` agent-attribution dot continues to render alongside git colors when populated.
- R8. Existing `workspaceStatus` gates (`starting` / `failed`) in `FilesView` are preserved — the tree only loads when the workspace is `ready`.

---

## Scope Boundaries

- No inline diff view of changed files (status colors only)
- No file search / fuzzy filter within the tree
- No in-browser editing
- No display of `.gitignore`'d files other than sub-repo roots
- No filesystem watcher / WebSocket push for file-change events
- No multi-workspace aggregation
- No automated tests (repo has no test runner per `CLAUDE.md`; verification is `tsc --noEmit` + manual smoke)

### Deferred to Follow-Up Work

- **`modifiedBy` agent-attribution producer**: backend will leave the field empty in v1. Populating it requires tracking per-session agent file edits — a separate concern best addressed when the agent execution path lands proper file-write telemetry. (Confirmed scope decision; UI keeps the rendering logic intact so it lights up when the producer ships.)
- **`file-change` activity type**: today's `activity_update` events all carry `type: "agent-action"`. v1 refreshes on every activity event with a debounce. A future PR may add a `file-change` activity type in `server/internal/handler/messages.go` so the frontend can filter precisely.
- **Folder-rollup status badges**: subtle indicator on folders that contain dirty descendants. Easy to add later.

---

## Context & Research

### Relevant Code and Patterns

- **Handler convention**: [server/internal/handler/messages.go](../../server/internal/handler/messages.go) `ListMessages` and [server/internal/handler/activities.go](../../server/internal/handler/activities.go) `ListActivities` — both parse `chi.URLParam(r, "sessionID")` into `uuid.UUID`, use `writeError(w, status, "CODE", "message")` helpers, and return camelCase JSON. Mirror this shape.
- **Route registration**: [server/internal/server/server.go:80-104](../../server/internal/server/server.go#L80-L104) under `r.Route("/api", ...)` → `r.Route("/sessions/{sessionID}", ...)`. Drop the new routes next to `r.Get("/activities", h.ListActivities)`.
- **Workspace lookup**: `Handler` already holds `workspaces *workspace.Manager` and DB `queries`. The session row has a workspace ID (used elsewhere to start/stop workspaces). Reuse that lookup.
- **DevPod bind-mount path**: For the docker provider, the workspace content lives at `$HOME/.devpod/agent/contexts/<context>/workspaces/<workspace-id>/content/` on the Deuce host's filesystem. Verified empirically (kol-5324 workspace inspected via `docker inspect`). The workspace container bind-mounts this path to `/workspaces/<workspace-id>`.
- **Frontend lazy-load**: [src/stores/session-store.ts:83-101](../../src/stores/session-store.ts#L83-L101) `setActiveSession` does fire-and-forget loads for messages and activities. Mirror for files.
- **WebSocket dispatch**: [src/hooks/use-websocket.ts:116-123](../../src/hooks/use-websocket.ts#L116-L123) handles `activity_update`. Hook the refresh in next to `addActivity`.
- **Color tokens** ([src/styles/globals.css](../../src/styles/globals.css)): `text-warning` (#d29922) for M, `text-success` (#3fb950) for U, `text-success-emphasis` (#238636) for A, `text-danger` (#f85149) for D. All wired through Tailwind v4 `@theme inline`.

### Institutional Learnings

- `docs/solutions/` does not exist. No prior learnings apply.

### External References

- None needed; entirely conventional patterns within the existing codebase.

---

## Key Technical Decisions

- **Read filesystem locally, no SSH**: The DevPod docker provider bind-mounts the workspace content at a predictable host path. The Deuce server can read it directly with Go stdlib (`filepath.WalkDir`, `os.ReadFile`, `exec.Command("git", "status")` with `cmd.Dir = repoPath`). Skips shell escaping, JSON-in-shell marshaling, SSH connection overhead, and gives us standard `context.WithTimeout` for free.
- **Two endpoints, not one**: `GET /api/sessions/{sessionID}/files` returns the full tree+status; `GET /api/sessions/{sessionID}/files/content?path=...` returns a single file's content. The tree response stays small and cacheable; content is fetched only when a file is clicked.
- **Sub-repo discovery via filesystem walk**: `filepath.WalkDir` from the workspace root. When `.git/` is found, mark the parent as a repo root, stop descending into `.git/`, and run `git status --porcelain` rooted at that directory. A parent repo's `.gitignore` is honored for ordinary children but explicitly overridden for sub-repo roots.
- **Walk prune list (hardcoded for v1)**: `.git`, `node_modules`, `dist`, `build`, `.next`, `.turbo`, `target`, `__pycache__`, `.venv`. Sub-repo roots inside these names are still surfaced when discovered (the prune list is for performance, not visibility — though in practice these are almost never sub-repos).
- **Per-request timeout**: wrap with `context.WithTimeout(r.Context(), 10*time.Second)`. The `find` + per-repo `git status` work is bounded and shouldn't approach this; if it does, fail with a clear error.
- **WS refresh: debounce, no filtering yet**: `activity_update` currently fires for every agent message. v1 refreshes on every event with a 500ms trailing debounce (per-session). Deferred follow-up may add a `file-change` activity type for precise filtering.
- **`gitStatus` enum on `FileNode`**: `"M" | "U" | "A" | "D" | undefined`. Single-letter, matches the porcelain output. Surfaced as both a colored badge slot and as the row's text color tint.
- **Sub-repo marker**: new `isRepoRoot?: boolean` field on `FileNode`. UI renders a small `[repo]` chip next to the folder name when true.
- **Binary detection**: null-byte sniff in the first 8KB of file content. Cheap, effective, and matches what `git` itself does.
- **File content size cap**: 1 MB. Larger files return a `truncated: true` marker with the first 1 MB. UI shows a "file too large to preview" message.
- **No `RunInWorkspace` helper needed**: the SSH-based approach in the brainstorm would have wanted a thin wrapper. Local-FS doesn't.
- **Provider assumption**: docker provider only. Documented in Risks below.

---

## Open Questions

### Resolved During Planning

- *Endpoint shape (combined vs split)*: split — tree+status combined into one endpoint, content into a separate per-file endpoint.
- *Walk strategy*: pure Go `filepath.WalkDir` on the bind-mount path, no SSH.
- *Bound walk cost*: hardcoded prune list inside the walk; sub-repos discovered first and recursed into separately.
- *Color tokens per state*: assigned to the existing Primer warning/success/success-emphasis/danger tokens.
- *Tree shape on the wire*: nested `FileNode[]` (children inline). The walk assembles the tree server-side rather than emitting flat and re-nesting client-side.

### Deferred to Implementation

- *Exact `workspace_result.json` parsing or env-derived path lookup*: implementation may choose to read `$HOME/.devpod/agent/contexts/<context>/workspaces/<id>/content` directly via env-derived `HOME` + a `default` context, or to parse the workspace metadata for the canonical content path. Pick during implementation; the contract is "given a workspace ID, return its filesystem path on this host."
- *Debounce mechanism for WS refresh*: a per-session timer in the websocket hook, or in the store. Pick the simpler one when wiring.
- *Whether to keep `src/mocks/data/seed.ts`'s `fileTreesBySession`*: it overwrites cleanly once the API returns real data, so it can stay or be removed. Deferred to implementation taste.

---

## Implementation Units

### U1. Backend tree+status endpoint

**Goal:** Add `GET /api/sessions/{sessionID}/files` returning the workspace's file tree with per-file git status.

**Requirements:** R1, R2, R3, R4, R8

**Dependencies:** None

**Files:**
- Create: `server/internal/handler/files.go`
- Modify: `server/internal/server/server.go` (register new route under `/sessions/{sessionID}`)

**Approach:**
- Resolve session → workspace ID via existing `Handler.queries`. Reject with 400 / `INVALID_SESSION_ID` for bad UUIDs, 404 / `SESSION_NOT_FOUND` for unknown sessions, and 409 / `WORKSPACE_NOT_READY` when the session's workspace isn't in a `ready` state.
- Derive the workspace content path: `<DEVPOD_AGENT_CONTENT_DIR>/<workspaceID>/content`, where `DEVPOD_AGENT_CONTENT_DIR` is an env var holding the base workspaces directory (default `${HOME}/.devpod/agent/contexts/default/workspaces`). The implementation appends `/<workspaceID>/content` to the env value. Stat-check the resulting directory; 404 / `WORKSPACE_NOT_FOUND` if missing.
- Wrap the request in `context.WithTimeout(r.Context(), 10*time.Second)`.
- Walk the tree with `filepath.WalkDir`:
  - Skip directories in the prune list (`.git`, `node_modules`, `dist`, `build`, `.next`, `.turbo`, `target`, `__pycache__`, `.venv`).
  - When encountering a `.git` entry that is itself a directory, mark the parent as a repo root, record the repo path, and prevent its own `.gitignore` from later hiding the root from any enclosing walk.
  - Build nested `FileNode` structures with `id` (path-derived), `name`, `path` (repo-relative from workspace root), `type`, `children`, `isRepoRoot`.
- For each discovered repo root, run `exec.CommandContext(ctx, "git", "status", "--porcelain=v1")` with `cmd.Dir = absoluteRepoPath`. Parse the porcelain output into a map of `relativePath → gitStatus`. Apply each map only to nodes inside its repo (not to siblings in the parent repo).
- Map porcelain codes:
  - `??` → `U`
  - `A ` or ` A` → `A`
  - ` D` or `D ` → `D`
  - ` M`, `M `, `MM` → `M`
  - Renames (`R `): emit the new path with `A`; ignore the old path. (V1 keeps the model simple.)
- Marshal the assembled tree as the response. JSON tags camelCase per repo convention.

**Patterns to follow:**
- Handler shape mirrors `ListMessages` in `server/internal/handler/messages.go` — same `chi.URLParam`, `writeError`, `writeJSON` pattern.
- Use the same `to<Entity>Response` mapper style for `toFileNodeResponse` if a separate response struct is preferred.

**Test scenarios:** *(manual smoke scenarios — no automated test runner per CLAUDE.md)*
- Covers R1. Happy path: hit `/files` for a session with a clean repo. Tree loads, no nodes have a `gitStatus`, response shape matches `FileNode[]`.
- Covers R2. Happy path: touch a tracked file in the workspace, hit `/files`, that file's `gitStatus` is `M`. Add a new file, status is `U`. `git add` it, status is `A`. `git rm` a tracked file, status is `D`.
- Covers R3, R4. Sub-repo scenario: clone a second repo into a `.gitignore`'d subdirectory of the workspace. `/files` returns both repos, the sub-repo root is visible with `isRepoRoot: true`, and modifying a file in the sub-repo shows `gitStatus: "M"` while parent-repo files remain unaffected.
- Edge case: empty workspace (just a `.git/` and nothing else). Response is `[]` plus the repo root entry, no errors.
- Edge case: workspace with no git repos at all (just files). Tree returns all files with no `gitStatus`.
- Edge case: file path with spaces. Path round-trips correctly in the response.
- Error path: invalid session UUID → 400 `INVALID_SESSION_ID`.
- Error path: nonexistent session → 404 `SESSION_NOT_FOUND`.
- Error path: session whose workspace isn't ready → 409 `WORKSPACE_NOT_READY`.
- Error path: simulated long-running walk (large workspace with prune list disabled) → request times out at 10s with a clean error envelope.
- Integration: `git status` failure inside one of several sub-repos doesn't tank the whole response; that repo's files come back without `gitStatus`, others are unaffected.

**Verification:**
- Hitting the endpoint with `curl` against a running session returns a JSON tree matching `FileNode[]`.
- `tsc --noEmit` passes on `src/types/index.ts` after U3 lands.
- All four porcelain states render in the response when manually induced.

---

### U2. Backend file content endpoint

**Goal:** Add `GET /api/sessions/{sessionID}/files/content?path=<relative>` returning a single file's content.

**Requirements:** R5

**Dependencies:** U1 (shares path resolution helper)

**Files:**
- Modify: `server/internal/handler/files.go` (add `GetFileContent` handler)
- Modify: `server/internal/server/server.go` (register `/files/content` route)

**Approach:**
- Reuse the workspace-path resolution from U1. Factor it into an unexported helper in the same file (`resolveWorkspaceContentPath`) if not already done.
- Validate the `path` query param: reject if empty, absolute, or contains `..` segments. 400 `INVALID_PATH` for any of these.
- Join with the workspace root using `filepath.Join` and re-check the result is still inside the workspace (defense-in-depth against escapes).
- Stat the file; 404 `FILE_NOT_FOUND` if missing, 400 `PATH_IS_DIRECTORY` if it's a directory.
- Read up to 1 MB. Detect binary by scanning the first 8 KB for a null byte.
- Response shape: `{ path, content (string, may be empty), isBinary (bool), truncated (bool), size (int) }`.

**Patterns to follow:**
- Same handler conventions as U1. Same error-envelope shape.

**Test scenarios:** *(manual smoke)*
- Covers R5. Happy path: fetch a small text file, content matches, `isBinary: false`, `truncated: false`.
- Happy path: fetch a 2 MB text file, content is truncated to 1 MB, `truncated: true`, `size` reflects actual file size.
- Edge case: fetch an empty file → `content: ""`, no error.
- Edge case: fetch a file whose first byte is null (e.g., a `.png`) → `isBinary: true`, `content: ""`.
- Edge case: fetch a path with `%20` (URL-encoded space) → resolves correctly.
- Error path: missing `path` query param → 400 `INVALID_PATH`.
- Error path: `path=/etc/passwd` (absolute) → 400 `INVALID_PATH`.
- Error path: `path=../../etc/passwd` (traversal) → 400 `INVALID_PATH`.
- Error path: well-formed path to a nonexistent file → 404 `FILE_NOT_FOUND`.
- Error path: path resolves to a directory → 400 `PATH_IS_DIRECTORY`.
- Error path: session not ready → 409 `WORKSPACE_NOT_READY` (consistent with U1).

**Verification:**
- `curl '/api/sessions/<id>/files/content?path=README.md'` returns the file content as JSON.
- Traversal and absolute-path attempts return 400 with no file content leaked.

---

### U3. FileNode type + API client

**Goal:** Extend the shared `FileNode` type with the new fields and add typed API wrappers.

**Requirements:** R2, R4, R5

**Dependencies:** None (can land before or in parallel with U1/U2)

**Files:**
- Modify: `src/types/index.ts` (add `gitStatus`, `isRepoRoot` to `FileNode`; add `FileContentResponse`)
- Modify: `src/lib/api.ts` (add `listFiles`, `getFileContent`)

**Approach:**
- `FileNode` gains:
  - `gitStatus?: "M" | "U" | "A" | "D"` — undefined for unchanged or untracked-outside-a-repo
  - `isRepoRoot?: boolean` — true for sub-repo root directories
- New `FileContentResponse` type matches U2's response shape.
- `api.listFiles(sessionId)` returns `Promise<FileNode[]>`.
- `api.getFileContent(sessionId, path)` returns `Promise<FileContentResponse>`. URL-encode the path query param.

**Patterns to follow:**
- Other wrappers in `src/lib/api.ts` use the `request<T>` helper. Mirror their shape.

**Test scenarios:**
- Test expectation: none — type definitions and thin API wrappers, no behavior to test beyond `tsc --noEmit` passing.

**Verification:**
- `npx tsc --noEmit` passes.
- `npm run lint` passes.

---

### U4. Store lazy-load + WebSocket refresh

**Goal:** Wire file fetching into the existing per-session lazy-load flow and refresh on activity events.

**Requirements:** R1, R6, R8

**Dependencies:** U1, U3

**Files:**
- Modify: `src/stores/session-store.ts` (lazy-load in `setActiveSession`; add a `refreshFiles(sessionId)` action; optionally a `fileContents: Record<string, FileContentResponse>` cache)
- Modify: `src/hooks/use-websocket.ts` (debounced refresh trigger in the `activity_update` handler)

**Approach:**
- In `setActiveSession`, after the existing messages/activities loads, fire `api.listFiles(sessionId).then(files => get().setFileTrees(sessionId, files))`. Gate on `session.workspaceStatus === "ready"` to avoid 409s on warming workspaces.
- Add a `refreshFiles(sessionId)` action that re-fetches and replaces the tree for that session.
- Add an optional `fileContents` slice keyed by `${sessionId}:${path}` for the content viewer to cache successful fetches. Invalidate the entry for a session on `refreshFiles`.
- In `use-websocket.ts`, when an `activity_update` arrives, schedule `refreshFiles(sessionId)` through a per-session debounce (500ms trailing). A simple `Map<sessionId, ReturnType<typeof setTimeout>>` in module scope is sufficient.
- Also call `refreshFiles` when the workspace transitions from `starting` → `ready` (existing `session_update` handler is the natural place — fire when the new status is `ready` and the old was anything else).
- Use the existing `addActivity` dedupe pattern as a model for not double-fetching in race scenarios — if a refresh is already in flight for a session, drop the duplicate.

**Patterns to follow:**
- The messages/activities lazy-load in `session-store.ts` for shape.
- The existing `session_update` → `api.listSessions()` refresh in `use-websocket.ts` for "WS event triggers REST refresh."
- The `addMessage` / `addActivity` dedupe-by-ID convention for handling overlapping responses.

**Test scenarios:** *(manual smoke)*
- Covers R1. Switch into a session with a ready workspace → files tab populates without manual action.
- Covers R6. With the Files tab open, induce an `activity_update` (have an agent post a message) → tree refreshes within ~1s, no flicker.
- Covers R6. Rapid sequence of three `activity_update` events within 500ms → only one refresh fires (debounce works).
- Covers R8. Open a session whose workspace is `starting` → no files fetch happens, no error in console. Transition to `ready` → fetch fires automatically.
- Edge case: switch sessions rapidly (A → B → A) → each session's tree is correct after switching, no cross-contamination.
- Edge case: backend returns 409 mid-session because workspace stopped → store catches the error, leaves prior tree in place, doesn't throw.

**Verification:**
- Devtools network panel shows exactly one `/files` request per session switch (and per debounced activity batch).
- Zustand state shows `fileTrees[sessionId]` populated after switch.

---

### U5. FilesView: status colors, badges, refresh, content fetch

**Goal:** Surface git status in the tree, add the sub-repo marker, fetch content on click, and add a refresh button.

**Requirements:** R2, R4, R5, R6, R7, R8

**Dependencies:** U2, U3, U4

**Files:**
- Modify: `src/components/files/FilesView.tsx`

**Approach:**
- Apply per-row color and badge based on `gitStatus`:
  - `M` → `text-warning`, badge `M`
  - `U` → `text-success`, badge `U`
  - `A` → `text-success-emphasis`, badge `A`
  - `D` → `text-danger`, badge `D` with `line-through` on the name
  - undefined → existing default styling
- Render the badge to the right of the filename in a fixed slot, before the existing `modifiedBy` dot. Use a small uppercase mono character with adequate spacing so M/U/A/D are scannable.
- Sub-repo marker: when `node.isRepoRoot`, render a small `[repo]` chip next to the folder name (use `text-foreground-subtle` for low visual weight).
- Preserve the existing `modifiedBy` accent dot exactly as-is at lines 64-66. It will be absent in v1 since the producer is deferred, but the rendering path stays in place.
- Refresh button: add a small icon button (Lucide `RotateCw`) in a header row above the tree, calling the new `refreshFiles(activeSessionId)` store action. Disable while a refresh is in flight (track via local component state or a store flag).
- File content fetch: when a file is selected, call `api.getFileContent(activeSessionId, node.path)` and render the response. Handle:
  - `isBinary: true` → show "Binary file — preview unavailable"
  - `truncated: true` → show "File truncated to 1 MB (full size: X MB)" above the content
  - Network error → show "Failed to load file"
- Loading skeleton: show a small spinner inline above the tree during the first load if `fileTrees[sessionId]` is undefined and the workspace is `ready`.

**Patterns to follow:**
- Existing `FileTreeItem` recursion structure stays intact.
- Existing `cn()`-based conditional classNames pattern.
- shadcn/ui icon-button conventions used elsewhere in `src/components/ui/`.

**Test scenarios:** *(manual smoke)*
- Covers R2. Induce all four states in a workspace. Each file in the tree shows the correct color and badge.
- Covers R4. Workspace with a sub-repo: the sub-repo root folder shows a `[repo]` chip; files inside reflect the sub-repo's git status, not the parent's.
- Covers R5. Click a small text file → contents render in the right pane. Click a large file (>1 MB) → truncation banner appears. Click a binary file → "preview unavailable" message.
- Covers R6. Click the refresh button → tree re-fetches, button shows a brief disabled state, no double-fires.
- Covers R7. Manually set `modifiedBy` in the store dev-tools on a node → dot renders. Combined with `gitStatus: "M"` → both render side by side without visual collision.
- Covers R8. Switch to a session with `workspaceStatus: "starting"` → existing "Workspace warming up" placeholder shows, no tree request fires.
- Edge case: empty tree → tree pane shows nothing (no error). Content pane shows the existing "Select a file to view" prompt.
- Edge case: a `D`-status file is selected → content pane handles `FILE_NOT_FOUND` gracefully (the file is gone from disk).
- Integration: rapid clicks across files don't tangle responses (last clicked wins, no stale content displayed).

**Verification:**
- Visual: all four colors are distinguishable in the dark theme.
- The agent dot and git badge can coexist without overlap.
- Refresh button visibly re-fetches.
- `tsc --noEmit` and `npm run lint` pass.

---

## System-Wide Impact

- **Interaction graph:** Adds two GET endpoints under `/api/sessions/{sessionID}`. Frontend touches the existing Zustand store, the WebSocket hook, and the FilesView component. No DB schema changes; no sqlc work needed.
- **Error propagation:** Backend returns the standard `{ error: { code, message } }` envelope for all failure modes. Frontend lazy-loads are fire-and-forget — failures are logged but don't tear down the session. Content-fetch failures surface in the right-pane viewer only.
- **State lifecycle risks:** A refresh and a WS-triggered refresh can race. Mitigated by per-session debounce + dedupe-in-flight in the store. Switching sessions during an in-flight fetch could land stale data — mitigated by the response handler checking `activeSessionId` (or stamping requests with the session ID and comparing on resolution).
- **API surface parity:** No other surface needs the same change.
- **Integration coverage:** The workspace lifecycle (`starting` → `ready` → `suspended`) interacts with the fetch gating. The `session_update` → refresh hook in U4 covers the warming-to-`ready` transition.
- **Unchanged invariants:** No DB schema changes. No changes to `messages.go`, `activities.go`, or the WS event vocabulary. The `modifiedBy` field stays on `FileNode` exactly as today (no producer, but no removal either).

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| **Non-docker DevPod provider** (Kubernetes, AWS, SSH) puts workspace content on a different host, so the local-FS approach fails. | Document the assumption explicitly. Resolver returns a clear `WORKSPACE_NOT_FOUND` if the path doesn't exist locally. Pluggable resolver is deferred to follow-up (YAGNI for current setup where `DEVPOD_PROVIDER=docker` is pinned in env and devcontainer config). |
| **DevPod content-path layout changes** between DevPod versions. | Make the base path configurable via `DEVPOD_AGENT_CONTENT_DIR` env. Default to the observed v0.6.15 layout. |
| **Walk performance on very large workspaces** (millions of files). | Hardcoded prune list cuts the common heavy directories. 10s request timeout caps the worst case. Folder-rollup status (would require a second walk pass) is deferred. |
| **`git status --porcelain` output ambiguity** with renames or unmerged states. | V1 maps renames as "new path = A, old path ignored." Unmerged states (`UU`, `AA`, etc.) map to `M` for simplicity. Document in code. Revisit if/when conflict-resolution UI is requested. |
| **Symlink cycles during walk.** | `filepath.WalkDir` does not follow symlinks by default; that's the right behavior here. |
| **`activity_update` event storm** overwhelms refreshes. | 500ms trailing debounce in the WS hook + in-flight dedupe in the store. |
| **Path traversal via content endpoint.** | Validation rejects absolute paths and `..` segments before `filepath.Join`; post-join, we re-check that the resolved absolute path still has the workspace root as a prefix. |
| **Content endpoint returning huge files.** | Hard 1 MB cap with `truncated: true` marker. |
| **Binary content corruption in JSON response.** | Null-byte sniff sets `isBinary: true` and returns empty `content`. Frontend never tries to render binary text. |

---

## Documentation / Operational Notes

- Update `CLAUDE.md` "Environment Variables" section with `DEVPOD_AGENT_CONTENT_DIR` (the base workspaces directory; implementation appends `/<workspaceID>/content`; default `$HOME/.devpod/agent/contexts/default/workspaces`) if added during implementation.
- No migration needed.
- No new dependencies — pure stdlib on the Go side, no new npm packages on the frontend side.
- Verification posture per repo convention: `npx tsc --noEmit` for types, `npm run lint`, manual smoke against a running workspace. The repo currently has no automated test runner.

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-05-12-files-tab-git-status-requirements.md](../brainstorms/2026-05-12-files-tab-git-status-requirements.md)
- Related code:
  - [src/components/files/FilesView.tsx](../../src/components/files/FilesView.tsx) — existing tree component to extend
  - [src/stores/session-store.ts](../../src/stores/session-store.ts) — Zustand store and lazy-load pattern
  - [src/hooks/use-websocket.ts](../../src/hooks/use-websocket.ts) — WS dispatch
  - [src/types/index.ts](../../src/types/index.ts) — `FileNode` definition
  - [src/lib/api.ts](../../src/lib/api.ts) — API client wrappers
  - [src/styles/globals.css](../../src/styles/globals.css) — Primer color tokens
  - [server/internal/handler/messages.go](../../server/internal/handler/messages.go) — handler pattern reference
  - [server/internal/handler/handler.go](../../server/internal/handler/handler.go) — `writeError` / `writeJSON` helpers
  - [server/internal/server/server.go](../../server/internal/server/server.go) — route registration site
  - [server/internal/workspace/manager.go](../../server/internal/workspace/manager.go) — workspace manager (unchanged but referenced for the workspace-id lookup pattern)
- Empirical inspection: DevPod workspace `kol-5324` confirmed bind-mount layout at `~/.devpod/agent/contexts/default/workspaces/<id>/content`.
