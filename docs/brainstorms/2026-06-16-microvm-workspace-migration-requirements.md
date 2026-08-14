---
date: 2026-06-16
topic: microvm-workspace-migration
---

# microVM Workspace Migration — Requirements

## Summary

Replace Deuce's DevPod/devcontainer workspace runtime with Kata-based microVMs, giving each session a real kernel-isolation boundary and a fast warm-templated start, while keeping the existing OCI/devcontainer image pipeline. Each repo gets an explicit, approved developer-environment microVM template that sessions fork from. Bake a lightweight, software-rendered desktop with a browser into the workspace so humans and agents can see UI changes live.

## Problem Frame

Today every session runs in a DevPod-managed devcontainer (Docker), provisioned by the ~613-line `server/internal/workspace/manager.go` shelling out to the `devpod` CLI. Two pressures motivate moving off that model:

- **Isolation.** Sessions run agent-driven and increasingly agent-generated code. A shared-kernel container boundary is weaker than wanted; the goal is a per-session kernel boundary so one session's workload can't reach the host or another session through a container escape.
- **Cold-start latency.** Building a devcontainer from scratch is slow, and that delay is paid on the path users feel — opening a session. The desire is to prepare an environment once and start subsequent sessions from that prepared state in milliseconds rather than rebuilding.

Separately, STRATEGY.md's "Coding & Preview" track calls for live UI previews as a first-class surface, and makes agent-native parity a hard constraint: every collaborative surface must be agent-callable, because agents do most of the build and design work. A "see your changes in a browser" desktop is squarely on that track — but only if the agent can drive it too, not just a human.

## Key Decisions

- **Kata Containers + warm-VM templating, not raw Firecracker snapshots.** Run each session's existing OCI/devcontainer image inside a Kata microVM (Firecracker or Cloud Hypervisor backend) and fork sessions from a warm template. This preserves the whole devcontainer image pipeline and gives the kernel boundary, while getting most of the boot-speed win from templating. Raw-Firecracker snapshot/restore (own rootfs + kernel, resume a fully-prepared snapshot per session) remains a measured fallback if templating proves not fast enough — not the starting point.

- **Self-hostable over managed.** Managed Firecracker (e.g. Fly.io Machines) would buy the hard parts but couples sessions to a cloud platform and forks Deuce's open-source, self-hostable deployment story. The runtime must be self-hostable; managed platforms are at most an optional provider later.

- **Software-rendered desktop, no GPU.** The preview need is "see UI changes in a browser," which a virtual framebuffer streamed to the browser satisfies without acceleration. This keeps the desktop a small addition to the workspace image and keeps Firecracker-class microVMs viable (GPU/virtio-gpu would not be).

- **Per-repo environment template with an explicit approval gate.** Templates are not an invisible optimization — building one is an explicit step in repo setup, the built template is approved before it becomes the fast-path fork source, and it is reused until it needs rebuilding. This makes the golden image an intentional, reviewed artifact rather than an implicit cache.

## Requirements

### Runtime and isolation

- R1. Each session runs in a microVM with its own kernel, replacing the per-session Docker container as the isolation boundary.
- R2. The existing OCI/devcontainer image pipeline is preserved — repo environments are still defined and built as container images, not hand-rolled VM rootfs/kernels.
- R3. The workspace runtime is self-hostable with no required dependency on a managed cloud VM platform.
- R4. Sessions start from a prepared template fast enough to remove the from-scratch devcontainer build from the session-open path, once a template exists and is approved.

### Per-repo environment template lifecycle

- R5. Repo setup includes an explicit step that builds a developer-environment microVM template for that repo.
- R6. A built template must be approved before it becomes the fork source for that repo's sessions.
- R7. Once approved, every session for the repo forks from the approved template (the fast path).
- R8. A template is reused until it needs rebuilding; a rebuild routes back through the build-and-approve step (R5–R6).
- R9. A template is flagged stale and prompted for rebuild when the repo's environment definition (its devcontainer/Dockerfile/setup configuration) changes; a manual "rebuild template" action is also available. Rebuilds do not fire automatically on every push.
- R10. Before a repo has an approved template, sessions are not blocked — the first session falls back to a plain cold boot. Approval gates the *fast* path, not repo usability.
- R11. On fork from the template, each session regenerates its own entropy and per-session secrets (re-seed RNG, regenerate any host/SSH keys baked into the template) so cloned sessions do not share cryptographic state.

### Desktop preview

