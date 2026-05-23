---
date: 2026-05-23
topic: exe-dev-dogfood-deploy
---

# exe.dev Dogfood Deployment via GitHub Actions

## Summary

Stand up a single internal-dogfood instance of Deuce on an exe.dev VM, deployed by a GitHub Actions workflow on push to `main`. The deploy unit is one Docker image containing the Go server with the React build embedded; Postgres runs alongside on the same VM via Docker Compose.

---

## Problem Frame

Deuce has no deployed environment today. The team can run the stack locally but cannot use Deuce on Deuce — the central premise of the product (shared workspace for human + agent collaboration) is invisible until it's running somewhere multiple people can hit at the same time. Every day without a shared instance is a day the team is reasoning about the product instead of using it.

exe.dev is a good fit for this specific moment: persistent Linux VMs with automatic HTTPS/DNS/IAM, $20/mo for far more capacity than a dogfood instance needs, and no infra for the team to babysit. The constraint is that exe.dev exposes only an SSH and HTTP-API surface — there's no "git push to deploy" primitive — so the project needs an actual CI pipeline rather than a click-deploy integration.

---

## Key Flows

- F1. Push-to-main deploy
  - **Trigger:** Commit lands on `main` (direct push or PR merge).
  - **Actors:** GitHub Actions; exe.dev VM.
  - **Steps:**
    1. GHA checks out the repo and runs lint + typecheck + Go tests.
    2. GHA builds a multi-stage Docker image (Node build → Go build with `dist/` embedded → distroless runtime), tagged with the commit SHA and `latest`.
    3. GHA pushes the image to GHCR using the workflow's auto-issued `GITHUB_TOKEN`.
    4. GHA SSHes to the exe.dev VM using a dedicated deploy key, updates the image tag in the VM-side `.env`, runs `docker compose pull app && docker compose up -d app`.
    5. The app container's entrypoint runs `goose up` against the on-box Postgres before starting the HTTP server.
  - **Outcome:** New version is live at the exe.dev-assigned URL. Old image remains on disk for rollback.
  - **Covered by:** R1, R2, R3, R4, R5, R7, R9

- F2. Manual rollback
  - **Trigger:** A deploy broke dogfood; team needs to revert.
  - **Actors:** Engineer.
  - **Steps:**
    1. Engineer triggers a `workflow_dispatch` of the deploy workflow with an input field for image tag (defaults to previous SHA).
    2. Same SSH + `docker compose pull && up -d` path as F1 runs against the chosen tag.
  - **Outcome:** Previous version is live. No manual SSH required for the common case.
  - **Covered by:** R8

---

## Requirements

**Deploy pipeline**
- R1. A GitHub Actions workflow runs on push to `main` and builds, pushes, and deploys without human intervention.
- R2. The pipeline runs lint, typecheck, and Go tests before building the image; failures block the deploy.
- R3. The Docker image is built once per commit and tagged with both the commit SHA and `latest`; the SHA tag is what the VM pulls.
- R4. The image is pushed to GitHub Container Registry (GHCR) using the workflow's built-in `GITHUB_TOKEN`; no third-party registry credentials.

**Runtime on the VM**
- R5. The VM runs `app` and `postgres` as a two-service Docker Compose stack; Postgres data lives on a named Docker volume on the persistent VM disk.
- R6. The Go server serves the embedded React build for all non-API routes (catch-all → `index.html` to keep TanStack Router history mode working) and `/api/*` + `/ws` as today.
- R7. Database migrations run on app-container start via `goose up` against the on-box Postgres; the app does not start serving if migrations fail.

**Operability**
- R8. Rollback to any previously-pushed image SHA is possible via a `workflow_dispatch` run of the same workflow with an image-tag input. No manual SSH required for rollback.
- R9. Deploy logs (image build, push, SSH session output) are visible in the GHA run; the engineer can diagnose a failed deploy from the GHA UI alone in the common case.

**Secrets and access**
- R10. Deploy authenticates to the VM via a dedicated SSH key whose private half lives only in GitHub Actions secrets; the user provisions and rotates this key out-of-band.
- R11. Production secrets consumed by the app (e.g., `DATABASE_URL`, `DEUCE_USER_ID`, `GITHUB_TOKEN` for repo listing) live in the VM-side `.env` file, not in the GHA workflow or the image.

---

## Acceptance Examples

- AE1. **Covers R2.** Given a commit on `main` whose Go tests fail, when the GHA workflow runs, then the image is not built or pushed and the VM continues serving the prior version.
- AE2. **Covers R7.** Given a deploy whose migration would fail (e.g., a malformed migration file), when the new app container starts, then it exits non-zero, Docker Compose marks it unhealthy, and `docker compose up -d` either keeps the old container running (if still present) or leaves the service down — but never partially-migrated-and-serving.
- AE3. **Covers R8.** Given the engineer triggers `workflow_dispatch` with image tag `abc1234` (a previously-pushed SHA), when the workflow runs, then the VM is left running the `abc1234` image with no manual SSH performed.
- AE4. **Covers R5, R11.** Given the VM reboots, when Docker starts back up, then `postgres` comes up against its existing volume, `app` pulls its env from the on-VM `.env`, and the stack is serving again without any GHA run.

---

## Success Criteria

