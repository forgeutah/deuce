---
title: "feat: Devcontainer config for Deuce development"
type: feat
status: completed
created: 2026-05-12
plan_depth: standard
---

# feat: Devcontainer config for Deuce development

## Summary

Add a `.devcontainer/` at the repo root so contributors can open Deuce in VS Code Dev Containers (or compatible tooling) and have a working environment in one step. The container ships every dependency the README's Quickstart asks the user to install — Node 20+, Go 1.25+, the DevPod CLI, the Go tools (`goose`, `sqlc`, `air`), and Docker access — and reuses the existing `docker-compose.yml` for Postgres so there is one source of truth for the database service.

The devcontainer is for **developing Deuce itself**, not the workspace template Deuce injects into agent sessions (that lives downstream of [docs/plans/2026-05-08-001-feat-real-agents-devcontainer-plan.md](docs/plans/2026-05-08-001-feat-real-agents-devcontainer-plan.md) and is out of scope here).

---

## Problem Frame

Today the README Quickstart asks every new contributor to install Node, Go, Docker, the DevPod CLI, an Anthropic API key, and a GitHub PAT, then run a five-step bring-up that includes copying `.env.example`, running `make migrate`, and launching two long-running processes (Go backend + Vite frontend). That's friction for casual contributors and a frequent source of "it works on my machine" drift across versions of Go, Node, and Postgres.

A devcontainer collapses the install path to "open the repo in VS Code → Reopen in Container", pins the toolchain versions, and gives contributors a known-good environment. It also makes the CLAUDE.md commands (`npm run dev`, `make dev`, `make migrate`, `make generate`) work out of the box without any host-side setup.

---

## Scope Boundaries

### In Scope

- A `.devcontainer/devcontainer.json` (and supporting Dockerfile + post-create script) that brings up a working Deuce dev environment
- Reuse of the existing root `docker-compose.yml` Postgres service via `dockerComposeFile`
- Pinned versions of Go, Node, `goose`, `sqlc`, `air`, and the DevPod CLI
- Auto-installation of frontend deps (`npm install`) and backend Go modules (`go mod download`)
- Auto-copy `server/.env.example` → `server/.env` on first create if missing (does not overwrite an existing `.env`)
- Auto-run `make migrate` once Postgres is reachable
- VS Code customizations: relevant extensions (Go, ESLint, Tailwind CSS IntelliSense, Docker) and minimal workspace settings (Go format-on-save, gofumpt-equivalent)
- Port forwarding for 5173 (Vite), 8080 (Go backend), 5432 (Postgres)
- Docker access for the host's DevPod CLI (docker-outside-of-docker), so the backend's `devpod up` works from inside the container
- A short README section pointing at the new flow

### Deferred to Follow-Up Work

- A devcontainer template that Deuce *injects* into agent workspaces (covered separately by the real-agents plan)
- A `docker compose up` "end-user" bring-up that runs Deuce itself in production-like mode (Roadmap → Foundation)
- GitHub Codespaces parity (see Risks — Codespaces blocks the docker-socket mount this plan relies on)
- Pre-built and published devcontainer image to GHCR (first iteration builds locally)
- VS Code task definitions / launch configs for the Go backend and Vite frontend

### Outside this product's identity

- Replacing `docker-compose.yml` or migrating off `goose`/`sqlc`/`air`
- Changing the README's manual Quickstart for users who don't use VS Code

---

## Key Technical Decisions

1. **Compose-based devcontainer, reusing root `docker-compose.yml`.** Use `dockerComposeFile` pointing at both the root `docker-compose.yml` and a small `.devcontainer/docker-compose.yml` override that adds the dev service. This avoids duplicating the Postgres service and means edits to the existing compose file (volumes, env vars, version bumps) flow through automatically.

2. **Docker-outside-of-docker (DooD), not Docker-in-Docker.** Mount the host's `/var/run/docker.sock` so the in-container DevPod CLI shares the host Docker daemon. DinD would isolate the daemon and create nested-container surprises (DevPod-spawned workspaces nested two levels deep), and is materially slower. Trade-off: this breaks GitHub Codespaces support out of the box (Codespaces blocks the socket mount). Documented as a known limitation, not a blocker — local VS Code is the primary target.

