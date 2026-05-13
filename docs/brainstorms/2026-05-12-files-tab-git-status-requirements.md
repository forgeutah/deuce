# Brainstorm: Files Tab with Git Status

**Date:** 2026-05-12
**Status:** Draft
**Participants:** Clint Berry, Claude
**Related:** [Deuce v0 UX](2026-05-08-deuce-v0-ux-brainstorm.md), [Session Creation via DevPod](2026-05-08-session-creation-devpod-brainstorm.md)

---

## What We're Building

Make the Files tab in a session actually work. Today, [FilesView.tsx](../../src/components/files/FilesView.tsx) renders a tree from the Zustand store's `fileTrees`, but no backend endpoint populates it — so the tab is wired but always empty.

v1 turns the Files tab into a workspace file explorer with git-status-aware coloring, so participants can see at a glance what the agent (or anyone) has changed without dropping to a terminal. It also needs to handle workspaces that contain multiple git repos cloned into them, since that's the real shape of Clint's setup.

---

## Goals

- A participant can open the Files tab on a session and see the workspace tree without doing anything else.
- File entries are visually distinct based on git state (modified / untracked / staged / deleted), VS Code Explorer style.
- Nested git repos inside the workspace are first-class — each has its own git status scope, and sub-repo roots are visible even when the parent repo `.gitignore`s them.
- Clicking a file shows its content in the existing right-hand viewer pane.
- The existing "modified by agent" dot survives alongside the new git colors as a second, independent signal.

## Non-Goals (v1)

- Inline diff view of changes (status colors only, no diff body)
- File search / fuzzy filter inside the tree
- In-browser editing
- Showing `.gitignore`'d files (other than sub-repo roots, which we always show)
- A filesystem watcher / WebSocket push for file-change events
- Multi-workspace anything — we're inside a single DevPod workspace per session

---

## Users and Use Case

**Who:** Anyone viewing a Deuce session — engineers checking what an agent built, reviewers giving feedback, the user themselves spot-checking work.

**What changes for them:** Today they alt-tab to a terminal and run `git status` or `ls` to see what's happening on disk. After v1, that's visible inline in the session UI with no context switch.

**Counterfactual:** Without this, the Files tab stays an empty placeholder, and the workspace is only legible via the Terminal tab.

---

## Key Decisions

### 1. Surface and shape

- **Where:** Files tab in `CenterPanel` — already present, no new surface.
- **Shape:** Full workspace tree on the left, content viewer on the right. Same as the existing component skeleton; we are filling in real data, not redesigning.
- **VS Code Explorer model:** standard tree, status colors and single-letter badges on files, neutral folders.

### 2. Git status set (four states)

| Code | State | Color (TBD by ce-plan, GitHub Primer tokens) |
|------|-------|----------------------------------------------|
| `M` | Modified (tracked, dirty) | yellow-ish |
| `U` | Untracked (new) | green-ish |
| `A` | Staged (added) | green, emphasized |
| `D` | Deleted | red, struck-through |

All four come from a single `git status --porcelain=v1` parse per repo. No need to differentiate index vs worktree at the UI level for v1 — `A` represents "staged-and-clean-or-modified," `M` represents "any non-staged dirty state."

### 3. Refresh model

- Fetch on **tab open**
- Re-fetch on **session switch**
- Re-fetch on `activity_update` WS events (piggyback on the existing hub)
- Manual **refresh button** as a fallback
- No polling, no timers, no filesystem watcher

Rationale: every refresh is a `devpod ssh --command` round-trip into the workspace, which is non-trivial. Activity-triggered refresh covers the realistic "an agent did something" case without burning SSH calls when nothing is happening.

### 4. Sub-repo support

The workspace is a single DevPod-mounted directory that contains a top-level repo *and* additional repos cloned into it by setup scripts. The nested repos are `.gitignore`'d by the parent.

