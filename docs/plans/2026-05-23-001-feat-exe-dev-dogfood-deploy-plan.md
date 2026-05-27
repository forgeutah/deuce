---
title: Deploy Deuce to exe.dev VM via GitHub Actions
type: feat
status: active
date: 2026-05-23
deepened: 2026-05-23
origin: docs/brainstorms/2026-05-23-exe-dev-dogfood-deploy-requirements.md
---

# Deploy Deuce to exe.dev VM via GitHub Actions

## Summary

Stand up a single-VM internal-dogfood deployment of Deuce on exe.dev. Build one Docker image per commit (Go server with the Vite/React `dist/` embedded), push to GHCR via GitHub Actions, and SSH to the VM to run a two-service Docker Compose stack (`app` + `postgres`). Migrations run in-process at app start. Auth is `forge-proxy` mode; Forge fronts the VM and injects signed identity headers.

---

## Problem Frame

Deuce has no deployed environment today and cannot be dogfooded by more than one person at a time. The team can't experience the "shared workspace for human + agent collaboration" thesis until Deuce is running somewhere two people can hit at once. See origin for the full pain narrative.

---

## Requirements

- R1. A GitHub Actions workflow runs on push to `main` and builds, pushes, and deploys without human intervention.
- R2. The pipeline runs lint, typecheck, and Go tests before building the image; failures block the deploy.
- R3. The Docker image is built once per commit and tagged with both the commit SHA and `latest`; the SHA tag is what the VM pulls.
- R4. The image is pushed to GHCR using the workflow's built-in `GITHUB_TOKEN`; no third-party registry credentials.
- R5. The VM runs `app` and `postgres` as a two-service Docker Compose stack; Postgres data lives on a named Docker volume on the persistent VM disk.
- R6. The Go server serves the embedded React build for all non-API routes (catch-all → `index.html`) and `/api/*` + `/ws*` as today.
- R7. Database migrations run on app-container start via `goose up` against the on-box Postgres; the app does not start serving if migrations fail.
- R8. Rollback to any previously-pushed image SHA is possible via a `workflow_dispatch` run with an image-tag input. No manual SSH required for rollback.
- R9. Deploy logs (build, push, SSH) are visible in the GHA run; an engineer can diagnose a failed deploy from the GHA UI alone in the common case.
- R10. Deploy authenticates to the VM via a dedicated SSH key whose private half lives only in GitHub Actions secrets; the user provisions and rotates this key out-of-band.
- R11. Production secrets consumed by the app (`DATABASE_URL`, `DEUCE_AUTH_MODE=forge-proxy`, `FORGE_PROXY_SECRET`, `FORGE_REQUIRED_ROLE`, `DEUCE_WS_ALLOWED_ORIGINS`, `DEUCE_CORS_ALLOWED_ORIGINS`, `GITHUB_TOKEN`) live in the VM-side `.env` file, not in the GHA workflow or the image.

**Origin flows:** F1 (push-to-main deploy), F2 (manual rollback)
**Origin acceptance examples:** AE1 (failed Go tests block deploy, covers R2), AE2 (failed migration prevents partial-serving, covers R7), AE3 (rollback via `workflow_dispatch` with a previous SHA, covers R8), AE4 (VM reboot brings the stack back up, covers R5/R11)

---

## Scope Boundaries

- No custom domain / TLS beyond what exe.dev's reverse proxy assigns automatically. The deployment is reachable at `https://<vmname>.exe.xyz/` for v1.
- No backups of any kind (no `pg_dump`, no off-box snapshots). Data loss is an accepted risk for the dogfood instance.
- No per-PR preview VMs. Single long-lived VM only.
- No exe.dev BYO-VM-image path. The image is pulled into a stock `exeuntu` VM by Docker Compose.
- No zero-downtime / blue-green deploy. A few seconds of 502 during `docker compose up -d app` is acceptable.
- No staging environment. `main` deploys straight to the dogfood VM.
- No Terraform / IaC for the VM. The VM is provisioned once by hand against exe.dev.
- No GHA-side infrastructure beyond what GitHub provides for free (no self-hosted runners, no third-party registries).

### Deferred to Follow-Up Work

- Wiring `DEUCE_CORS_ALLOWED_ORIGINS` into a richer policy (per-route allowlists, agent-callable surfaces audit): this plan only makes the existing allowlist env-driven so the deploy can function. Broader agent-callable parity per STRATEGY's "Coding & Preview" track is a separate effort.

---

## Context & Research

### Relevant Code and Patterns

- `server/internal/server/server.go` — single chi v5 `Router()` function. New static handler mounts at the **end** (after `/api`, `/ws`, `/ws/terminal/*`), before `return r`. CORS middleware is currently constructed with a hardcoded `[]string{"http://localhost:4000", "http://localhost:8080"}` at the top of `Router()`.
- `server/internal/config/config.go` — single config struct, `caarlos0/env/v11` + `godotenv`. `Validate()` already gates forge-proxy mode on `FORGE_PROXY_SECRET`, `FORGE_REQUIRED_ROLE`, and a non-empty `DEUCE_WS_ALLOWED_ORIGINS` (no wildcard). `FORGE_CONTRACT_VERSION` defaults to `1`. Extend with `CORSAllowedOrigins` and a sibling `CORSAllowedOriginList()` method.
- `server/main.go` — composition root. Calls `config.Load()`, opens DB pool, builds router via `srv.Router()`, calls `http.ListenAndServe`. Insert migration step after DB-pool open and before `srv.Router()`.
- `server/internal/db/migrations/` — 6 existing goose-formatted SQL migrations (`001_initial_schema.sql` through `005_session_description.sql`, plus `002_seed_data.sql`). Already compatible with `goose.SetBaseFS` — no rewrites needed.
- `server/Makefile` — `make migrate` runs `goose -dir internal/db/migrations postgres "$(DATABASE_URL)" up`. Keep this for local dev; the deployed app uses the embedded library invocation instead.
- `docker-compose.yml` (root) — dev-only, single `postgres:17` service exposing `5432`. Leave untouched; prod compose is a separate file at `deploy/compose.yaml`.
- `vite.config.ts` — default `outDir: dist/`, no `base` override. SPA shell at root `index.html` mounts `src/main.tsx`. TanStack Router is declared in `package.json` but not actually wired up (no `createRouter` call); the SPA-fallback rule still belongs in the plan as future-proofing.

### Institutional Learnings

None applicable. `docs/solutions/` only contains `architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md`, which is about an in-app data plane. After this lands, capturing the GHCR + SSH-pull topology + the goose-on-startup decision under `docs/solutions/` is worth doing via `/ce-compound`.

### External References