3. **Microsoft `devcontainers/features` for Go and Node, custom Dockerfile for the rest.** Use `ghcr.io/devcontainers/features/go:1` (pinned to Go 1.25) and `ghcr.io/devcontainers/features/node:1` (pinned to Node 20) instead of hand-rolling installs. The Dockerfile starts from `mcr.microsoft.com/devcontainers/base:ubuntu-24.04` and adds only what the features don't cover (DevPod CLI, build deps).

4. **Pin Go tool versions in the post-create script.** `go install github.com/pressly/goose/v3/cmd/goose@v3.x.x`, sqlc and air pinned similarly. Version-pinning prevents toolchain drift from breaking the dev container weeks after creation. Versions are stored in a single `.devcontainer/tool-versions.env` sourced by `post-create.sh` so bumps are one-line edits.

5. **`postCreateCommand` over `onCreateCommand` for the heavy install step.** `postCreateCommand` runs after the workspace is mounted, which is required because we need access to the repo to run `npm install`, `go mod download`, and `make migrate`. `postStartCommand` is reserved for fast checks (DB reachability) so re-attaching to the container stays fast.

6. **Run migrations on every container start, not just on create.** `postStartCommand` runs `cd server && make migrate` so contributors who pull new migrations from main don't have to remember to re-run them. `goose ... up` is idempotent so re-running on every start is safe.

7. **Do not bake API keys into the image.** `ANTHROPIC_API_KEY` and `GITHUB_TOKEN` are surfaced via `containerEnv` referencing host env vars (`${localEnv:ANTHROPIC_API_KEY}`), so the contributor sets them on the host once and they're forwarded into the container without ending up in the Dockerfile or compose file.

---

## High-Level Technical Design

This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

```
.devcontainer/
├── devcontainer.json          # entry point, references compose + Dockerfile
├── docker-compose.yml         # adds 'dev' service to the root compose project
├── Dockerfile                 # base image + features anchor + DevPod CLI
├── post-create.sh             # one-time setup: tools, deps, .env, migrate
├── post-start.sh              # per-start: wait for postgres, run migrations
└── tool-versions.env          # pinned versions: GOOSE_VERSION, SQLC_VERSION, AIR_VERSION, DEVPOD_VERSION

Compose graph (after merge):
  postgres (from root compose)
    └── dev    (added by .devcontainer/docker-compose.yml)
         volumes:
           - ..:/workspaces/deuce:cached
           - /var/run/docker.sock:/var/run/docker.sock  # DooD
         depends_on: [postgres]
```

---

## Output Structure

```
.devcontainer/
├── devcontainer.json
├── docker-compose.yml
├── Dockerfile
├── post-create.sh
├── post-start.sh
└── tool-versions.env
```

The per-unit `**Files:**` sections are authoritative. The implementer may collapse, split, or rename if it improves the result.

---

## Implementation Units

### U1. Devcontainer base config (Dockerfile, devcontainer.json, compose override)

**Goal:** Stand up the container definition itself — base image, language features, port forwarding, VS Code customizations, and the compose-file wiring that joins the root `docker-compose.yml` Postgres service.

**Requirements:** Contributors need a `.devcontainer/` that VS Code recognizes and can build into a running container with Postgres reachable.

**Dependencies:** None

**Files:**
- `.devcontainer/devcontainer.json` (create)
- `.devcontainer/docker-compose.yml` (create — extends root compose with the `dev` service)
- `.devcontainer/Dockerfile` (create)