- R12. The workspace provides a lightweight, software-rendered desktop with a browser, reachable from within Deuce as a session surface (alongside the existing terminal and Open-in-VS-Code surfaces).
- R13. The desktop is baked into the repo's environment template so it is available immediately on a forked session, with no per-session desktop setup cost.
- R14. The desktop requires no GPU and no GPU/virtio-gpu passthrough.
- R15. The preview surface is agent-callable: an agent can observe and interact with the same desktop (e.g. screenshot and drive it), not only a human in a browser. This satisfies the agent-native-parity constraint for the Coding & Preview track.

### Access paths

- R16. The "Open in VS Code" path is reworked for the VM model. There is no `docker exec` into a microVM, so the SSH proxy's channel-open mechanism (currently `docker exec` in `server/internal/workspace/`-adjacent SSH handling) is rebuilt to exec into the VM (via the runtime's exec primitive or an in-guest sshd).
- R17. The per-session Pi agent runtime continues to run inside the workspace; its JSONL channel rides whatever new exec/transport replaces the container exec path.

## Acceptance Examples

- AE1. **Covers R5, R6, R7, R10.** A repo is connected to Deuce for the first time. **Given** no template exists yet, **when** a user opens the first session, **then** it cold-boots (slow) and is fully usable; meanwhile the repo-setup template build is available to run. **When** the built template is approved, **then** subsequent sessions for that repo fork from it and open fast.

- AE2. **Covers R8, R9.** A repo has an approved template. **When** the repo's environment definition changes, **then** the template is flagged stale and a rebuild is prompted; existing approved template keeps serving sessions until the rebuilt template is approved. **When** nothing about the environment definition changes, **then** ordinary code pushes do not trigger a rebuild.

- AE3. **Covers R11.** Two sessions for the same repo fork from the same approved template. **Then** they do not share RNG state or per-session secrets — each has freshly seeded entropy and regenerated keys.

- AE4. **Covers R12, R15.** A session is running with the desktop surface. **When** a human opens the desktop in the browser, **then** they see the live UI of the work in progress. **When** the agent needs to see the same UI, **then** it can screenshot and interact with that desktop through agent-callable handles.

## Scope Boundaries

### Deferred for later

- Raw-Firecracker snapshot/restore as the primary fast-start mechanism — kept as a fallback to adopt only if warm-templating benchmarks fall short.
- A managed-VM provider (e.g. Fly.io Machines) as an optional deployment backend, layered on after the self-hostable runtime exists.
- Per-user named-volume caching of `~/.vscode-server` and similar boot-time download optimizations (pre-existing v2 follow-ups, not part of this migration's core).

### Outside this product's identity

- GPU-accelerated desktops, virtio-gpu, or GPU passthrough — the preview is software-rendered by design; acceleration is not a goal of this work.
- Managed cloud VMs as a *required* runtime — would undercut Deuce's self-hostable, open-source positioning.

## Dependencies / Assumptions

- Assumes the host can run a microVM stack (KVM available, Kata + a Firecracker/Cloud Hypervisor backend installable) in target self-hosted and hosted deployments.
- Assumes "real isolation is needed" reflects a genuine threat model (untrusted/agent-generated code per session) rather than anticipatory hardening; the depth of the isolation work should track that threat model. Recorded as an explicit assumption because the brainstorm did not pin a specific incident or compliance trigger.
- Assumes warm-templating start time will be acceptable; this is unverified and should be benchmarked against the raw-Firecracker-snapshot fallback before the fallback is ruled fully out (see Outstanding Questions).
- The desktop is delivered in-browser from inside the VM; the exact streaming mechanism (e.g. a VNC/WebRTC stack) is an implementation choice for planning, constrained only by R14 (no GPU) and R15 (agent-callable).

## Outstanding Questions

### Resolve before planning

- None blocking — the direction and lifecycle are pinned.

### Deferred to planning

- Which Kata backend (Firecracker vs Cloud Hypervisor) and how templating/forking is configured.
- The concrete approval-gate surface: who approves a template, where that lives in repo setup, and how approval state is stored.
- The desktop streaming stack and how the agent's screenshot/interact handles are exposed (tie-in to the agent-native tool surface).
- The replacement exec/transport for the SSH proxy and Pi channel, and how the terminal-vs-VS-Code environment divergence documented in CLAUDE.md changes under the VM model.
- Benchmark plan: warm-templated fork time vs raw-Firecracker snapshot resume, to confirm the fallback stays a fallback.

## Sources / Research

- `server/internal/workspace/manager.go` — current DevPod/`devpod`-CLI provisioning (~613 lines); the provisioning layer this migration replaces.
- `CLAUDE.md` — SSH proxy via `docker exec`, the terminal-vs-Open-in-VS-Code divergence, and devcontainer compatibility requirements that the VM model must account for.
- `STRATEGY.md` — "Coding & Preview" track (live UI previews) and the agent-native-parity hard constraint that drives R15.
- `docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md` — existing devcontainer/DevPod workspace patterns.
