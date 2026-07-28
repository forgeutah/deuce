---
title: "DevPod private GitHub repo clone hangs then fails with exit status 128"
date: 2026-07-28
category: integration-issues
module: server/internal/workspace
problem_type: integration_issue
component: authentication
symptoms:
  - "clone repository: exit status 128 when creating a session for a private GitHub repo"
  - "session creation hangs ~60s before failing (git blocked on a credential prompt)"
  - "git ls-remote on the private HTTPS URL hangs or times out locally"
  - "public repos clone fine; only private repos fail"
root_cause: incomplete_setup
resolution_type: code_fix
severity: high
related_components:
  - server/internal/config
  - server/internal/server
  - devpod
tags:
  - devpod
  - git-credentials
  - github-token
  - private-repo
  - workspace-manager
  - https-auth
  - credential-helper
---

# DevPod private GitHub repo clone hangs then fails with exit status 128

## Problem

New-session creation failed whenever DevPod tried to clone a **private** GitHub repo: the workspace build hung for ~60 seconds and then died with `clone repository: exit status 128`. Deuce had a `GITHUB_TOKEN`, but it was never handed to git or DevPod — so private clones got no credentials, blocking users from starting sessions on any private repo (i.e. most real work).

## Symptoms

- Session/workspace creation stalls, then the streamed `devpod up` output ends with:
  ```
  info  URL: https://github.com/<org>/<private-repo>.git
  info  clone repository: exit status 128
  fatal run agent command: Process exited with status 1
  ```
- **The tell:** the failure is *not* instant. There's a ~60s hang before exit 128. An outright auth *rejection* would come back fast; the long pause is git blocking on a credential prompt / callback that never resolves. Treating exit 128 as "bad token / instant 401" sends you down the wrong path — the token was never *offered* at all.
- `git ls-remote` on the private HTTPS URL hangs or times out locally (reproduces the same credential stall outside DevPod).
- Public repos clone fine; only private ones fail — reinforcing that it's a credentials-plumbing gap, not a DevPod or network problem.
- The repo-picker UI works (org/repo listing populates), which misleadingly *looks* like GitHub auth is wired up end-to-end.

## What Didn't Work

- **Assuming exit 128 == instant auth rejection.** It's a ~60s *hang* first. The mental model "git returned 128, so the token is wrong/expired" is a trap — git never received a token; it blocked waiting for one. Chasing token scopes/expiry wastes time.
- **Assuming `GITHUB_TOKEN` already covered clones because it's set and the repo picker works.** The token was wired *only* into the go-github API client in `server/internal/handler/github.go` (`github.NewClient(nil).WithAuthToken(h.githubToken)` at lines ~65 and ~166, gated by `h.githubToken` at ~41). That path is the REST repo-lister — it has nothing to do with the git clone DevPod runs. Same env var, two totally separate consumers; one working told you nothing about the other.
- **Embedding the token in the clone URL** (`https://x-access-token:TOKEN@github.com/org/repo.git`). Deuce's `server/internal/handler/validation.go` explicitly *rejects* credentials-in-URL, so this can't be the fix — and it would leak the token into logs, `workspace.json`, and process args. Dead end by design.
- **Expecting DevPod to clone on the host** where the operator's `~/.gitconfig` would apply. DevPod clones **inside the container**; for credentials it calls back out to the host via `devpod agent git-credentials`, which runs `git credential fill` on the host as a child of the `devpod up` process. So the fix has to make *that host-side child process* resolve github.com — not the container, and not by mutating the URL.

## Solution

The gap: the token reached only the API client, never git/devpod.

`server/internal/handler/github.go` (the *only* pre-fix consumer):
```go
client := github.NewClient(nil).WithAuthToken(h.githubToken)
```

**Fix**, in `server/internal/workspace/manager.go`. Seed an isolated git credential store from the token and hand its env to the `devpod up` subprocess.

New `gitCredentialFiles` (writes `~/.deuce/gitconfig` + `~/.deuce/git-credentials`, both `0600`):
```go
func gitCredentialFiles(baseDir, token string) ([]string, error) {
	if token == "" {
		return nil, nil // empty token = no-op; public clones still work
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create git credential dir: %w", err)
	}
	credPath := filepath.Join(baseDir, "git-credentials")
	credLine := fmt.Sprintf("https://x-access-token:%s@github.com\n", token)
	if err := os.WriteFile(credPath, []byte(credLine), 0o600); err != nil {
		return nil, fmt.Errorf("write git credentials: %w", err)
	}
	cfgPath := filepath.Join(baseDir, "gitconfig")
	cfg := fmt.Sprintf("[credential]\n\thelper = store --file=%s\n", credPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return nil, fmt.Errorf("write gitconfig: %w", err)
	}
	return []string{
		"GIT_CONFIG_GLOBAL=" + cfgPath,
		"GIT_CONFIG_SYSTEM=/dev/null",
	}, nil
}
```