**Approach:**
- `Dockerfile`: base on `mcr.microsoft.com/devcontainers/base:ubuntu-24.04`. Install build essentials (`build-essential`, `curl`, `git`, `make`, `ca-certificates`, `postgresql-client` for `psql`). The Go and Node toolchains come from devcontainer features in `devcontainer.json`, not the Dockerfile.
- `.devcontainer/docker-compose.yml`: declares a `dev` service that builds from the Dockerfile, mounts the workspace at `/workspaces/deuce`, mounts `/var/run/docker.sock`, sets `command: sleep infinity`, and adds `depends_on: postgres`. Uses `network_mode` defaults so Postgres is reachable at hostname `postgres`.
- `devcontainer.json`:
  - `dockerComposeFile`: `["../docker-compose.yml", "docker-compose.yml"]`
  - `service`: `dev`
  - `workspaceFolder`: `/workspaces/deuce`
  - `features`: `ghcr.io/devcontainers/features/go:1` (pinned to Go 1.25), `ghcr.io/devcontainers/features/node:1` (pinned to Node 20), `ghcr.io/devcontainers/features/common-utils:2`
  - `forwardPorts`: `[5173, 8080, 5432]`
  - `portsAttributes`: friendly labels for each port (`Vite`, `Backend`, `Postgres`)
  - `customizations.vscode.extensions`: Go (`golang.go`), ESLint (`dbaeumer.vscode-eslint`), Tailwind CSS IntelliSense (`bradlc.vscode-tailwindcss`), Docker (`ms-azuretools.vscode-docker`), Prettier (`esbenp.prettier-vscode`)
  - `customizations.vscode.settings`: Go format-on-save, default Go formatter, ESLint as default JS/TS formatter, files.eol = `\n`
  - `containerEnv`: `DATABASE_URL`, `DEUCE_USER_ID`, `ANTHROPIC_API_KEY=${localEnv:ANTHROPIC_API_KEY}`, `GITHUB_TOKEN=${localEnv:GITHUB_TOKEN}`, `DEVPOD_PROVIDER=docker`
  - `remoteUser`: `vscode` (provided by base image)
  - `postCreateCommand`: `bash .devcontainer/post-create.sh` (defined in U3)
  - `postStartCommand`: `bash .devcontainer/post-start.sh` (defined in U3)

**Patterns to follow:** Existing `docker-compose.yml` at the repo root for compose conventions. Microsoft's devcontainer feature reference for the canonical features manifest shape — `references/visual-communication.md` and Microsoft docs for any unfamiliar fields.

**Test scenarios:**
- "Reopen in Container" in VS Code completes a build and attaches to the `dev` service
- `docker ps` from inside the container resolves (proves the docker socket mount works)
- `psql -h postgres -U deuce -d deuce -c '\\dt'` from inside the container connects to the Postgres service
- `go version` reports 1.25.x and `node --version` reports v20.x
- Forwarded ports 5173, 8080, 5432 appear in the VS Code Ports panel with the expected labels
- `ANTHROPIC_API_KEY` and `GITHUB_TOKEN` set on the host are visible inside the container (`echo $ANTHROPIC_API_KEY`)
- `ANTHROPIC_API_KEY` unset on the host: container still builds and starts (variable is empty, not an error)

**Verification:** From inside the running container, `psql -h postgres -U deuce -d deuce -c 'SELECT 1'` returns 1 and `go version` + `node --version` report the pinned versions.

---

### U2. Tool version pin file

**Goal:** Centralize pinned versions for `goose`, `sqlc`, `air`, and the DevPod CLI in a single file so version bumps are a one-line change and the post-create script reads from one place.

**Requirements:** Reproducible toolchain across contributors and over time (Decision 4).

**Dependencies:** None (can land in parallel with U1)

**Files:**
- `.devcontainer/tool-versions.env` (create)

**Approach:**
- Plain `KEY=value` env file, sourced via `set -a; . tool-versions.env; set +a` in `post-create.sh`
- Variables: `GOOSE_VERSION`, `SQLC_VERSION`, `AIR_VERSION`, `DEVPOD_VERSION`
- Pin to current latest stable at implementation time. Implementer should grab the current latest stable from each project's GitHub releases at the moment of authoring rather than guessing — exact version strings are deferred (see Deferred Implementation Notes)
- Comment at the top of the file explaining the bump process: "edit a value, rebuild the devcontainer, verify CLAUDE.md commands still work"

**Patterns to follow:** None — small new file, conventional shell-sourceable env format.

**Test scenarios:**
- File is sourceable by `bash` without errors
- All four variables are non-empty after sourcing

**Verification:** `bash -c 'set -a && . .devcontainer/tool-versions.env && set +a && [ -n "$GOOSE_VERSION$SQLC_VERSION$AIR_VERSION$DEVPOD_VERSION" ] && echo OK'` prints `OK`.