Rules:
- **Discovery**: walk the workspace for directories containing a `.git/` entry. Each such directory is a repo root.
- **Visibility override**: a sub-repo root is always shown in the tree even if a parent's `.gitignore` would exclude it. (Files *inside* the sub-repo are subject to the sub-repo's own `.gitignore`.)
- **Status scoping**: run `git status --porcelain` from inside each repo root. A file's status is computed by its nearest enclosing repo, never by an ancestor.
- **UI marker**: sub-repo root directories get a small `[repo]` badge or equivalent so the boundary between repos is visible.
- **Performance bound**: when descending into a directory, stop walking past common heavy dirs (`node_modules`, `vendor` *only when not a repo root itself*, `.git`, build dirs). ce-plan to pick the exact ignore list.

### 5. Agent attribution dot

Keep the existing `modifiedBy` accent dot at [FilesView.tsx:64-66](../../src/components/files/FilesView.tsx#L64-L66). It conveys a different signal than git status:

- **Git color** = current on-disk state vs HEAD
- **`modifiedBy` dot** = an agent edited this file during the session (even if it was later reverted)

These can disagree (agent edited then reverted → dot yes, color no; user edited via terminal → color yes, dot no), and both are useful context. Backend must attribute file edits to a participant per session for this to actually populate — today the field exists in `FileNode` but no producer fills it.

### 6. File content view

The existing viewer reads `selectedFile.content` inline. That doesn't scale and isn't populated. v1 fetches file content on demand when a file is clicked, via a separate endpoint. Text files render in the existing `<pre>` block; binary files show a placeholder.

---

## Scope Boundaries

### Deferred for later (not v1, but plausible v2)
- Folder-rollup status badges (folders containing dirty files get a hint)
- Inline diff view of a changed file
- File search / fuzzy finder
- Showing ignored files dimmed (VS Code parity)
- Live filesystem watcher → WS push for sub-second freshness
- In-browser editing

### Outside this feature's identity (probably never here)
- Branch switching, commit, push, merge — git *actions* live elsewhere (terminal, separate UI)
- Multi-session file aggregation
- Conflict resolution UI

---

## Dependencies and Assumptions

- **DevPod workspace is running.** The existing `workspaceStatus` gates in `FilesView` (`starting` / `failed` states) are preserved; the tree only loads when the workspace is `running`.
- **`devpod ssh --command` is the data plane.** Backend shells into the workspace to list files and run git. No agent or daemon installed inside the workspace for v1.
- **One workspace per session.** Already the model in [server/internal/workspace/manager.go](../../server/internal/workspace/manager.go).
- **Sub-repos are plain clones, not submodules.** Discovery walks for `.git/` rather than parsing `.gitmodules`. If submodules appear later they'd happen to be discovered the same way, but the design isn't optimized for them.
- **Single repo per workspace was previously assumed in other docs.** This brainstorm explicitly contradicts that and is the source of truth going forward.

---

## Open Questions for ce-plan

These are *implementation* choices ce-plan resolves; they don't need product decisions:

- Exact backend endpoint shape: one combined `/api/sessions/:id/files` returning the unified tree with `gitStatus` per node, vs. separate `/files` (structure) and `/git-status` (overlay) endpoints.
- Whether to compute the tree server-side as a single recursive scan or stream it.
- How to bound walk cost on large workspaces (depth limit? `find` with prune list? respect `.gitignore` via `git ls-files` for the top repo and fallback for sub-repos?).
- File content endpoint: full-file fetch vs. streamed range; binary detection rule.
- Color tokens to assign to each of M / U / A / D (must come from the existing Primer palette in `src/styles/globals.css`).

---

## Success Criteria

- Opening a session's Files tab populates a tree within a couple of seconds for a normal-sized workspace.
- Files that `git status` reports as M, U, A, or D appear in their respective colors with matching badges.
- A workspace containing a top-level repo and at least one cloned sub-repo shows both, with the sub-repo's files getting *its* git status, not the parent's.
- Clicking a file shows its current contents.
- Switching sessions or receiving an `activity_update` triggers a refresh without the user clicking anything.
- The existing `modifiedBy` dot still renders for any file flagged as agent-touched.
