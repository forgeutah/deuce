---
title: Embed an SSH proxy in the Go server to route VS Code Remote-SSH into devcontainers via docker exec
date: 2026-05-28
category: architecture-patterns
module: sshproxy
problem_type: architecture_pattern
component: tooling
severity: medium
applies_when:
  - "You want a click-to-open VS Code (or Cursor / Zed / JetBrains Gateway) experience into ephemeral, user-controlled devcontainers with zero client-side configuration"
  - "Your auth model already lives in your own database (per-user SSH public keys, session membership) and you do not want a per-container authorized_keys file"
  - "Your backend runs on the same host as the Docker daemon and can shell out to docker exec"
  - "You are willing to operate a long-lived TCP listener on a port other than 22 (your VM admin sshd usually owns 22)"
related_components:
  - authentication
  - development_workflow
tags:
  - ssh-proxy
  - vscode-remote
  - docker-exec
  - devpod
  - devcontainer
  - golang-x-crypto-ssh
  - host-key
  - zero-config
---

# Embed an SSH proxy in the Go server to route VS Code Remote-SSH into devcontainers via docker exec

## Context

Deuce sessions run inside DevPod-managed devcontainers. The in-browser terminal panel is fine for shell work but does not deliver a real IDE — no LSP, no debugger, no rich editing. Users asked for "Open in VS Code" because their editing muscle is in VS Code Remote-SSH.

The constraints that make this non-trivial:

1. **Devcontainers are user-controlled.** They are configured by the repositories the user is working in. Installing `sshd` inside every devcontainer (the Coder / Gitpod shape) is off the table.
2. **No client-side setup.** No `~/.ssh/config` edits, no NSS modules, no host-level configuration. The flow must be: click button → VS Code opens → it just works.
3. **Auth reuses the existing user model.** Public keys live on the Deuce user. No per-container `authorized_keys` files.
4. **Port 22 is already taken.** The VM runs the admin sshd on 22 for ops access; the proxy cannot collide.
5. **Concurrent channels are expected.** VS Code Remote-SSH opens several SSH channels per connection (install probe, exec server, terminals, port forwards).

The pattern that satisfies all five is an **SSH server embedded as a goroutine inside the existing Go API process**, listening on a separate port, that authenticates against the same Postgres user model and routes each SSH channel into the target container via `docker exec`. The SSH wire protocol stays untouched on the VS Code side — Remote-SSH sees a vanilla OpenSSH-compatible endpoint.

Coder and Gitpod solve the same problem with a per-container sshd. Tailscale's `tailssh` and `gliderlabs/ssh` solve adjacent problems but lean on either a tailnet identity layer or a higher-level wrapper that hides the channel-dispatch details VS Code's probe sequence demands.

## Guidance

The skeleton has six load-bearing pieces. Each makes a small commitment that compounds.

### 1. Embed the SSH server in the API process, not a separate binary

```go
// server/main.go — the SSH listener is just another goroutine.
sshSrv, err := sshproxy.New(sshproxy.Config{
    Listen:      cfg.SSHListenAddr,         // ":2222" by default
    HostKeyPath: cfg.SSHHostKeyPath,         // "~/.deuce/ssh_host_ed25519_key"
    Queries:     queries,                    // same pgxpool-backed sqlc queries
    Workspace:   workspaceMgr,               // same workspace.Manager the HTTP side uses
    PublicHost:  cfg.PublicHostname,
})
if err != nil {
    slog.Error("ssh proxy disabled", "err", err)
    appState.SSHDisabled = true              // HTTP keeps serving; URI endpoint returns 503
} else {
    go sshSrv.ListenAndServe()
    defer sshSrv.Shutdown(shutdownCtx)
}
```

Two practical wins fall out of this:

- One process to deploy, observe, and SIGTERM. The HTTP and SSH listeners drain under the same 10s shutdown context.
- The SSH listener degrades to off if host-key load or `net.Listen` fails. HTTP keeps serving and the URI endpoint returns `503 SSH_UNAVAILABLE` instead of 502'ing the whole product. Bad host-key file ≠ outage.

Extraction to a standalone `cmd/deuce-sshd/main.go` is a file-move plus thin `main`, not an architectural commitment that has to be made now.

### 2. Use `golang.org/x/crypto/ssh` directly, not `gliderlabs/ssh`

VS Code Remote-SSH does specific things: it opens a session channel, runs an install-probe via `exec` (no PTY), sometimes opens a `shell` without a prior `pty-req`, opens a `direct-tcpip` channel back to a loopback port, and (optionally) requests the `sftp` subsystem. Each one needs a precise reply on the channel-request side.