---

### U3. Post-create and post-start scripts

**Goal:** Install the Go tools and DevPod CLI on first create, hydrate the dev environment (`.env`, frontend deps, Go modules), and re-run migrations on every container start so pulled migrations are applied automatically.

**Requirements:** A contributor opening the container should land on a checkout where `npm run dev` and `cd server && make dev` both work without any further setup (Decisions 4, 5, 6).

**Dependencies:** U1, U2

**Files:**
- `.devcontainer/post-create.sh` (create)
- `.devcontainer/post-start.sh` (create)

**Approach:**

`post-create.sh` (idempotent — safe to re-run):
1. `set -euo pipefail` and source `.devcontainer/tool-versions.env`
2. `go install github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}`
3. `go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}`
4. `go install github.com/air-verse/air@${AIR_VERSION}`
5. Install DevPod CLI: download the pinned release tarball/binary from the DevPod GitHub release URL, place under `/usr/local/bin/devpod`, `chmod +x`. Skip if already present at the pinned version (`devpod version | grep -q "${DEVPOD_VERSION}"`)
6. `cp server/.env.example server/.env` only if `server/.env` does not exist (`[ -f server/.env ] || cp ...`). Never overwrite — protects API keys a contributor has already filled in
7. `cd server && go mod download`
8. `npm install` (root, for the frontend)
9. Echo a final "Devcontainer ready. Try `cd server && make dev` in one terminal and `npm run dev` in another." message