- exe.dev docs (markdown alternates of the JS-rendered pages): [proxy](https://exe.dev/docs/proxy.md), [api](https://exe.dev/docs/api.md), [sharing](https://exe.dev/docs/sharing.md), [docker FAQ](https://exe.dev/docs/faq/docker.md), [host key](https://exe.dev/docs/faq/host-key.md), [cnames](https://exe.dev/docs/cnames.md), [serverful](https://exe.dev/docs/serverful.md), [copy-files FAQ](https://exe.dev/docs/faq/copy-files.md).
- GitHub Actions 2026 stack: [actions/checkout v5](https://github.com/actions/checkout/releases), [setup-go v6](https://github.com/actions/setup-go), [docker/build-push-action v7](https://github.com/docker/build-push-action/releases), [GHA cache backend](https://docs.docker.com/build/cache/backends/gha/), [GHA SHA-pinning policy (Aug 2025)](https://github.blog/changelog/2025-08-15-github-actions-policy-now-supports-blocking-and-sha-pinning-actions/).
- [Go embed package](https://pkg.go.dev/embed), [pressly/goose v3 godoc](https://pkg.go.dev/github.com/pressly/goose/v3).
- [Compose depends_on healthcheck docs](https://docs.docker.com/compose/how-tos/startup-order/).
- [appleboy/ssh-action](https://github.com/appleboy/ssh-action) (still the de-facto SSH-deploy action in 2026; SHA-pin per current policy).

---

## Key Technical Decisions

- **Migrations run in-process via `goose.SetBaseFS` + `goose.UpContext` from `main.go`, before the HTTP listener opens.** Single artifact, atomic with the binary version, fail-fast on bad migrations, no race between a separate migrate step and app start. The existing `make migrate` flow (CLI-based) is kept for local dev only. Safe under concurrent boots because goose acquires a Postgres advisory lock before applying. **Reversal path:** because `RunMigrations` lives in its own package (U2), extracting it into a Compose `migrate` one-shot service (`restart: no`, `app` `depends_on: { condition: service_completed_successfully }`) is mechanically straightforward if the team ever runs >1 app replica. The choice is deliberate-and-reversible, not coincidentally-reversible.
- **Migrations are forward-only by convention.** Rollback (R8 / AE3) deploys a prior image but does NOT down-migrate the database. Operators must not roll back across a destructive migration boundary (dropped column, narrowed type) — the older image will hit a schema it doesn't understand. The runbook in U7 documents this as a known constraint; adding a `migrate-down` subcommand is deferred follow-up.
- **Forge-proxy is the deployed auth mode.** `DEUCE_AUTH_MODE=forge-proxy` plus the required forge env vars in the VM-side `.env`. Forge fronts the VM as a reverse proxy and injects the signed identity headers the existing middleware expects. Forge IS the in-app auth boundary.
- **Forge-proxy bypass mitigation is operator-level for v1, not middleware-level.** The forge-proxy middleware authenticates by validating a shared-secret header; anyone who reaches `https://<vmname>.exe.xyz/` directly with the secret + crafted identity headers is fully authenticated as any user. The architecturally correct fix is an IP allowlist in the middleware (env-driven CIDR list, reject other source IPs before the secret compare). This plan does NOT add that code — it's an out-of-scope change to the forge-proxy track. For v1 the operator MUST ensure `<vmname>.exe.xyz` is unreachable from the open internet at the network layer: use exe.dev's `share port` access control + IAM to restrict the public port to Forge's egress, OR park the VM behind a non-guessable subdomain and treat the URL itself as a secret. Documented in U7 §5 and §9. Acceptable for dogfood; **must** be revisited via a middleware change before sharing with non-team users.
- **CORS allowlist becomes env-driven (`DEUCE_CORS_ALLOWED_ORIGINS`).** Forge serves the SPA from its own origin and proxies API/WS calls to the exe.dev VM; the browser's `Origin` header carries Forge's origin and reaches the Go server. Without this change, the current hardcoded localhost allowlist would reject every request before the forge-proxy middleware runs.
- **Single multi-stage `Dockerfile` at repo root with Vite `dist/` embedded into the Go binary.** Stage 1 Node + `npm ci` + `vite build`, stage 2 Go + `COPY --from=frontend /app/dist ./internal/web/dist` + `go build` with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, stage 3 `gcr.io/distroless/static-debian12:nonroot` with the binary and `EXPOSE 8080`.
- **`distroless/static-debian12:nonroot` over alternatives.** Correct for a CGO_ENABLED=0 static Go binary: `distroless/base-debian12` carries glibc + libssl needed only by CGO builds (wasted bytes + attack surface); `alpine` introduces musl (subtle DNS/TLS/timezone differences vs. the Linux dev environment) and a shell (extra attack surface for no benefit); `chainguard/static` is a fine peer but introduces a third-party registry dependency where the GoogleContainerTools image is already first-party and zero-cost.
- **App listens on a fixed port (8080) and Dockerfile declares `EXPOSE 8080`.** exe.dev's proxy auto-selects from `EXPOSE` directives (prefers 80, otherwise smallest TCP ≥ 1024). A single fixed port is simpler than 80 and avoids permissions surprises with non-root distroless.
- **Two compose files, not one with profiles.** `docker-compose.yml` stays as the dev-only Postgres-publishing stack. `deploy/compose.yaml` is the new prod stack and never gets `docker compose up`'d locally. Dev and prod have shape differences (dev publishes `:5432`, prod doesn't; dev has no healthcheck wiring, prod gates `app` on `service_healthy`; dev env comes from the developer's shell, prod from a managed VM `.env`) — these are shape differences, not toggle differences, so profiles would obscure them.
- **Postgres pinned to `postgres:17.5-alpine` in the prod compose**, matching the dev `postgres:17` major. Compose `depends_on` with `condition: service_healthy` gates app startup on Postgres readiness; in-process retry-with-backoff is the second safety net.
- **Container hardening defaults (U5):** `cap_drop: [ALL]` on both services, `security_opt: [no-new-privileges:true]` on both, `read_only: true` + `tmpfs: /tmp` on the app (distroless static has no writable runtime paths), `user: "70:70"` on Postgres so PID 1 is non-root. These additions are five minutes to bake in now and painful to retrofit after the Postgres volume has been written under root-owned files.
- **GHCR auth via `GITHUB_TOKEN`** with `permissions: { contents: read, packages: write }` on the workflow. No PAT, no OIDC. Image tags: `ghcr.io/forgeutah/deuce:<sha>` (canonical) and `:latest` (convenience). The VM pulls by SHA tag, not `:latest`.
- **SSH deploy via plain `ssh <vmname>.exe.xyz` from the runner** using a dedicated ed25519 deploy key (no passphrase) registered on exe.dev via `ssh-key add`. Host fingerprint pinned to exe.dev's published value `SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo`. Use `appleboy/ssh-action@<sha-pinned>` with `fingerprint:` input. The deploy key's `authorized_keys` entry restricts to a forced command (see U7) so a leaked GHA secret cannot become a full interactive shell on the VM.
- **Initial VM bootstrap is documented manual work, not codified in IaC.** First-time: `ssh exe.dev new` to create the VM, register CI deploy key with restricted `authorized_keys`, `scp deploy/compose.yaml deploy/.env.example` to `/opt/deuce/`, edit `.env` with real secrets, run `docker compose up -d postgres` once to materialize the volume. Subsequent deploys are CI-driven.
- **No build/push when CI is run by a fork PR.** Workflow gates the deploy job on `github.event_name == 'push'` (or `workflow_dispatch`) and `github.repository == 'forgeutah/deuce'`. PRs run lint/typecheck/test only.

---

## Open Questions

### Resolved During Planning

- **Migration mechanism (CLI vs library)** — embedded library invocation (`goose.SetBaseFS` + `goose.UpContext`). See Key Technical Decisions.
- **Compose layout (one file with profiles vs two files)** — two files. See Key Technical Decisions.
- **Static-file SPA fallback semantics under `embed.FS`** — gate fallback on `fs.Stat`, not URL path-prefix, so hashed `/assets/*` 404 cleanly instead of silently returning the SPA shell. Cache `index.html` with `no-cache`; cache `/assets/*` with `immutable, max-age=31536000`.
- **exe.dev port mechanics** — `EXPOSE 8080` in the Dockerfile is sufficient; the proxy auto-routes. No platform-injected `PORT` env var.
- **Auth-mode choice** — `forge-proxy`, confirmed by user. See Key Technical Decisions.
- **Cross-plan dependency on `feat/forge-proxy-auth`** — the forge-proxy track (branch `feat/forge-proxy-auth`, plan `docs/plans/2026-05-22-001-feat-forge-proxy-auth-mode-plan.md`) **must be merged to `main` before** this deploy plan's first push lands. If the deploy ships against a `main` that lacks forge-proxy code, `config.Validate()` will reject the prod env contract at startup and the container will restart-loop. This is treated as a sequencing constraint on the merge order; also surfaced as a Risk row for visibility.

### Deferred to Implementation

- **Exact Forge upstream origin string for `DEUCE_CORS_ALLOWED_ORIGINS` and `DEUCE_WS_ALLOWED_ORIGINS`** — depends on which Forge environment(s) front the dogfood VM. The implementer puts placeholder values in `deploy/.env.example` and the user fills in the real origins when seeding the VM's `.env`.
- **exe.dev VM name** — user-chosen at provisioning time. Plan uses `<vmname>` as a placeholder; `deploy/.env.example` and `docs/deploy.md` both flag it.
- **GHA secret names exactly** — the plan proposes `DEPLOY_SSH_KEY`, `DEPLOY_SSH_HOST`, `DEPLOY_SSH_USER`, but the implementer may adjust to match any existing repo conventions when the workflow file is written.
- **Whether to register the host fingerprint as a separate secret or hardcode exe.dev's published value** — the implementer chooses based on operational preference; both are acceptable since the fingerprint is public.

---

## High-Level Technical Design

> *This illustrates the intended deploy flow and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```mermaid
sequenceDiagram
    autonumber
    participant Dev as Developer
    participant GH as GitHub
    participant CI as GHA Runner
    participant GHCR
    participant VM as exe.dev VM
    participant PG as Postgres (compose svc)
    participant App as App container

    Dev->>GH: git push main
    GH->>CI: trigger deploy.yml
    CI->>CI: lint + tsc + go test
    Note over CI: build job blocks on success
    CI->>CI: docker buildx build (multi-stage)
    CI->>GHCR: docker push :<sha> + :latest
    CI->>VM: ssh + sed image tag in /opt/deuce/.env
    CI->>VM: docker compose pull app && up -d app
    VM->>GHCR: pull image by SHA
    App->>PG: pool.Ping (retry with backoff)
    App->>PG: goose.UpContext (embedded migrations)
    App->>App: bind :8080, serve /api, /ws, /*
    Note over VM,App: exe.dev proxy routes :8080 -> https://<vmname>.exe.xyz/
```

```mermaid
flowchart LR
    Browser -->|"Origin: forge.tld"| Forge
    Forge -->|"X-Forge-* signed headers"| ExeProxy["exe.dev proxy"]
    ExeProxy --> AppContainer
    AppContainer -->|"unix socket / 5432"| Postgres
```

The second diagram shows the runtime request path: Forge is the authentication boundary; `<vmname>.exe.xyz` is the transport endpoint, not the SPA's origin.

---

## Implementation Units

### U1. Serve embedded SPA from chi router

**Goal:** Add an `embed.FS` of the Vite `dist/` output and mount a catch-all static handler on the chi router that serves real files when they exist and falls back to `index.html` otherwise.

**Requirements:** R6

**Dependencies:** none

**Files:**
- Create: `server/internal/web/web.go`
- Create: `server/internal/web/web_test.go`
- Modify: `server/internal/server/server.go` (mount the static handler at the end of `Router()`, after API and WS routes, before `return r`)

**Approach:**
- New `server/internal/web/` package owns the `//go:embed all:dist` directive, the `fs.Sub(distFS, "dist")` unwrap, and the SPA handler. Use `all:` prefix so files starting with `_` aren't skipped.
- The handler:
  1. `path.Clean` the request path, strip leading `/`, empty → `"index.html"`.
  2. `fs.Stat` the requested file. If missing, rewrite `r.URL.Path = "/"` and serve via `http.FileServer(http.FS(sub))`.
  3. Set `Cache-Control: public, max-age=31536000, immutable` when the request path starts with `/assets/`; otherwise `Cache-Control: no-cache`.
- Register on chi as `r.Handle("/*", web.Handler())` at the bottom of `Router()`. API routes (`/api/*`, `/ws`, `/ws/terminal/*`) are registered earlier and take precedence — chi resolves most-specific first.
- The package exposes a single `Handler() http.Handler` function (no global state, no panics).
- **Dist embedding wiring:** the Dockerfile (U4) is responsible for copying the Vite build output into `server/internal/web/dist/` before `go build` runs. For local Go-only builds without the frontend, the build will fail unless an empty placeholder file exists — add `server/internal/web/dist/.gitkeep` (committed) and add `server/internal/web/dist/*` (except `.gitkeep`) to `.gitignore` so the real build artifacts are never checked in.
- **Document the dist contract.** Add a `doc.go` (or package comment on `web.go`) stating the invariant: *"Callers must populate `server/internal/web/dist/` before `go build`. The Dockerfile (`Dockerfile` at repo root) is the production populator; `npm run build && cp -r dist server/internal/web/dist` is the local equivalent. A `make embed-dist` target wraps this for convenience."* Without this comment, the next contributor adding a `web_test.go` outside CI will hit a confusing embed-error and waste time tracing it.
- Add `embed-dist` target to `server/Makefile`: builds the frontend at repo root and copies `dist/` into `server/internal/web/dist/`. Local-only convenience, not consumed by Docker (the Dockerfile does the copy via `COPY --from=frontend`).

**Patterns to follow:**
- The standard Go-embed-SPA-with-fallback idiom documented at [pkg.go.dev/embed](https://pkg.go.dev/embed) plus the `fs.Stat`-gated fallback pattern.

**Test scenarios:**
- Happy path: GET `/` returns 200, content-type `text/html`, body contains `<div id="root">`.
- Happy path: GET `/assets/<existing-hashed-file>.js` returns 200, content-type `application/javascript`, response header `Cache-Control` contains `immutable`.
- Edge case: GET `/some/unknown/deep/path` returns 200 with `index.html` body and `Cache-Control: no-cache` (SPA fallback).
- Edge case: GET `/assets/does-not-exist.js` returns 404 (NOT the SPA shell — this asserts the `fs.Stat` gate).
- Edge case: GET `/index.html` returns 200 with `Cache-Control: no-cache`.
- Integration: with the static handler registered on a chi router that already has an `/api/whatever` route, a GET to `/api/whatever` reaches the API handler (not the static fallback). Verify by registering both routes in the test and asserting the response body is the API handler's output, not `index.html`.
- Smoke: the embedded `fs.FS` contains at least `index.html` (asserts the embed directive is wired correctly).

**Verification:**
- `go test ./internal/web/...` passes.
- Running the server locally with a populated `server/internal/web/dist/index.html` returns the SPA at `http://localhost:8080/` and 404s for `/assets/missing.js`.

---

### U2. Run goose migrations in-process at startup

**Goal:** Replace the assumed CLI-invoked migration step with an embedded, in-process `goose.UpContext` call from `main.go` that runs after DB-pool open and before the HTTP listener binds.

**Requirements:** R7

**Dependencies:** none (independent of U1)

**Files:**
- Create: `server/internal/db/migrate.go`
- Create: `server/internal/db/migrate_test.go`
- Modify: `server/main.go` (insert the migration call after DB pool open, before `srv.Router()`)
- Modify: `server/go.mod` and `server/go.sum` (add `github.com/pressly/goose/v3`)

**Approach:**
- New `server/internal/db/migrate.go` owns `//go:embed migrations/*.sql` and exposes `RunMigrations(ctx context.Context, db *sql.DB) error`. Inside:
  1. `goose.SetBaseFS(embeddedFS)`
  2. `goose.SetDialect("postgres")`
  3. `goose.UpContext(ctx, db, "migrations")`
- The pgx pool used by app handlers stays; convert to a `*sql.DB` for goose only via `stdlib.OpenDBFromPool(pool)` (pgx stdlib adapter). The migration `*sql.DB` is closed immediately after `RunMigrations` returns; goose owns no long-lived handle. (Note: `stdlib.OpenDBFromPool` may or may not be the exact symbol depending on pgx version — implementer confirms against `github.com/jackc/pgx/v5/stdlib` godoc at write time. If the adapter is awkward, opening a parallel `sql.Open("pgx", databaseURL)` for migrations is acceptable.)
- Wrap the call in `main.go` with a wait-for-Postgres retry loop (5 attempts, exponential backoff with jitter, hard ceiling of 60 s via `context.WithTimeout`). On failure, `log.Fatalf("migrations failed: %v", err)` — Compose's `restart: unless-stopped` will surface the loop without partial-serving.
- The retry loop pings `pool.Ping(ctx)` first; only after the ping succeeds does migration run.
- **Migrations are forward-only by convention** (see Key Technical Decisions). `RunMigrations` calls `Up`, never `Down`. The package does not expose `RunMigrationsDown`; if down-migration becomes needed, it's a follow-up unit that should add a separate `migrate-down` subcommand to the binary so that rollback semantics are explicit at the operational layer, not silently invoked by app start.
- **Reversal path for multi-replica future:** keeping `RunMigrations` in its own package means it can be lifted into a Compose `migrate` one-shot service later (`restart: no`, `app` `depends_on: { condition: service_completed_successfully }`) without touching app code. Documented here so the decision is deliberate-and-reversible.

**Patterns to follow:**
- The canonical `goose.SetBaseFS` library-invocation pattern from [pressly/goose v3 godoc](https://pkg.go.dev/github.com/pressly/goose/v3).

**Test scenarios:**
- Happy path: against a freshly-created test DB, `RunMigrations` returns nil and `SELECT count(*) FROM goose_db_version` returns one row per embedded migration plus one baseline row (don't hard-code the count — the forge-proxy branch already adds a 7th migration and more will land).
- Idempotency: a second call to `RunMigrations` on the same DB is a no-op (returns nil, `goose_db_version` count unchanged).
- Smoke: every `.sql` filename in `server/internal/db/migrations/` is present in the embedded `fs.FS`. (Asserts the embed directive captures the whole directory; avoids hard-coding the file count since the forge-proxy branch adds `006_user_forge_id.sql` and more migrations will follow.)
- **Covers AE2.** Error path: when a deliberately broken migration is injected via a test-only `goose.SetBaseFS` override (or via a parallel test fixture), `RunMigrations` returns a non-nil error and the DB is not left in a partially-applied state (verify by querying for the broken migration's table — must not exist).
- Integration: in a `main_test.go`-style test or a small integration harness, boot the app against an empty test Postgres, observe that the HTTP listener does NOT come up if `RunMigrations` fails (process exits non-zero).

**Verification:**
- `go test ./internal/db/...` passes against a real Postgres test container.
- Local: `docker compose -f docker-compose.yml up -d postgres`, then `go run ./server` against an empty DB, app boots and `psql` shows the expected schema. Re-running is a no-op.

---

### U3. Make CORS allowlist env-driven

**Goal:** Replace the hardcoded `[]string{"http://localhost:4000", "http://localhost:8080"}` CORS allowlist in `server.go` with values pulled from a new `DEUCE_CORS_ALLOWED_ORIGINS` env var, defaulting to the current localhost values. This is the change that makes the forge-proxy deploy actually accept Forge-origin browser requests.

**Requirements:** R11 (extends the env contract)

**Dependencies:** none (independent of U1, U2)

**Files:**
- Modify: `server/internal/config/config.go` (add `CORSAllowedOrigins` field with `envDefault:"http://localhost:4000,http://localhost:8080"` and a `CORSAllowedOriginList()` helper that mirrors `WSAllowedOriginList()`)
- Modify: `server/internal/server/server.go` (pass `cfg.CORSAllowedOriginList()` into the `cors.Handler` construction at the top of `Router()` instead of the hardcoded slice)
- Modify: `server/.env.example` (document the new var)
- Modify: `server/internal/config/config_test.go` (extend existing test for the new field and validation)

**Approach:**
- New field on `Config`:
  ```
  CORSAllowedOrigins string `env:"DEUCE_CORS_ALLOWED_ORIGINS" envDefault:"http://localhost:4000,http://localhost:8080"`
  ```
- New method `CORSAllowedOriginList() []string` — implementation mirrors `WSAllowedOriginList()` (split on `,`, trim, drop empties).
- In `Validate()`, reject the following in the CORS list (footgun guards — each one is a footgun even when the framework would silently ignore it):
  - `*` wildcard (re-opens CSRF surface with credentials)
  - entries without an explicit scheme (`forge.tld` instead of `https://forge.tld`) — silently inconsistent across chi-cors versions
  - entries with paths or query (`https://app.forge.tld/foo`) — silently accepted, never matches
  - upper-case scheme (`HTTPS://`) — origin matching is case-sensitive in browsers
- `Router()` reads the slice from config and passes it through to the existing chi-cors middleware. No change to allowed methods/headers list. Verify (in a test) that `Access-Control-Allow-Credentials: true` is set so cookies/auth headers cross-origin; this is required for forge-proxy mode where Forge passes auth cookies through.
- Default value matches today's hardcoded behavior so existing local dev continues to work without `.env` changes.

**Patterns to follow:**
- The existing `WSAllowedOrigins` / `WSAllowedOriginList` shape in `server/internal/config/config.go` — same field, same helper signature, same validation discipline.

**Test scenarios:**
- Happy path: when `DEUCE_CORS_ALLOWED_ORIGINS=https://app.forge.tld,https://staging.forge.tld`, `cfg.CORSAllowedOriginList()` returns the two-element slice in order with whitespace trimmed.
- Edge case: when the env var is unset, `CORSAllowedOriginList()` returns the two-element default localhost slice.
- Edge case: when the env var contains stray whitespace and empty entries (`" a, ,b "`), the result is `["a", "b"]`.
- Error path: `Validate()` rejects each footgun individually with a recognizable error — `*`, scheme-less (`forge.tld`), path-bearing (`https://app.forge.tld/foo`), upper-case scheme (`HTTPS://forge.tld`).
- Integration: with `cfg.CORSAllowedOrigins=https://forge.tld`, a request to any chi route with `Origin: https://forge.tld` receives the appropriate `Access-Control-Allow-Origin: https://forge.tld` response header **and** `Access-Control-Allow-Credentials: true`; a request with `Origin: https://evil.tld` does not.

**Verification:**
- `go test ./internal/config/... ./internal/server/...` passes.
- Manual: set `DEUCE_CORS_ALLOWED_ORIGINS=https://example.com`, start the server, `curl -i -H "Origin: https://example.com" http://localhost:8080/api/health` returns an `Access-Control-Allow-Origin` echoing that origin.

---

### U4. Multi-stage Dockerfile + .dockerignore

**Goal:** Produce a deployable Docker image with the Vite build embedded into the Go binary, sized small enough for fast pulls, running as non-root on `gcr.io/distroless/static-debian12:nonroot`.

**Requirements:** R3 (image tags via labels), R5 (image is the `app` service source)

**Dependencies:** U1 (the embed package and `server/internal/web/dist/` placeholder exist), U2 (the embedded-migrations binary is what gets built)

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Modify: `.gitignore` (add `server/internal/web/dist/*` with `!server/internal/web/dist/.gitkeep` exception, and add root-level `dist/` if not already excluded)

**Approach:**
- Three stages:
  1. `node:22-bookworm-slim AS frontend` — `WORKDIR /app`, `COPY package*.json ./`, `RUN npm ci`, `COPY . .`, `RUN npm run build`. Output at `/app/dist`.
  2. `golang:1.25-bookworm AS backend` — `WORKDIR /src`, `COPY server/go.mod server/go.sum ./server/`, `RUN cd server && go mod download`, `COPY server ./server`, `COPY --from=frontend /app/dist ./server/internal/web/dist`, `RUN cd server && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/deuce .`.
  3. `gcr.io/distroless/static-debian12:nonroot` — `COPY --from=backend /out/deuce /deuce`, `EXPOSE 8080`, `USER 65532:65532`, `ENTRYPOINT ["/deuce"]`. No shell, no package manager.
- `.dockerignore` excludes: `.git`, `node_modules`, `dist`, `server/tmp`, `server/bin`, `.devcontainer`, `docs`, `*.md`, `.env*` (everywhere), `.github`, `.claude`.
- Build context is the **repo root** so both `server/` and the frontend sources (`package.json`, `src/`, `vite.config.ts`, `index.html`, `public/`) are reachable.
- Image labels: set `org.opencontainers.image.source=https://github.com/forgeutah/deuce` and `org.opencontainers.image.revision=<sha>` via the build args the GHA workflow passes (U6). These power GHCR's automatic linkage to the repo and surface the originating commit in container metadata.

**Test scenarios:**
- Test expectation: none — Dockerfile is configuration. Validated by verification.

**Verification:**
- `docker build -t deuce:dev .` succeeds on a clean clone in under 5 minutes (cold cache).
- `docker image inspect deuce:dev` reports a size under 50 MB.
- `docker run --rm -p 8080:8080 -e DATABASE_URL=postgres://invalid deuce:dev` starts, attempts to connect to Postgres, exits non-zero with a recognizable error on the retry-loop timeout (proves the migration-on-startup gating).
- `docker run --rm -p 8080:8080 deuce:dev` with a real Postgres reachable serves the SPA at `/` and an empty 404 (or whatever the API contract is) at `/api/missing`.
- Image runs as UID 65532 (verify with `docker inspect`).

---

### U5. Production compose stack + env template

**Goal:** Ship a Docker Compose file the VM uses to run `app` + `postgres` with healthcheck-gated startup, named volume for Postgres data, and an env template documenting the prod env contract (including forge-proxy mode).

**Requirements:** R5, R11

**Dependencies:** U4 (the image being referenced must build)

**Files:**
- Create: `deploy/compose.yaml`
- Create: `deploy/.env.example`

**Approach:**

`deploy/compose.yaml` shape (directional, not literal):

```yaml
networks:
  internal:
    driver: bridge
    internal: false   # app must reach GHCR over the default network; internal=true would block egress

services:
  postgres:
    image: postgres:17.5-alpine
    restart: unless-stopped
    user: "70:70"     # alpine postgres uid:gid; PID 1 stays non-root
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    networks: [internal]
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
      interval: 5s
      timeout: 5s
      retries: 5
      start_period: 10s
    # No ports: section. Postgres is not exposed beyond the compose network.

  app:
    image: ghcr.io/forgeutah/deuce:${DEUCE_IMAGE_TAG}
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp:size=64m,mode=1777
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    networks: [internal]
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
    # No ports: section. The exe.dev proxy reaches the container via the EXPOSE
    # directive, not via published host ports.

volumes:
  pgdata:
```

**Hardening rationale (see Key Technical Decisions):** `cap_drop: [ALL]` + `no-new-privileges:true` on both services removes the surface area for kernel-level capability abuse; `read_only: true` + `tmpfs: /tmp` on the app is free because distroless-static has no writable runtime paths; `user: "70:70"` on Postgres prevents PID 1 from running as root. The dedicated `internal` bridge network keeps the two services from accidentally exposing themselves to any future sibling service added to the file; `internal: false` is required so app can pull updated images from GHCR through the default route. **Volume permission caveat:** the first `docker compose up -d postgres` (before any data lands) must run with the `user:` directive already in place so the volume mounts under UID 70 from the start. Adding `user:` after Postgres has written files as root will fail to start with permission errors and require a `chown -R 70:70` on the volume.

`deploy/.env.example` documents every var the prod stack reads — split by purpose:

```
# Compose-substituted (consumed by compose.yaml itself)
DEUCE_IMAGE_TAG=latest
POSTGRES_DB=deuce
POSTGRES_USER=deuce
POSTGRES_PASSWORD=__REPLACE_ME__

# Consumed by the app container (env_file)
PORT=8080
DATABASE_URL=postgres://deuce:__REPLACE_ME__@postgres:5432/deuce?sslmode=disable

# Auth — forge-proxy mode (this deployment)
DEUCE_AUTH_MODE=forge-proxy
FORGE_PROXY_SECRET=__REPLACE_ME__
FORGE_REQUIRED_ROLE=__REPLACE_ME__
FORGE_CONTRACT_VERSION=1

# Origin allowlists — both MUST include the Forge origin(s) that proxy to this VM
DEUCE_WS_ALLOWED_ORIGINS=https://app.forge.tld
DEUCE_CORS_ALLOWED_ORIGINS=https://app.forge.tld

# Optional integrations
GITHUB_TOKEN=
ANTHROPIC_API_KEY=
```

- `DEUCE_IMAGE_TAG` is the only value the deploy workflow modifies on each run (U6). Everything else stays untouched between deploys.
- `restart: unless-stopped` on both services covers AE4 (reboot recovery) and U2's fail-fast loop (a bad migration will restart-loop until the next deploy fixes it, but won't partially serve).
- The app container reads `.env` via `env_file:`; compose substitutes `${DEUCE_IMAGE_TAG}` and the Postgres creds in compose.yaml itself. **Compose env var precedence note for the implementer:** values in `.env` are loaded into the compose process's environment AND consumed via `env_file:` by the app service, but `${...}` substitution in `compose.yaml` reads from the process environment. Confirm the chosen split works as expected when running `docker compose --env-file .env -f deploy/compose.yaml up -d` — if there's any surprise, drop `env_file:` and rely entirely on compose-process env, or split `.env` into `.env` (compose-substituted) and `app.env` (env_file:).

**Test scenarios:**
- Test expectation: none — compose and env files are configuration. Validated by verification.

**Verification:**
- `docker compose -f deploy/compose.yaml --env-file deploy/.env.example config` exits 0 with no warnings (compose syntax + env interpolation validation).
- On a workstation with Docker and `deploy/.env.example` copied to `deploy/.env` (and a placeholder image in `DEUCE_IMAGE_TAG`), `docker compose -f deploy/compose.yaml --env-file deploy/.env up -d postgres` brings up Postgres and the named volume; the healthcheck reaches `service_healthy` within ~15 s.
- **Covers AE4.** With both services up locally, `docker compose restart` brings them back with the same volume contents (`SELECT count(*) FROM users` returns the same row count before and after).

---

### U6. GHA deploy workflow

**Goal:** Add `.github/workflows/deploy.yml` that runs lint/typecheck/test on every PR and push, and additionally builds, pushes, and deploys on push to `main` or `workflow_dispatch` with a `rollback_to` input. Add `.github/dependabot.yml` to keep SHA-pinned third-party actions current.

**Requirements:** R1, R2, R3, R4, R8, R9, R10

**Dependencies:** U4 (Dockerfile exists), U5 (`deploy/compose.yaml` referenced on the VM exists). The workflow file is mergeable before the VM exists; the first push to `main` after merge will fail at the deploy step until U7 (VM bootstrap) is completed by the operator. That ordering is operational, not a code-build dependency.

**Files:**
- Create: `.github/workflows/deploy.yml`
- Create: `.github/dependabot.yml`

**Approach:**

Workflow shape (directional):

- **Triggers:** `on: { pull_request: { branches: [main] }, push: { branches: [main] }, workflow_dispatch: { inputs: { rollback_to: { description: "image SHA tag to roll back to (omit to deploy current HEAD)", required: false, default: "" } } } }`.
- **Top-level `permissions:`** — `contents: read`, `packages: write`, `id-token: none`. Default to least-privilege.
- **Concurrency:** `group: deploy-${{ github.ref }}`, `cancel-in-progress: false`. Sequential deploys, no two pipelines racing the VM.
- **Jobs:**
  1. `test` (always runs) — checkout, setup-node v5 (npm cache), setup-go v6 (`go-version-file: server/go.mod`), `npm ci`, `npm run lint`, `npx tsc --noEmit`, `cd server && go test ./...`.
  2. `build_and_push` (depends on `test`, runs only when `github.event_name == 'push'` OR `github.event_name == 'workflow_dispatch' && inputs.rollback_to == ''` AND `github.repository == 'forgeutah/deuce'`) — checkout, setup-buildx-action v3, login to GHCR with `${{ github.actor }}` + `${{ secrets.GITHUB_TOKEN }}`, docker/metadata-action v5 to compute `tags: type=sha,prefix=,format=long` + `latest`, docker/build-push-action v7 with `context: .`, `push: true`, `cache-from: type=gha,scope=app`, `cache-to: type=gha,mode=max,scope=app`, `build-args` for `org.opencontainers.image.revision=${{ github.sha }}`.
  3. `deploy` (depends on `test`; depends on `build_and_push` only when not a rollback) — resolve the target image tag: `inputs.rollback_to` if non-empty, else `${{ github.sha }}`. Use `appleboy/ssh-action@<sha-pinned>` with `host: ${{ secrets.DEPLOY_SSH_HOST }}` (e.g., `<vmname>.exe.xyz`), `username: ${{ secrets.DEPLOY_SSH_USER }}`, `key: ${{ secrets.DEPLOY_SSH_KEY }}`, `fingerprint: ${{ secrets.DEPLOY_SSH_FINGERPRINT }}` (or hardcoded `SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo`). The `script:` runs on the VM:
     ```
     cd /opt/deuce
     sed -i "s|^DEUCE_IMAGE_TAG=.*|DEUCE_IMAGE_TAG=${TARGET_TAG}|" .env
     grep -q "^DEUCE_IMAGE_TAG=${TARGET_TAG}$" .env || { echo "image tag rewrite failed"; exit 1; }
     docker compose --env-file .env -f compose.yaml pull app
     docker compose --env-file .env -f compose.yaml up -d app
     docker compose --env-file .env -f compose.yaml ps app
     ```
     Pass `TARGET_TAG` via `envs:` input on the action. The `grep` assertion is critical: a hand-edit that added a trailing comment or whitespace to the `DEUCE_IMAGE_TAG=` line would make the `sed` silently no-op and silently redeploy the current tag with zero error — the assertion catches that case.
- **Migration ordering convention:** because deploys are serialized by the workflow's `concurrency:` group and migrations are forward-only (see Key Technical Decisions), back-to-back deploys are safe AS LONG AS no PR ships a destructive (non-additive) migration. The workflow does not enforce this — the operator's PR-review discipline does. Documented in U7 as a known operational constraint.
- **Job logs:** GHA captures stdout/stderr from every step including the SSH script — covers R9 (deploy logs visible in the GHA UI).
- **Action pinning policy:** first-party `actions/*` and `docker/*` at major tag (`@v5`, `@v3`, etc.); third-party (`appleboy/ssh-action`) SHA-pinned with the matching version in a trailing comment.
- **`.github/dependabot.yml`** configures `package-ecosystem: github-actions` weekly so the SHA pins on third-party actions don't rot. Without this, a compromised third-party maintainer release silently sits in the workflow indefinitely.

**Patterns to follow:**
- 2026 GHA conventions surfaced in research: SHA-pinning third-party, `permissions:` block scoped to job needs, `concurrency:` for serialized deploys, `docker/build-push-action@v7` + GHA cache backend v2 (`type=gha,scope=...`).

**Test scenarios:**
- Test expectation: none — the workflow itself is config. Behavior verified by the verification scenarios below across real CI runs.

**Verification:**
- **Covers AE1.** On a feature branch with a deliberately-broken Go test, opening a PR to `main` runs the `test` job and fails it; no `build_and_push` or `deploy` job runs.
- **Covers AE3.** After a successful deploy of SHA `abc1234`, a `workflow_dispatch` run with `rollback_to: abc1234` skips `build_and_push`, runs `deploy` only, and `docker compose ps` on the VM after the run reports the app container running the `abc1234`-tagged image.
- First green push to `main` ends with `<vmname>.exe.xyz` serving the SPA.
- The GHA run page shows the deploy step's SSH `script:` stdout (the compose `pull`/`up`/`ps` output), satisfying R9.
- A fork-PR build runs `test` only and does NOT push to GHCR (verify by checking the `build_and_push` job is skipped due to the repository-name guard).

---

### U7. VM bootstrap + ops documentation

**Goal:** A `docs/deploy.md` runbook that walks a teammate through every operational concern of running the dogfood instance: provisioning, hardened SSH access, env seeding, Forge wiring, deploys, rollbacks, secret rotation, incident response, common failure modes, and disk hygiene.

**Requirements:** R10, R11, indirectly enables R5/R8/AE4

**Dependencies:** U5 (the file the doc tells the user to scp), U6 (the secret names the doc tells the user to configure)

**Files:**
- Create: `docs/deploy.md`
- Modify: `README.md` (add a brief "Deployment" section linking to `docs/deploy.md`)

**Approach:**

`docs/deploy.md` covers, in order:

1. **What this is.** One paragraph: single-VM dogfood Deuce on exe.dev, deployed by GHA on push to `main`. Link to `docs/brainstorms/2026-05-23-exe-dev-dogfood-deploy-requirements.md` for the why. Up-front note: this runbook describes a manual ~30-minute bootstrap that must be completed once before the workflow can deploy successfully — the workflow file itself can be merged earlier, but the first green push happens only after these steps land.
2. **One-time VM provisioning + hardened deploy-key registration.**
   - `ssh exe.dev new --name <vmname>` (or via web UI), capture the assigned SSH user from `ssh exe.dev ls --json`
   - Generate a dedicated ed25519 deploy key on a trusted workstation: `ssh-keygen -t ed25519 -f deuce-deploy-key -N ""` (no passphrase). This key never leaves the workstation except as a GHA secret.
   - **Restrict the key on the VM.** Add the pubkey to `~/<deploy-user>/.ssh/authorized_keys` as a single line of the form `command="/usr/local/bin/deuce-deploy",restrict ssh-ed25519 AAAA... ghactions`. `restrict` is shorthand for `no-agent-forwarding,no-port-forwarding,no-pty,no-X11-forwarding,no-user-rc`. The `command=` forces every SSH invocation to run the deploy script regardless of what the GHA workflow tries to execute; the workflow's `script:` arguments arrive via `$SSH_ORIGINAL_COMMAND` and become the image tag input. Without `command=`, a leaked GHA SSH private key becomes a full shell on the VM.
   - Create `/usr/local/bin/deuce-deploy` (root-owned, 0755). It does: `set -euo pipefail; cd /opt/deuce; TAG="${SSH_ORIGINAL_COMMAND}"; [[ "$TAG" =~ ^[a-f0-9]{40}$|^latest$ ]] || { echo "bad tag"; exit 2; }; sed -i "s|^DEUCE_IMAGE_TAG=.*|DEUCE_IMAGE_TAG=${TAG}|" .env; grep -q "^DEUCE_IMAGE_TAG=${TAG}$" .env || { echo "tag rewrite failed"; exit 1; }; docker compose --env-file .env -f compose.yaml pull app; docker compose --env-file .env -f compose.yaml up -d app; docker compose --env-file .env -f compose.yaml ps app`. The tag-regex check prevents shell injection through `SSH_ORIGINAL_COMMAND`.
   - The GHA workflow's `script:` becomes a single command: the image tag, passed as the SSH command argument. The workflow doesn't need to know what runs on the VM — `authorized_keys` enforces the recipe.
   - Verify SSH from a workstation: `ssh -i deuce-deploy-key <user>@<vmname>.exe.xyz <test-sha>` and confirm only the deploy script runs (interactive shell is denied).
3. **Configure the GHA repo secrets.** Settings → Secrets and variables → Actions:
   - `DEPLOY_SSH_KEY`: full private key contents
   - `DEPLOY_SSH_HOST`: `<vmname>.exe.xyz`
   - `DEPLOY_SSH_USER`: the exe.dev-assigned deploy user
   - `DEPLOY_SSH_FINGERPRINT`: `SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo` (or hardcode in workflow)
4. **Seed the VM.** From a trusted workstation:
   - `ssh <vmname>.exe.xyz mkdir -p /opt/deuce` (one-time; this SSH call comes from a workstation key, not the restricted deploy key)
   - `scp deploy/compose.yaml deploy/.env.example <vmname>.exe.xyz:/opt/deuce/`
   - On the VM (`ssh <vmname>.exe.xyz` interactively): `cp .env.example .env`, `chmod 600 .env`, then `vim /opt/deuce/.env` to fill real secrets — `POSTGRES_PASSWORD`, `DATABASE_URL` password, `FORGE_PROXY_SECRET`, `FORGE_REQUIRED_ROLE`, `DEUCE_WS_ALLOWED_ORIGINS`, `DEUCE_CORS_ALLOWED_ORIGINS`. Edit on the VM, never `scp` a populated `.env` (shell-history risk).
   - `cd /opt/deuce && docker compose --env-file .env -f compose.yaml up -d postgres` to materialize the Postgres volume *before* any app deploy (so volume permissions are set under the `user: "70:70"` directive from the start; see U5).
5. **Forge upstream wiring.** What Forge must do for forge-proxy mode to authenticate requests:
   - Forge proxies SPA + API + WS requests to `https://<vmname>.exe.xyz/`
   - Forge signs the role/identity headers using `FORGE_PROXY_SECRET` per the contract version in `FORGE_CONTRACT_VERSION`
   - Forge's serving origin (e.g., `https://app.forge.tld`) must match `DEUCE_CORS_ALLOWED_ORIGINS` and `DEUCE_WS_ALLOWED_ORIGINS` in the VM's `.env`
   - **Critical:** Forge must terminate TLS and MUST NOT log the `X-Forge-*` identity/secret headers. Any logging of those headers becomes a credential leak.
   - **Critical (bypass mitigation, see Key Technical Decisions):** the operator MUST restrict who can reach `https://<vmname>.exe.xyz/` at the network layer — use exe.dev's `share port` access control to allowlist Forge's egress IPs only, OR keep the VM behind exe.dev IAM and grant Forge an authenticated tunnel. Without this, anyone who learns the URL + secret can impersonate any user. This is acceptable for dogfood only with explicit team awareness; revisit before any non-team user touches the instance.
6. **First deploy + smoke checks.** Push to `main`; watch the GHA run. After the deploy step succeeds, verify:
   - `curl -fsS https://<vmname>.exe.xyz/api/health` returns 2xx (proves the proxy + app + DB chain)
   - `ssh <vmname>.exe.xyz` (interactive, from a workstation): `docker compose logs app | grep -i "migrat"` shows the goose success line and no panic
   - `docker compose ps` shows both services `running (healthy)`
   - Visit the SPA through Forge and confirm an authenticated session loads
7. **Rolling back.** Use `workflow_dispatch` on the deploy workflow with `rollback_to: <previous-sha>`. The workflow short-circuits to the deploy job only.
   - **Rollback is code-only and unsafe across migration boundaries.** If the deploy being rolled back contained a destructive migration (dropped column, narrowed type), the older image will hit a schema it doesn't understand and either restart-loop or serve broken data. There is no automatic down-migration in v1; if the team needs to roll back across such a boundary, the path is: roll forward with a new migration that restores the lost column/type, not roll back. Adding a `migrate-down` subcommand to the binary is deferred follow-up.
8. **Secret rotation + incident response.** A dedicated subsection covering each secret:
   - `FORGE_PROXY_SECRET`: coordinate rotation with Forge — generate new secret, update the VM's `.env`, restart `app` (`docker compose up -d --force-recreate app`), update Forge's signing config in the same window. Document the expected ~5-min window of 4xx from Forge during the rotation.
   - `POSTGRES_PASSWORD` and the embedded password in `DATABASE_URL`: rotate together — `docker compose exec postgres psql -U postgres -c "ALTER USER deuce WITH PASSWORD '...';"`, update `.env`, `docker compose up -d --force-recreate app`. Restart Postgres too if it caches the old credential.
   - Deploy SSH key (`DEPLOY_SSH_KEY`): generate a new key on a workstation, append to `authorized_keys`, update the GHA secret, verify a deploy succeeds, then remove the old key from `authorized_keys`.
   - `GITHUB_TOKEN` consumed by the app for repo listing: revoke in GitHub Settings → Developer settings, generate replacement, update VM `.env`, `docker compose up -d --force-recreate app`.
   - **Suspected leak:** if any of the above MAY have leaked (visible in a shared screenshot, accidentally pasted to chat, present in a debug log), treat as confirmed — rotate immediately and audit access logs (`docker compose logs app --since 30m`).
9. **Diagnosing failures.** Common scenarios with concrete next steps:
   - App container restart-looping → `docker compose logs --tail 200 app` — typically migration error, CORS/WS origin mismatch, missing forge-proxy env var.
   - Forge returning 502 → SSH in, check `docker compose ps`; check `curl -i http://localhost:8080/api/health` on the VM (direct, bypassing the exe.dev proxy).
   - GHA deploy job fails on SSH connect → host fingerprint changed (verify against `ssh-keyscan -t ed25519 <vmname>.exe.xyz | ssh-keygen -lf -`) or deploy key was rotated/revoked on exe.dev.
   - **`:latest` is poisoned after a bad deploy.** A failed deploy still pushes the bad image to GHCR and tags it `:latest`. Never `docker compose pull` manually until a green deploy fixes it — that pulls the bad image again. Recovery: `workflow_dispatch` rollback to the last-good SHA, then the next green push to `main` repairs `:latest`.
10. **Disk hygiene.** exe.dev gives 100 GB pooled across all VMs in the subscription. Each deploy leaves the previous image (useful for rollback) plus dangling buildx-cached layers. Monthly checklist:
    - `df -h /var/lib/docker` — flag if `>70%` used
    - `docker image prune -af --filter "until=720h"` (drops images older than 30 days, keeps recent rollback targets)
    - `docker volume ls` to confirm only `pgdata` exists
    - `du -sh /opt/deuce` to track non-Docker growth
11. **Known constraints.** A short subsection listing operational gotchas that aren't bugs:
    - Migrations are forward-only; rollback across destructive migrations is unsafe (see §7)
    - Back-to-back deploys are serialized by `concurrency:` and safe as long as no PR ships a destructive migration
    - exe.dev intra-platform hop between edge and VM is platform-trusted (not end-to-end encrypted to the container) — see Risks

`README.md` adds a short "Deployment" subsection pointing here.

**Test scenarios:**
- Test expectation: none — documentation. Validated by a teammate walkthrough.

**Verification:**
- A second engineer can follow `docs/deploy.md` end-to-end on a fresh exe.dev VM and reach a serving instance without consulting the original author.
- The rollback section is followed for a real rollback (drill, not a real incident) and works in under 5 minutes.

---

## System-Wide Impact

- **Interaction graph:** New static handler on chi is mounted at the catch-all position after `/api` and `/ws*`. Order matters — any future route added at `/somethingnew` will work because chi is most-specific-first, but a future route registered with a wildcard like `/foo/*` would conflict with the static handler and must be registered before it. Document this in the static handler's package doc comment.
- **Error propagation:** Migration failures `log.Fatalf` from the binary, which causes the container to exit non-zero, which causes Compose to restart it per `restart: unless-stopped`. The app never partially-serves a broken-schema state. CORS/WS origin misconfigurations surface as 4xx in the app logs; the SPA fails to make API calls.
- **State lifecycle risks:** The Postgres named volume `pgdata` is the only durable state. `docker compose down` does not remove named volumes by default (only `docker compose down -v` does), so accidental data loss requires explicit action. Image-tag rollback does not touch the volume — schema changes from a newer migration that was rolled back can still leave the DB with extra columns/tables, which a re-deploy of the newer image will pick back up.
- **API surface parity:** No API surface change. The Go server's routes remain identical; only the catch-all GET handler is added.
- **Integration coverage:** The static-handler-vs-API ordering test (in U1's test scenarios) is the integration assertion that the SPA fallback doesn't shadow API routes. The migration-blocks-startup test (in U2) is the integration assertion that bad migrations don't half-serve.
- **Unchanged invariants:** Local development workflow (Vite on `:4000`, Go server on `:8080`, Postgres via root `docker-compose.yml`) is unchanged. `make dev`, `make migrate`, `make test` still work. The dev `CORS` defaults remain localhost-friendly.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| **(Security, P0) Forge-proxy bypass via direct exe.dev hostname.** The middleware authenticates by validating a shared-secret header. Anyone who hits `https://<vmname>.exe.xyz/` directly with `X-Forge-Proxy-Secret: <secret>` + crafted identity headers is fully authenticated as any user — including impersonation of arbitrary user IDs and roles. The secret only needs to leak once for full impersonation. | Operator-level for v1 (see Key Technical Decisions): the public port MUST be restricted to Forge's egress via exe.dev's `share port` access control or kept behind exe.dev IAM. Acceptable for dogfood **only** with explicit team awareness. Adding an IP-allowlist field to the forge-proxy middleware is the architecturally correct fix and is **required before any non-team user touches the instance** — tracked as deferred follow-up on the forge-proxy track. |
| **(Security, P0) No replay protection on signed forge-proxy headers.** The contract is shared-secret + identity headers — no timestamp, nonce, or HMAC over the request body/path. Any captured request can be replayed indefinitely against any endpoint, including mutating ones. | Accepted for dogfood, contingent on the bypass mitigation above (so the attack surface is limited to Forge's TLS-terminating infrastructure). Forge MUST terminate TLS, never log `X-Forge-*` headers, and use a vetted reverse-proxy. Revisit before non-team users — adding HMAC-over-request-line to the contract is the right v2 move. |
| **(Security, P0) exe.dev intra-platform hop is platform-trusted.** Forge → exe.dev edge is HTTPS, but the secret + identity headers traverse plaintext on whatever shared infrastructure exe.dev runs between its edge and the VM (the proxy terminates TLS, then routes plaintext to the container's `EXPOSE 8080`). | Accepted by virtue of using exe.dev. Documented so the team isn't surprised. If end-to-end encryption to the container ever matters, the move is mTLS between Forge and a small TLS-terminating sidecar on the VM. |
| **(Security, P1) No documented secret-rotation procedure if a secret leaks.** Without a playbook, rotation under stress is error-prone and slow. | U7 §8 documents an explicit per-secret rotation procedure (`FORGE_PROXY_SECRET`, `POSTGRES_PASSWORD`, deploy SSH key, `GITHUB_TOKEN`) including the expected downtime window and how to handle suspected-leak cases. |
| **(Security, P1) Third-party action SHA pins go stale without automation.** A compromised maintainer release or a missed CVE patch sits in the workflow indefinitely. | `.github/dependabot.yml` (U6) configures weekly checks against `package-ecosystem: github-actions` so pin updates surface as PRs. |
| **(Reliability, P0) Cross-plan dependency on `feat/forge-proxy-auth`.** If this deploy plan's first push lands against a `main` that lacks forge-proxy code, `config.Validate()` rejects the prod env contract and the container restart-loops on boot. | Merge order is enforced manually: the forge-proxy plan (`docs/plans/2026-05-22-001-feat-forge-proxy-auth-mode-plan.md`) MUST merge to `main` before this deploy plan does. Captured as a Resolved-During-Planning note and called out here for visibility. |
| **(Reliability) Rollback across destructive migration boundaries leaves an older binary against a newer schema.** The plan's forward-only convention means rollback is code-only; rolling back across a `DROP COLUMN` is unsafe. | Documented in Key Technical Decisions, U2, U6, and U7 §7. Operator's recovery path is roll-forward with a restoring migration, not roll-back. Adding `migrate-down` is deferred follow-up. |
| **(Operations) `:latest` is contaminated for the window between a bad deploy and the next green push.** A manual `docker compose pull` during that window pulls the bad image again. | U7 §9 explicitly warns operators never to `docker compose pull` manually after a bad deploy; recovery is `workflow_dispatch` rollback to a known-good SHA, after which the next green push restores `:latest`. |
| **(Operations) VM disk fills up.** Each deploy leaves the previous image (good for rollback) plus buildx-cached layers, and `pgdata` grows unbounded. 100 GB pooled is generous but not infinite. | U7 §10 documents a monthly hygiene checklist: `df -h /var/lib/docker`, `docker image prune -af --filter "until=720h"`, volume inventory. Consider promoting to a weekly cron once dogfood traffic is real. |
| Forge's expected origin string for `DEUCE_CORS_ALLOWED_ORIGINS` is unknown at plan time — the wrong value silently breaks every browser request. | Documented in U7 §5 as a fill-in by the operator; placeholder in `deploy/.env.example`; verification step in U7 §6 includes a `curl -i -H "Origin: ..."` smoke test before declaring the deploy good. |
| exe.dev is a young platform with thin documentation; an outage or API change could break the deploy path with no clear remediation. | The deploy path is plain SSH + Docker — no exe.dev-specific tooling. If exe.dev goes down, the image is in GHCR and the compose file is portable; the VM can be re-stood-up on any Docker-capable host with `scp deploy/compose.yaml` + `docker compose up -d`. |
| GHCR has a 10 GB cache cap; runaway buildx cache could evict useful layers. | `cache-to: type=gha,mode=max,scope=app` with a single scope keeps cache bounded; if it becomes a problem, switch to `mode=min` or add a scheduled cache cleanup. |
| The first-time `.env` editing over SSH risks a passphrase or secret ending up in shell history or a SSH multiplexer log. | U7 §4 instructs the operator to edit `.env` directly on the VM (no scp of a populated `.env`) and to keep the file at `chmod 600 /opt/deuce/.env`. |
| Compose-substituted vars (`${DEUCE_IMAGE_TAG}`) vs `env_file:`-loaded vars have subtle precedence rules that could silently misload secrets. | U5's verification step runs `docker compose ... config` against the populated `.env` to materialize the effective config before the first up; the implementer's note in U5 lays out the fallback (split into `.env` + `app.env`) if precedence surprises appear. |
| The fingerprint-pinning of `SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo` could go stale if exe.dev rotates host keys. | Documented as the published value at plan time; if SSH connections start failing, the U7 §9 troubleshooting section directs the operator to verify with `ssh-keyscan`. |
| Migration-on-startup race with multiple app containers (none today, but easy to add later). | Goose acquires an advisory lock in Postgres before running migrations — safe under concurrent app boots. Documented in U2's Approach as the reason in-process migration is safe even when we eventually scale. |

---

## Documentation / Operational Notes

- After this lands, run `/ce-compound` to document two patterns under `docs/solutions/`: (1) the multi-stage `Go binary + embedded Vite SPA` Dockerfile shape, (2) the `goose-on-startup` decision and its operational consequences (fail-fast, restart-loop semantics).
- `README.md` gets a short Deployment section that links to `docs/deploy.md`. The existing Development section stays unchanged.
- The first-deploy procedure is one-shot manual work; subsequent deploys are fully automated. Document the manual step's expected duration (~15 min) in `docs/deploy.md` so a teammate sets expectations correctly.
- Future plan: extend `docs/deploy.md` with a "Custom domain" section when the team is ready to move off `<vmname>.exe.xyz` per the brainstorm's deferred-for-later list.
- Monitoring/alerting is intentionally not in this plan. For internal dogfood, `docker compose logs --tail 200 app` over SSH is sufficient. Revisit when there's a real user the team would want to be paged about.

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-05-23-exe-dev-dogfood-deploy-requirements.md](../brainstorms/2026-05-23-exe-dev-dogfood-deploy-requirements.md)
- **Related recent work:** the forge-proxy auth track — commits `89b9472`, `f46ee3a`, `f62a668` on the active branch `feat/forge-proxy-auth`, and the prior plan `docs/plans/2026-05-22-001-feat-forge-proxy-auth-mode-plan.md`. The deploy plan assumes that work is merged before deploy goes live.
- **External:**
  - [exe.dev HTTP proxies](https://exe.dev/docs/proxy.md), [exe.dev SSH/API](https://exe.dev/docs/api.md), [exe.dev Docker FAQ](https://exe.dev/docs/faq/docker.md), [exe.dev host key](https://exe.dev/docs/faq/host-key.md), [exe.dev custom domains](https://exe.dev/docs/cnames.md)
  - [GHA cache backend (Docker)](https://docs.docker.com/build/cache/backends/gha/), [GHA SHA-pinning policy (Aug 2025)](https://github.blog/changelog/2025-08-15-github-actions-policy-now-supports-blocking-and-sha-pinning-actions/)
  - [pressly/goose v3 godoc](https://pkg.go.dev/github.com/pressly/goose/v3)
  - [Compose depends_on healthcheck](https://docs.docker.com/compose/how-tos/startup-order/)
  - [GoogleContainerTools/distroless](https://github.com/googlecontainertools/distroless)
  - [appleboy/ssh-action](https://github.com/appleboy/ssh-action)
