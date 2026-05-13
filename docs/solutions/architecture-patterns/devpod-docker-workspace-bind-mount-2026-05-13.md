---
title: Read DevPod docker workspaces from the host filesystem, not via SSH
date: 2026-05-13
category: architecture-patterns
module: server
problem_type: architecture_pattern
component: tooling
severity: medium
applies_when:
  - "Backend Go handlers need to read, list, walk, or run git against DevPod workspace files"
  - "DevPod is configured with the docker provider (the common local dev case)"
  - "The backend process runs on the same host as the DevPod agent"
related_components:
  - development_workflow
tags:
  - devpod
  - filesystem
  - docker-provider
  - bind-mount
  - workspace
---

# Read DevPod docker workspaces from the host filesystem, not via SSH

## Context

The default mental model for interacting with a DevPod workspace is `devpod ssh --command "..."` — shell in, run something, capture stdout. For anything beyond a single read (listing a tree, running `git status` across multiple subdirectories, fetching file contents), this gets brittle: shell escaping, JSON-in-shell marshaling, SSH connection overhead per call, opaque error modes that conflate "SSH failed," "shell failed," and "your command failed."

The gap: when DevPod uses the **docker provider**, the workspace container is just a Docker container with a bind mount from the host. The host already has direct filesystem access to every file the container sees under `/workspaces/<id>`. SSH is unnecessary indirection.

This was discovered while building the Files tab — the brainstorm doc originally specified `devpod ssh --command` as "the data plane," and the user pushed back ("the filesystem should be local and mounted inside the devcontainer"). `docker inspect` confirmed the bind mount, and switching to direct FS access dramatically simplified the implementation.

## Guidance

For the **docker provider**, workspace content lives on the host at:

```
${HOME}/.devpod/agent/contexts/<context>/workspaces/<workspace-id>/content/
```

Defaults: context is `default`; workspace-id is whatever was passed to `devpod up --id <id>`. To verify on a given system, find the container and inspect its mounts:

```bash
docker inspect <workspace-container-name> --format \
  '{{range .Mounts}}{{.Type}} {{.Source}} -> {{.Destination}}{{println}}{{end}}'
```

Look for the `bind` whose Destination is `/workspaces/<id>` — its Source is the host path you can read directly.

In a Go handler, resolve the path once and use stdlib:

```go
base := os.Getenv("DEVPOD_AGENT_CONTENT_DIR")
if base == "" {
    home, _ := os.UserHomeDir()
    base = filepath.Join(home, ".devpod", "agent", "contexts", "default", "workspaces")
}
root := filepath.Join(base, workspaceID, "content")

if info, err := os.Stat(root); err != nil || !info.IsDir() {
    writeError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace content not found on host")
    return
}

// Read files
data, err := os.ReadFile(filepath.Join(root, relPath))

// Run git with the workspace as cwd
ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
defer cancel()
cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
cmd.Dir = root
out, err := cmd.Output()
```

Two non-negotiables:

1. **Make the base path overridable** via env var (e.g., `DEVPOD_AGENT_CONTENT_DIR`). DevPod's internal layout could change between minor versions; an env override lets operators adapt without a code change. It also leaves an obvious knob for non-docker providers if they ever land.
2. **Stat-check the resolved path** and return a clear error (e.g., 404 `WORKSPACE_NOT_FOUND`) when the directory doesn't exist. Silent fallbacks here mask provider misconfiguration.

Also relevant when the endpoint is a content-fetch surface: lexical path validation isn't enough on its own — call `filepath.EvalSymlinks` and re-check the resolved path's prefix against the workspace root before reading, or a symlink planted inside the workspace can escape it.

## Why This Matters

Replacing SSH with direct FS access collapses an entire class of complexity. There's no shell pipeline to escape, no stdout marshaling, no SSH connection overhead, and `context.WithTimeout` actually works the way you'd expect (it cancels the syscall, not a remote process you can't reach). `filepath.WalkDir` becomes available for free, `git` invocations use `cmd.Dir` instead of `cd && git`, and tests can point at any local directory.

Performance improves materially (typical SSH cold-start is 100–500ms; a stat is microseconds), but the bigger win is **correctness**: every bug class tied to shell escaping, partial output, and SSH connection lifecycle simply disappears. The handler becomes ordinary Go I/O.

**Trade-off**: this only works for the docker provider with a local host. Remote DevPod providers (Kubernetes, AWS, SSH) put workspace content on a different host and will need either SSH or a pluggable resolver. The env-overridable base path leaves the door open.

## When to Apply

- Backend services need to read, list, walk, or run shell tools (`git`, `rg`, `find`) against DevPod workspace files.
- DevPod is configured with the docker provider — the common local-dev case, and what this repo's devcontainer pins via `DEVPOD_PROVIDER=docker`.
- The backend process runs on the same host as the DevPod agent (the devcontainer hosting Deuce, or the host machine in a non-devcontainer setup).
- The workspace-id is known to the backend (it was passed to `devpod up --id` and is stored on the session).
- Operations are read-heavy or involve subprocess invocation.

Do **not** apply when: the provider is Kubernetes / AWS / SSH-remote, the backend runs on a different machine than the agent, or workspace mutation needs to go through DevPod's own lifecycle (use the `devpod` CLI for that).

## Examples

**Before — SSH approach:**

```go
cmd := exec.CommandContext(ctx, "devpod", "ssh", workspaceID,
    "--command", fmt.Sprintf("cat %q", relPath)) // quoting bugs waiting to happen
out, err := cmd.Output()
if err != nil {
    // could be SSH failure, missing file, or shell error — opaque
}
return out, nil
```

**After — local FS approach:**

```go
root := filepath.Join(agentContentDir, workspaceID, "content")
if _, err := os.Stat(root); err != nil {
    return nil, ErrWorkspaceNotFound
}
return os.ReadFile(filepath.Join(root, relPath))
```

Same shape for `git`:

```go
// Before
exec.CommandContext(ctx, "devpod", "ssh", id,
    "--command", "cd /workspaces/x && git status --porcelain")

// After
cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
cmd.Dir = root
```

Working implementation in this repo: [server/internal/handler/files.go](../../../server/internal/handler/files.go) — see `workspaceContentPath` for the path resolver and `ListFiles` / `GetFileContent` for callers that exercise both `filepath.WalkDir` + per-repo `git status` and `os.ReadFile` with symlink-resolved path validation.

## Related

- [docs/brainstorms/2026-05-12-files-tab-git-status-requirements.md](../../brainstorms/2026-05-12-files-tab-git-status-requirements.md) — the brainstorm that originally specified SSH as the data plane, until this discovery contradicted it.
- [docs/plans/2026-05-12-003-feat-files-tab-git-status-plan.md](../../plans/2026-05-12-003-feat-files-tab-git-status-plan.md) — implementation plan that captures the local-FS decision in Key Technical Decisions.
- [server/internal/workspace/manager.go](../../../server/internal/workspace/manager.go) — the existing SSH-based exec helper, still appropriate for the interactive terminal (which genuinely needs a PTY) but not for file I/O.