- The team can use Deuce on a shared URL within a week of this plan landing, and any commit to `main` is reflected on that URL within ~10 minutes without anyone SSHing anywhere.
- The next engineer to touch the deploy can roll back a bad release from the GHA UI in under two minutes without reading internal docs.
- `ce-plan` and an implementer can pick up this brainstorm and choose the multi-stage Dockerfile layout, GHA action versions, and SSH-action details without coming back to ask product questions.

---

## Scope Boundaries

- No custom domain or TLS configuration beyond what exe.dev's reverse proxy assigns automatically — the assigned URL is good enough for dogfood; custom domain is a separate decision.
- No backups of any kind for now (no `pg_dump`, no off-box snapshots). Data loss is an accepted risk for the dogfood instance; revisit when there's anything in the database the team would mourn.
- No per-PR preview VMs. Single long-lived VM only. The exe.dev HTTP API path stays unexplored until the team actually wants preview environments.
- No exe.dev BYO-VM-image path. The image is pulled into a stock VM by Docker Compose, not used to boot the VM itself; revisit if exe.dev's docs clarify disk-preservation semantics for that flow.
- No zero-downtime / blue-green deploy. A few seconds of 502 during `docker compose up -d app` is acceptable for dogfood.
- No staging environment. `main` deploys straight to the dogfood VM; there is no `staging.deuce.*` tier.
- No Terraform / IaC for the VM. The VM is provisioned once by hand against exe.dev; only the app and its compose file are managed by CI.
- No GHA-side infrastructure beyond what GitHub provides for free (no self-hosted runners, no third-party registries).

---

## Key Decisions

- **Approach A (Docker image → GHCR → SSH-pull) over native binary + systemd**: matches GitHub Actions ecosystem conventions, gives atomic deploys keyed on an immutable image tag, makes rollback symmetric with deploy, and keeps the VM's filesystem state minimal so dogfood doesn't accumulate snowflake config. The cost — Postgres-in-container being slightly clunkier for `pg_dump` — is moot under the no-backups decision.
- **GHCR over Docker Hub or third-party registries**: zero extra credentials (uses the workflow's auto-issued `GITHUB_TOKEN`), free for private images on GitHub-hosted projects, no separate account to manage.
- **Migrations on app start, not as a separate GHA step**: keeps migration coupled to the exact image being deployed; impossible to deploy a binary that expects a schema the DB doesn't have yet. Tradeoff: app boot is gated on DB readiness, but the compose `depends_on` + a small retry loop in the entrypoint handles that cleanly.
- **One VM, on-box Postgres, no separate environments**: matches the "internal dogfood only" intent. Splitting prod/staging or moving Postgres off-box are reversible later decisions; doing them now is premature complexity for a dogfood instance.
- **No backups, accept data loss**: explicit user decision. The dogfood instance has no irreplaceable data yet; spending the operational budget on backups now would be optimizing for a future state.
- **Dedicated SSH deploy key, user-provisioned**: removes the need for a third-party deploy service or storing personal SSH keys in CI. User confirmed they can issue and rotate this key.

---

## Dependencies / Assumptions

- exe.dev's reverse proxy routes HTTPS traffic to a single configurable port on the VM. Unverified against their docs (JS-rendered, not readable by our doc-fetch); planning should confirm and either configure the app to bind that port or document how to set it.
- exe.dev's HTTP API and bearer-token model are not used in v1; nothing in this brainstorm depends on them. They're available for the future preview-VMs flow.
- The Go server can serve the embedded `dist/` via `embed.FS` with a catch-all to `index.html` such that TanStack Router history-mode routes work. Standard pattern; flagged as an assumption only because the code change isn't trivial — current `server/internal/server/server.go` doesn't serve static files (verified: server only registers API + WS routes).
- The VM has Docker and Docker Compose available out of the box, or installable via `apt` in a one-time bootstrap. exe.dev advertises stock Ubuntu, so this is expected.
- `GITHUB_TOKEN` (the env var the app uses for repo listing, per `CLAUDE.md`) and any future agent API keys are stored in the VM-side `.env`, not committed and not in GHA. Planning should produce the canonical `.env.example` to match.
- The `dist/` directory currently checked into the repo (per the `ls` at brainstorm time) is a stale local build artifact; the deploy pipeline always rebuilds it inside the Docker image and does not consume the checked-in copy. Planning should add `dist/` to `.gitignore` if not already.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R6][Technical] Exact static-file serving wiring in `server/internal/server/server.go` — catch-all route placement relative to API routes, MIME-type handling, immutable-asset caching headers.
- [Affects R5, R7][Technical] Compose file shape: image-pinning strategy for the Postgres image (major version pin), volume naming, healthcheck for app→postgres ordering, restart policy.
- [Affects R1, R2][Technical] GHA workflow file structure — single job vs. build/deploy split, which `actions/setup-*` versions, which SSH action (`appleboy/ssh-action` is the obvious pick but worth a quick check for current best practice).
- [Affects R7][Technical] Entrypoint script for the app container — how to wait for Postgres readiness before `goose up`, how to fail fast vs. retry, log format.
- [Affects R10][Needs research] Where the VM-side `.env` originates on first boot (manual scp once? GHA writes it on first deploy from secrets? a small bootstrap script the user runs by hand?). Product-level call: "manual scp once" is fine for dogfood; planning should pick.
- [Affects all][Needs research] exe.dev specifics: which port the proxy expects to hit, how the assigned URL is discovered (env var on the VM? SSH command output?), whether there's an idempotent VM-bootstrap command. Confirm against exe.dev docs at plan time.