`gliderlabs/ssh` is a great fit for SSH-as-a-shell — it hides exactly the channel-dispatch detail you need to control here. `x/crypto/ssh` gives you the raw `Channel.Accept`, `Reply(ok bool, payload []byte)`, and the freedom to allowlist channel types and request types yourself.

```go
const channelTypeSession = "session"
const channelTypeTCPIP   = "direct-tcpip"

for newCh := range chans {
    switch newCh.ChannelType() {
    case channelTypeSession:
        go handleSessionChannel(ctx, srvConn, newCh, perms)
    case channelTypeTCPIP:
        go handleDirectTCPIP(ctx, srvConn, newCh, perms)
    default:
        newCh.Reject(ssh.Prohibited, "channel type not allowed")
    }
}
```

Refuse-by-omission is the right posture for everything that isn't on the allowlist — `x11`, `auth-agent@openssh.com`, `direct-streamlocal@openssh.com`, etc.

### 3. Encode the session identity in the SSH username, validate before any DB load

`dc-<canonical-uuid>` as the username. A strict regex matches *before* any Postgres query fires:

```go
var usernameRegex = regexp.MustCompile(
    `^dc-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func (s *Server) publicKeyCallback(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
    user := meta.User()
    if !usernameRegex.MatchString(user) {
        return nil, errors.New("invalid username")     // no DB load
    }
    sid, _ := uuid.Parse(user[len("dc-"):])
    fp := ssh.FingerprintSHA256(key)

    matched, err := s.queries.LookupSessionMemberKeyByFingerprint(ctx,
        db.LookupSessionMemberKeyByFingerprintParams{SessionID: sid, Fingerprint: fp})
    // ... resolve container, build Permissions{Extensions: {session_id, user_id, key_id, fp}}
}
```

Three properties from this one decision:

- **No DB cost from garbage usernames.** Slow path is gated on a regex hit.
- **Username confusion timing attacks are defeated.** Reject paths return uniform errors; an attacker cannot distinguish "no such session" from "no key on file" by latency.
- **The username carries the *resource* (which session), not the user.** The auth callback resolves the owning user from the offered public key's fingerprint. URI history stays valid across container recreates because the session UUID is stable.

### 4. Per-user multi-key table with `(user_id, fingerprint)` uniqueness

```sql
CREATE TABLE user_ssh_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 255),
    public_key   TEXT NOT NULL CHECK (length(public_key) BETWEEN 1 AND 8192),
    fingerprint  TEXT NOT NULL CHECK (fingerprint ~ '^SHA256:[A-Za-z0-9+/]{43}=?$'),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_user_ssh_keys_user_fp
    ON user_ssh_keys(user_id, fingerprint);