`post-start.sh` (fast, runs on every container start):
1. Wait for Postgres reachability (loop with `pg_isready -h postgres -U deuce` and a 30s timeout). If timeout, print a clear error and exit 0 (don't fail the start)
2. `cd server && make migrate` (idempotent via goose)

Both scripts must be executable (`chmod +x` committed via git mode).

**Patterns to follow:** Repo conventions in CLAUDE.md for the toolchain (sqlc, goose, air, npm). The existing `make migrate` invocation in the README's Quickstart for migration semantics.

**Test scenarios:**
- First container build: `goose`, `sqlc`, `air`, `devpod` are all on `PATH` after post-create
- First container build with no existing `server/.env`: file is created from `server/.env.example`
- First container build with an existing `server/.env` containing a real API key: file is **not** overwritten (key is preserved)
- Second container start (no rebuild): `post-create.sh` is not re-run; `post-start.sh` re-runs and `make migrate` reports no pending migrations on a clean DB
- Pull a branch with a new migration, restart the container: `post-start.sh` applies the new migration without manual intervention
- Postgres unreachable (e.g., compose dependency misorder): `post-start.sh` exits cleanly with a clear log line, container remains usable for non-DB work
- Re-running `post-create.sh` manually does not re-download tools that are already at the pinned version

**Verification:** After a fresh container build, `goose --version`, `sqlc version`, `air -v`, and `devpod version` all report the pinned versions, and `cd server && make dev` boots without prompting for any further setup.

---

### U4. README documentation

**Goal:** Tell contributors the devcontainer exists and what to expect.

**Requirements:** Discoverability — the README is the entry point.

**Dependencies:** U1, U3 (so the documented flow actually works)

**Files:**
- `README.md` (modify — add a short subsection under Quickstart)

**Approach:**
- Add a `### Open in a Dev Container (recommended)` subsection at the top of Quickstart, above the existing manual steps
- Three lines: prerequisite (Docker + VS Code with Dev Containers extension), action ("Reopen in Container"), expectation ("Postgres + tools + migrations all set up; run `cd server && make dev` and `npm run dev` in two terminals")
- Briefly note known limitation: "GitHub Codespaces support is best-effort because the DevPod CLI requires the host Docker socket"
- Leave the existing manual Quickstart in place for non-VS-Code users
- Add a small note in the existing `cp server/.env.example server/.env` step that the devcontainer does this automatically

**Patterns to follow:** Existing README tone and structure (concise, action-oriented).

**Test scenarios:** None — documentation-only change. `Test expectation: none -- documentation update with no behavioral surface.`

**Verification:** Render the README locally (or in a markdown viewer) and confirm the new section is the first thing under Quickstart with no broken links.

---

## System-Wide Impact

- **Repo layout:** New top-level `.devcontainer/` directory. No existing files moved or renamed.
- **`docker-compose.yml`:** Unchanged in structure, but now also referenced from `.devcontainer/devcontainer.json` via `dockerComposeFile`. Any future edits flow through automatically.
- **CI:** Untouched. The devcontainer is a developer convenience and is not used by CI. (CI continues to install Go/Node directly.)
- **README:** New subsection under Quickstart; existing manual flow preserved for non-VS-Code users.
- **Performance:** First container build downloads images and installs tools (~2–4 minutes on a warm Docker cache). Subsequent attaches are seconds.
- **Security:** Mounting `/var/run/docker.sock` grants the container root-equivalent on the host Docker daemon. This is standard for DooD setups but worth being explicit about — documented in the README known-limitation note.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| GitHub Codespaces blocks `/var/run/docker.sock` mount | Codespaces users can't run DevPod from the devcontainer | Document the limitation in README. Codespaces parity is deferred (see Scope Boundaries). Fallback for Codespaces users is the existing manual Quickstart. |
| Devcontainer feature versions drift (Microsoft's `:1` tag is a moving target for minor updates within Go 1.x / Node 20.x) | Subtle toolchain changes between contributor environments | Pin to specific feature versions only if drift becomes a problem in practice. The `tool-versions.env` file gives us a fast path to pin language toolchains too if needed. |
| `go install` of `goose`/`sqlc`/`air` slow on first build | First-time setup feels sluggish | Acceptable trade-off for version pinning. Could be cached in a future iteration via a pre-built devcontainer image published to GHCR (deferred). |
| `npm install` failures on first build (registry hiccups, peer-dep churn) | Container reaches `vscode` shell but frontend doesn't run | `post-create.sh` should not abort on `npm install` failure — log a clear error and continue, so the container is still usable for backend work. Contributor re-runs `npm install` manually. |
| Host has no `ANTHROPIC_API_KEY` / `GITHUB_TOKEN` set | Variables forward as empty strings, real-agent execution and repo discovery silently broken | Document in README that these env vars must be set on the host before launching the container. Match the existing `.env.example` pattern. |
| Existing `server/.env` accidentally overwritten | Loss of contributor's real API keys | `post-create.sh` checks for file existence before copying (Decision 7, U3 step 6). |
| Docker socket mount grants broad host access | Security note for contributors | Mention explicitly in README (Decision 2 + System-Wide Impact). Standard DooD trade-off. |

---

## Deferred Implementation Notes

- **Exact pinned versions** for `GOOSE_VERSION`, `SQLC_VERSION`, `AIR_VERSION`, `DEVPOD_VERSION` will be set at implementation time by checking each project's latest stable release. Deferring rather than guessing avoids planning-time staleness.
- **DevPod install command** may need adjustment depending on whether the official install script is preferred over a direct binary download. Both work; the direct binary route is more deterministic but requires architecture detection (`x86_64` vs `arm64`).
- **Go feature version selection** (`ghcr.io/devcontainers/features/go:1` vs a more specific minor pin like `:1.25`) — start with `:1` and the `version: "1.25"` option, escalate to a fully pinned ref only if drift becomes a problem.
- **`network_mode` for the `dev` service** — using compose's default network should be enough for `postgres` to be reachable as a hostname, but if conflicts arise with the existing `pgdata` volume name or default network, a `networks:` block scoped to the devcontainer compose file may be needed.
- **Whether to install Claude Code in the dev container itself** (so contributors can use `claude` from inside the devcontainer) — out of scope for this plan unless it surfaces as a need during implementation; the agent execution pathway installs Claude Code into the *agent* workspace, not this dev container.

---

## Verification

The plan is complete when:

- A fresh `git clone` followed by "Reopen in Container" in VS Code produces an attached container in which:
  - `cd server && make dev` boots the backend with hot reload
  - `npm run dev` boots the frontend
  - `http://localhost:5173` loads with the backend reachable at `/api`
  - `make migrate-status` shows all migrations applied
  - `devpod version` reports the pinned version
- The README's Quickstart points contributors at the devcontainer flow first, with the manual flow preserved as a fallback.
