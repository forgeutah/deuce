---
title: Deploy Deuce as a container sharing the host Docker daemon, with path parity
date: 2026-07-31
category: architecture-patterns
module: server
problem_type: architecture_pattern
component: deployment
applies_when:
  - "Deploying Deuce to a VM or any host outside a developer laptop"
  - "Changing how the runtime image is built or which user the server process runs as"
  - "The files tab renders a tree but every file is missing its git status"
  - "Sessions report `missing` after a restart when their workspaces should have survived"
related_components:
  - workspace
  - sshproxy
  - development_workflow
tags:
  - deployment
  - devpod
  - docker-provider
  - path-parity
  - upgrade
  - bind-mount
---

# Deploy Deuce as a container sharing the host Docker daemon, with path parity

## Context

Deuce orchestrates containers, which makes "where does Deuce itself run" a real architectural question rather than a packaging detail. Three shapes were considered: run it directly on the host under systemd; run it in a privileged container with its own nested Docker daemon (what the devcontainer does); or run it in an ordinary container that drives the *host's* daemon through a mounted socket.

The socket-mounted shape looks obviously best on paper — no privileged container, no nested storage, and workspace containers are siblings of Deuce rather than children, so replacing Deuce doesn't disturb them. The reason it needed validating first is that it has a failure mode which produces no error at all.

Deuce does not read workspace files through DevPod. It reads them off its own filesystem and runs `git` against them (`server/internal/handler/files.go`), and it finds workspace containers via a label it reads from DevPod's own on-disk records (`server/internal/workspace/manager.go`). Both resolve through `os.UserHomeDir()`. So Deuce and the daemon it drives must agree on what a path *string* means. If they disagree, DevPod still succeeds, the container still starts, and the bind mount silently resolves to an empty host directory — the team sees an empty file tree while the agent works normally somewhere nobody is looking.

## Guidance

Run Deuce in an unprivileged container with the host Docker socket mounted, and bind the state directory **at the same absolute path inside and outside**, with `HOME` pointing at it:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
  - ${DEUCE_STATE_DIR}:${DEUCE_STATE_DIR}   # same string on purpose
environment:
  HOME: ${DEUCE_STATE_DIR}
group_add:
  - "${DOCKER_GID}"                         # host-specific; getent group docker
```

One mount covers everything that must survive a container replacement, because all of it hangs off `HOME`: DevPod's CLI-side workspace records, DevPod's agent-side cloned content, and the SSH host key. Do not mount these individually — the single-`HOME` mount is what makes parity checkable by inspection instead of by audit.

Three non-negotiables:

1. **The runtime image must carry `devpod`, the Docker CLI, and `git`.** Deuce is not self-contained; it shells out to all three. A distroless image boots, migrates, serves the SPA, and then fails on the first session create.

2. **Declare workspace trees safe for git — and compensate for what that turns off.** DevPod clones as the devcontainer's `remoteUser` (uid 1000 on most images) while the server runs as its own uid. Git refuses across that mismatch, and it fails *quietly* — the tree still lists, because walking a directory needs no ownership match, so only the per-file status vanishes. Matching uids is not available as a fix: `remoteUser` varies per devcontainer image, one deployment serves many repos, and the value isn't known until the workspace is built. So the image sets `git config --system --add safe.directory '*'`.

   That check was not merely pedantic, and turning it off has a cost that must be paid back. Git executes `core.fsmonitor` as a command and honours it from a repository's *own* `.git/config` — and workspace repositories are not trusted, since anyone with terminal access to a session, or the agent itself, can write there. With the ownership check suppressed, a planted value would run in the server process, which on this topology can reach the host Docker daemon: a path from workspace container to host root. Every git invocation against workspace content therefore pins the setting off on the command line, where it takes precedence over repository config (`server/internal/handler/files.go`), covered by a regression test that plants the config and asserts it never runs.

   The general rule when adding git invocations here: the command line is the only barrier between untrusted repository config and the server process. Anything git will execute from config must be pinned there.

3. **Pin an explicit image tag, never a floating one.** Upgrade becomes a deliberate edit and rollback is the reverse edit.

### The symptom to recognize later

This is the knowledge most likely to be needed and least likely to be re-derived. If someone changes the mount, the state directory, or the user the process runs as, the deployment does not break loudly — it degrades in one of two specific ways:

- **Empty or partial file tree, sessions reporting `missing` after a restart** → path parity is broken. Deuce and the daemon are resolving the same string to different directories. Check that the bind mount's source and destination are identical and that `HOME` matches.
- **Full file tree, but no file has a git status** → the ownership declaration is missing. Run `git status` inside the container against a workspace content path; "detected dubious ownership" confirms it.

Neither logs an error. Both look like "the files tab is a bit broken."

## Why This Matters

The alternative shapes each cost something concrete.

A nested-daemon container works — it's what the devcontainer does, and it sidesteps parity entirely because there is only one filesystem. But restarting it kills the nested daemon, so **every upgrade stops every workspace**, and it needs `privileged: true` plus overlayfs-on-overlayfs storage.

Host-native under systemd also works and has no parity risk, but it gives up containerized packaging and needs a raw binary artifact the release pipeline doesn't publish.

The socket-mounted shape keeps compose-simple packaging *and* leaves workspaces running across upgrades, which is the property that matters most in practice: upgrading Deuce should not disturb work in progress.

## Verification

Confirmed on an Ubuntu 24.04 VM (Docker 29.1.3) against a live deployment:

- The workspace container's bind mount source was the exact host path Deuce reads from.
- Files written *inside* the workspace container appeared in Deuce's file listing immediately, and modifications reported the correct git status.
- Rolling the Deuce image to a new tag left the workspace container running untouched — `Up 13 minutes` before and after — across three separate restarts.
- Stopping a workspace container produced `stopped`, not `missing`; restarting returned `ready` with content preserved and no re-clone.
- The SSH proxy authenticated a registered key and landed in the container as `vscode` (uid 1000), not root, with git working.

## Related

- [devpod-docker-workspace-bind-mount-2026-05-13.md](devpod-docker-workspace-bind-mount-2026-05-13.md) — establishes host-filesystem reads as the workspace data plane, which is what makes path parity load-bearing rather than incidental.
- [embedded-ssh-proxy-for-vscode-remote.md](embedded-ssh-proxy-for-vscode-remote.md) — the `--user` handling this topology depends on.
- [docs/brainstorms/2026-07-30-vm-deploy-and-upgrade-requirements.md](../../brainstorms/2026-07-30-vm-deploy-and-upgrade-requirements.md) — the deploy requirements this decision unblocks.
- [docs/plans/2026-07-31-001-feat-vm-deploy-topology-spike-plan.md](../../plans/2026-07-31-001-feat-vm-deploy-topology-spike-plan.md) — the spike that produced it.