```

**Per-user uniqueness, not global.** Corporate shared keys (yubikeys, team accounts) genuinely need to be registrable by two users without one of them getting a 409 that leaks the other's key existence. The composite index doubles as the O(1) auth lookup — no separate `user_id` index needed.

`label` and `last_used_at` exist for the settings UI; `fingerprint` is the only field the auth callback reads.

### 5. `docker exec` is the channel transport — `-it` only when the client asked for a PTY

The session channel handler maintains a tiny state machine: did the client send `pty-req` before `shell` or `exec`? If yes, allocate a PTY and run `docker exec -it`. If no, plain `docker exec -i` with three separate pipes.

```go
// Pseudocode — see server/internal/sshproxy/session.go for the full version.
if ptyRequested {
    ptmx, _ := pty.Start(exec.CommandContext(ctx, "docker", "exec", "-it", container, "bash", "-l"))
    go io.Copy(channel, ptmx)
    go io.Copy(ptmx, channel)
} else {
    cmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "bash", "-l")
    cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinPipe, stdoutPipe, stderrPipe
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    cmd.Start()
    // ... pump goroutines, cleanup, etc.
}
```

The `-it` vs `-i` distinction is the single most error-prone part of the build. VS Code's install probe (`ssh -T`) opens a `shell` channel *without* a prior `pty-req`. Forcing a synthetic PTY echoes the piped install script back interleaved with real stdout, and VS Code's parser bails with `BadLocalDownloadRequest`. Treat "no `pty-req` seen ⇒ no PTY allocated" as an invariant of the handler.

`Setpgid: true` plus an explicit `SIGTERM → 5s → SIGKILL` cleanup on channel close is how you avoid leaving orphan `docker exec` children when VS Code disconnects abruptly.

### 6. Resolve the container by reading DevPod's on-disk workspace metadata, not by a label DevPod doesn't set

The trap: DevPod's docker provider does **not** label its containers `devpod.workspace=<id>`. Containers are labeled by the embedded devcontainer CLI as `dev.containers.id=<context>-<truncated-id>-<hash>`. The mapping from your workspace ID to that label's `uid` lives on disk:

```bash
~/.devpod/contexts/<context>/workspaces/<workspace-id>/workspace.json
# { "id": "...", "uid": "<the hash you need>", ... }
```

Read that file, grab `uid`, then `docker ps --filter label=dev.containers.id=<uid>* --format {{.Names}}`. Validate the resulting name against `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$` (Docker's own naming rules) before passing it to `docker exec` — defense-in-depth against a future output-format change or a label-injection vector.

Pre-checking container reachability **inside the auth callback** (not later, when a channel opens) means a docker-down or container-not-running condition surfaces as a clean SSH auth rejection with a clear server-side log entry, not as a mysterious mid-channel failure VS Code can't recover from.

## Why This Matters

**SSH is the universal remote-IDE protocol.** VS Code, Cursor, Zed, JetBrains Gateway, neovim's distant.nvim — all of them speak Remote-SSH. Building one SSH-side surface gives you every editor for free. A custom protocol or a per-editor extension is years of maintenance you didn't sign up for.

**Devcontainers stay untouched.** Modifying user-controlled devcontainer images to install sshd, mount authorized_keys, or run a per-container agent is a non-starter for any product where the user owns the image. `docker exec` lets the host orchestrate without touching the container's filesystem.

**One auth model, one user table.** The HTTP API and the SSH proxy both resolve the same `users` row through the same `pgxpool.Pool`. There's no parallel identity system, no token-exchange dance, no second source of truth. Revoking a key is a SQL DELETE; revocation takes effect on the next auth attempt without any IPC between listeners.

**Failure modes are localized.** A panic inside a session goroutine cannot kill the HTTP listener. A bad host-key file disables SSH but leaves HTTP serving. A docker-daemon outage rejects new SSH connections cleanly at auth time instead of producing mid-channel errors VS Code can't parse. Each layer fails in a way the next layer up can describe.

**Modern crypto posture is cheap.** ed25519 host key, KEX limited to `curve25519-sha256`, ciphers `chacha20-poly1305@openssh.com` and `aes256-gcm@openssh.com`. Refuse password, keyboard-interactive, GSSAPI, agent forwarding, and `direct-streamlocal` channels. These are one-line server-config changes that eliminate entire downgrade and pivot classes.

The trade-off you accept: a new TCP listener on a non-22 port. If a load balancer or browser SSH-URI handler in your environment can't deal with port-in-URI, you either bind on a dedicated subdomain's port 22 or ship a "copy this `~/.ssh/config` snippet" fallback. In Deuce's case, modern VS Code Stable handles port-in-URI fine when paired with `?windowId=_blank` (so the editor spawns a new window instead of hijacking the user's focused workspace).

## When to Apply

- You want zero-config remote-IDE access into ephemeral containers from VS Code, Cursor, Zed, or JetBrains Gateway.
- Your auth model lives in your own database — per-user public keys, session/team membership — and you do not want to maintain a per-container `authorized_keys` file.
- Your backend can shell out to `docker exec` (the deuce process user is in the `docker` group, or has an equivalent privileged path to the Docker daemon).
- You can operate on a non-22 port without it confusing your users — typically true on a deployment where the VM admin sshd already owns 22.
- You're willing to document devcontainer requirements (`bash`, `tar`, `curl`/`wget`, `openssh-sftp-server`, glibc — VS Code Remote needs all four).

Do **not** apply when:

- The container host is *not* the same host as the backend (e.g., DevPod with the Kubernetes or AWS provider). `docker exec` doesn't reach across hosts; you'd need a per-host agent or a different transport entirely.
- You need a real audit trail of every command (`docker exec` is invisible to the in-container shell history; the SSH proxy's structured logs are your only record).
- Your tenants share Docker daemons across security boundaries. `docker exec` runs as the container's image-default UID — if that's root on a shared host, the SSH proxy is also a privilege-escalation path you have to harden against.

## Examples

### Username gate before any DB load (auth.go)

```go
// server/internal/sshproxy/auth.go
var usernameRegex = regexp.MustCompile(
    `^dc-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func (s *Server) publicKeyCallback(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
    if !usernameRegex.MatchString(meta.User()) {
        return nil, errors.New("invalid username")
    }
    sid, _ := uuid.Parse(meta.User()[len("dc-"):])
    fp := ssh.FingerprintSHA256(key)

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    matched, err := s.queries.LookupSessionMemberKeyByFingerprint(ctx, db.LookupSessionMemberKeyByFingerprintParams{
        SessionID:   sid,
        Fingerprint: fp,
    })
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, errors.New("no auth")
    }

    // Pre-check container reachability so docker-down surfaces here, not mid-channel.
    container, err := s.workspace.ContainerName(ctx, sid.String())
    if err != nil {
        return nil, errors.New("session unavailable")
    }

    return &ssh.Permissions{Extensions: map[string]string{
        extSessionID: sid.String(),
        extUserID:    matched.UserID.String(),
        extKeyID:     matched.KeyID.String(),
        extFP:        fp,
        "container":  container,
    }}, nil
}
```

### Channel handler — PTY only when the client asked

```go
// Track whether pty-req fired before shell/exec.
var ptyRequested bool

for req := range channelReqs {
    switch req.Type {
    case reqPTY:
        ptyRequested = true
        req.Reply(true, nil)
    case reqShell:
        cmd := exec.CommandContext(ctx, "docker", "exec",
            execMode(ptyRequested),                       // "-it" or "-i"
            container, "bash", "-l")
        runWithCleanup(ctx, cmd, channel, ptyRequested)
        req.Reply(true, nil)
    case reqExec:
        // Same shape, but `bash -c <command>` instead of `bash -l`.
    }
}

func execMode(pty bool) string {
    if pty {
        return "-it"
    }
    return "-i"
}
```

### Two bugs that bit during integration

These compound the pattern — the architecture is sound, but the specific failures below cost real wall-clock time, so they're worth burning into the doc:

1. **`exec.Cmd.Wait` closes pipes before pump goroutines finish reading.** The non-PTY path spawned `cmd.Wait` concurrently with the stdout/stderr pumps. For fast-exiting children (`echo`-class commands), `Wait` reaped the zombie and closed the read end of the pipe before the pump ever read a byte. The pump then opened on a closed fd and forwarded zero bytes — VS Code saw an empty install-probe response. Use `cmd.Process.Wait()` directly to reap the zombie without closing the pipes, set `cmd.ProcessState` manually so exit-status reads still work, then let the pumps EOF naturally, then close stdin. (Commit `0650ab6`.)

2. **DevPod's docker provider doesn't set a `devpod.workspace` label.** The label is `dev.containers.id=<uid>` and the `uid` lives in `~/.devpod/contexts/<context>/workspaces/<id>/workspace.json`. Filtering by the wrong label silently returned no container for every session, surfacing as auth-time `session unavailable` rejections with no obvious connection to the misconfigured filter. Always verify your container-discovery filter against a live `docker inspect` before shipping. (Commit `0af74dd`.)

### Working implementation in this repo

- [server/internal/sshproxy/](../../../server/internal/sshproxy/) — the embedded SSH server package.
- [server/internal/sshproxy/auth.go](../../../server/internal/sshproxy/auth.go) — username gate, fingerprint-based public-key auth, container pre-check.
- [server/internal/sshproxy/session.go](../../../server/internal/sshproxy/session.go) — channel and request dispatch, PTY-or-not branching, cleanup.
- [server/internal/sshproxy/tcpip.go](../../../server/internal/sshproxy/tcpip.go) — `direct-tcpip` bridged to in-container loopback via `bash`'s `/dev/tcp` builtin.
- [server/internal/sshproxy/hostkey.go](../../../server/internal/sshproxy/hostkey.go) — first-boot ed25519 host-key generation, persisted at `~/.deuce/ssh_host_ed25519_key`.
- [server/internal/workspace/manager.go](../../../server/internal/workspace/manager.go) — `ContainerName(ctx, workspaceID)` reading DevPod's `workspace.json` and validating the resulting container name.
- [server/main.go](../../../server/main.go) — the SSH listener wired in alongside the HTTP server, with degraded-mode fallback.

## Related

- [docs/plans/2026-05-26-003-feat-vscode-ssh-proxy-plan.md](../../plans/2026-05-26-003-feat-vscode-ssh-proxy-plan.md) — the implementation plan, including the U1–U16 unit breakdown, the deferred follow-ups, and the verify-early items that shaped this design.
- [docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md](./devpod-docker-workspace-bind-mount-2026-05-13.md) — the precedent for reading DevPod state directly off the host filesystem, which informed the workspace.json lookup in section 6 above.
- [CLAUDE.md](../../../CLAUDE.md) — the env-var reference for `DEUCE_SSH_*` and `DEUCE_PUBLIC_HOSTNAME`, the hosted-deployment checklist for the SSH proxy, the terminal-vs-VS-Code divergence note, and the devcontainer compatibility requirements.
- Mozilla OpenSSH hardening guidelines — <https://infosec.mozilla.org/guidelines/openssh> — source for the cipher/KEX posture.
- VS Code Remote-SSH port-in-URI history — <https://github.com/microsoft/vscode-remote-release/issues/8764> — verify-early item that shaped the URI fallback plan.