Provisioned once and cached on the `Manager` (token stored via `NewManager(bin, provider, githubToken)`):
```go
func (m *Manager) gitCredentialEnv() []string {
	m.gitOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Warn("git credential setup skipped: no home dir; private repo clones will fail", "error", err)
			return
		}
		env, err := gitCredentialFiles(filepath.Join(home, ".deuce"), m.githubToken)
		if err != nil {
			slog.Warn("git credential setup failed; private repo clones will fail", "error", err)
			return
		}
		m.gitEnv = env
	})
	return m.gitEnv
}
```

Wired into `Create`, on the `devpod up` command:
```go
cmd := exec.CommandContext(ctx, m.bin, args...)

// Make GITHUB_TOKEN available to DevPod's clone-time credential lookup.
// DevPod runs `git credential fill` on this host (via `devpod agent
// git-credentials`) as a child of this process.
if gitEnv := m.gitCredentialEnv(); gitEnv != nil {
	cmd.Env = append(os.Environ(), gitEnv...)
}
```

Callers threading the token through: `server/main.go` (both `NewManager` sites) and `server/internal/server/server.go`.

## Why This Works

DevPod forwards credentials rather than baking them into the clone. When the in-container clone hits github.com, DevPod invokes `devpod agent git-credentials` **on the host**, which runs `git credential fill`. That process is a **child of the `devpod up` process**, so it inherits `cmd.Env`. `GIT_CONFIG_GLOBAL` points git at our `~/.deuce/gitconfig`, whose `credential.helper = store --file=...` resolves `https://github.com` to `x-access-token:<token>` — exactly what git needs. The credential flows back to the container, the clone authenticates, and the ~60s hang disappears.

- **Isolated config, not `~/.gitconfig`.** Using `GIT_CONFIG_GLOBAL` + `GIT_CONFIG_SYSTEM=/dev/null` means git consults *only* Deuce's config for this subprocess. The operator's real `~/.gitconfig` is never read or written — no risk of clobbering their helpers, and no surprise behavior on shared hosts.
- **`x-access-token` is the right username.** It's GitHub's documented username for PAT-over-HTTPS basic auth; the password field carries the token. GitHub ignores the username content but requires the basic-auth shape, so this is the canonical form.
- **Fail-soft.** Empty token → `nil` env → public clones still work. Setup failure logs a warning and continues; only private clones would then fail, rather than breaking the whole workspace path.

## Prevention

- **Mechanism test (the important one):** `TestGitCredentialFiles_GitResolvesToken` in `server/internal/workspace/git_credentials_test.go` runs the *real* `git credential fill` with the generated env (the exact call DevPod's `devpod agent git-credentials` makes) and asserts the output contains `username=x-access-token` and `password=<token>`. This proves end-to-end resolution, not just that files were written. Skips cleanly if `git` isn't installed.
- **No-op guard:** `TestGitCredentialFiles_EmptyTokenIsNoop` asserts an empty token returns `nil` env and writes zero files — locks in the public-repo path.
- **Perms:** `TestGitCredentialFiles_WritesConfigAndCredentials` asserts the credentials file is `0600` and that the gitconfig's store helper points at it. Keep these — the file holds a live token.
- **Scope limits to remember:**
  - **HTTPS github.com only.** The credential line is hardcoded to `@github.com`. Other hosts (GHE, GitLab) and **SSH remotes** are unaddressed — an SSH clone URL bypasses this entirely.
  - **`~/.deuce` base path must not contain spaces.** Git's `store --file=<path>` helper value is split on whitespace, so a spaced path silently breaks credential resolution. If the base dir ever becomes configurable, validate against spaces.
  - **Token still never goes in the URL** — `validation.go` rejects that on purpose; keep the credential in the isolated store, not the clone URL.

## Related Issues

- PR: [forgeutah/deuce#40](https://github.com/forgeutah/deuce/pull/40) — the fix.
- [`architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md`](../architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md) — same module and DevPod docker-provider domain; covers host-side workspace *file reads*, whereas this doc covers clone-time git credential provisioning.
- [`architecture-patterns/embedded-ssh-proxy-for-vscode-remote.md`](../architecture-patterns/embedded-ssh-proxy-for-vscode-remote.md) — same pattern of the Go server injecting scoped env/identity into a container subprocess (`docker exec` env allowlist vs `GIT_CONFIG_GLOBAL`).
- [`architecture-patterns/pi-loads-agent-skills-standard-in-rpc-mode.md`](../architecture-patterns/pi-loads-agent-skills-standard-in-rpc-mode.md) — another asset provisioned into the DevPod container at session creation.
