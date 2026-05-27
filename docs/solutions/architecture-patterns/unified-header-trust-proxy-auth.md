---
title: "Unified Header-Trust Proxy Auth Middleware"
module: "server/internal/auth"
date: 2026-05-26
problem_type: architecture_pattern
component: authentication
severity: high
applies_when:
  - "Multiple upstream auth proxies (forge-proxy, Tailscale Serve, exe.dev) need to share one backend"
  - "Identity is conveyed via HTTP headers from a trusted reverse proxy"
  - "Optional security checks (shared secret, contract version, role) vary per deployment"
  - "Some proxies omit display name and require frontend onboarding fallback"
  - "Email is used as the canonical user identifier across providers"
related_components:
  - "server/internal/config/config.go"
  - "server/internal/handler/users.go"
  - "src/components/auth/WelcomeView.tsx"
  - "src/app/App.tsx"
tags:
  - authentication
  - reverse-proxy
  - middleware
  - header-trust
  - config-validation
  - multi-provider
  - race-conditions
  - onboarding
category: architecture-patterns
---

# Unified Header-Trust Proxy Auth Middleware

## Context

Deuce's first auth mode behind a reverse proxy was `forge-proxy` — a dedicated middleware that required `X-Forge-Proxy-Secret`, pinned `X-Forge-Contract-Version: 1`, and gated on a CSV role header. Then we wanted to support Tailscale Serve (no shared secret; roles as a JSON-object capability grant) and exe.dev (email + user-id only; no name, no role, no secret).

The naive route is a mode-per-provider switch — one middleware per upstream. That balloons code paths, makes the auth surface dependent on the *list of vendors we happen to support*, and forces every new proxy through a Go change.

Instead, the middleware was rewritten to be **one configurable header-trust gate** whose optional security checks (shared secret, contract version, required role) light up *only when their backing env vars are present*. Adding a new proxy is now an `.env` edit, not a code change.

## Guidance

The pattern has six moving pieces. They reinforce each other; skipping one of the validations re-opens a footgun the others assume is closed.

**1. Gate optional checks by config presence, not a mode flag.**

```go
secretCheckEnabled := pc.SecretHeader != ""
contractCheckEnabled := pc.ContractVersionHeader != ""
roleCheckEnabled := pc.RolesHeader != ""

// later, per-request:
if secretCheckEnabled {
    got, ok := singleHeader(r, pc.SecretHeader)
    if !ok { /* 401 */ }
    if len(gotBytes) != len(secretBytes) ||
       subtle.ConstantTimeCompare(gotBytes, secretBytes) != 1 { /* 401 */ }
}
```

The booleans are computed once at startup. Per-request branching is just `if enabled { check }`. No mode-per-provider switch.

**2. Validate env-var pairs symmetrically at startup — refuse to boot on asymmetry.**

```go
if (c.ProxyHeaderSecret == "") != (c.ProxySecret == "") {
    problems = append(problems, "DEUCE_PROXY_HEADER_SECRET and DEUCE_PROXY_SECRET must both be set or both empty")
}
```

A header name without its value (or a secret value with no header to read it from) is almost always a typo, and silently disables the check. Treating it as a startup error matches the fail-closed posture proxy mode is supposed to enforce. The same shape repeats for contract-version-header / contract-version, and roles-header / required-role / roles-format.

**3. Reject header-slot collisions and malformed header names.**

```go
var headerTokenRE = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)
// ...
if !headerTokenRE.MatchString(slot.value) {
    problems = append(problems, fmt.Sprintf("%s=%q is not a valid HTTP header name", slot.envVar, slot.value))
}
if other, ok := seen[slot.value]; ok {
    problems = append(problems, fmt.Sprintf("%s and %s both map to header %q", other, slot.envVar, slot.value))
}
```

`X-Forge Email` (space) silently never matches at runtime; the regex turns it into a clear startup failure. Aggregating every misconfiguration into a single error (rather than returning the first) lets an operator fix everything at once.

**4. Email as canonical identifier, normalized before lookup.**

```go
email := strings.ToLower(strings.TrimSpace(rawEmail))
```

`Alice@Example.COM` and `alice@example.com` collapse to one account. The trade-off: if a user's email changes upstream, they get re-provisioned as a new account. Acceptable in practice because email changes are rare; the existing `email UNIQUE NOT NULL` constraint on `users` is the trust anchor.

**5. Race-tolerant provisioning via `ON CONFLICT (email) DO NOTHING`.**

```go
user, err := store.LookupUserByEmail(ctx, email)
if errors.Is(err, pgx.ErrNoRows) {
    user, err = store.CreateUserByEmail(ctx, params)
    if errors.Is(err, pgx.ErrNoRows) {
        // ON CONFLICT swallowed the insert — re-lookup picks up the winner.
        user, err = store.LookupUserByEmail(ctx, email)
    }
}
```

The race-loser path returns `pgx.ErrNoRows` from the INSERT — **not** a `23505` PgError — because `RETURNING` produces zero rows when the conflict clause fires. A second `LookupUserByEmail` picks up the winner's row. (Subtle: this means a 409 EMAIL_CONFLICT branch that detects 23505 is unreachable under `ON CONFLICT DO NOTHING`. Either drop the 409 path or switch to a plain `INSERT` with explicit 23505 handling if you actually need to distinguish "race loser" from "inconsistent state".)

**6. Welcome screen for proxies that don't supply a name.**

Email is the only required identity header — name is optional. When the name header is unset, users are provisioned with `name = ""`. The frontend gates on it:

```tsx
if (currentUser && !currentUser.name) {
  return <WelcomeView email={currentUser.email} onComplete={...} />;
}
```

`WelcomeView` posts to `PATCH /api/me`, which trims, validates `len <= 100`, and updates the row. The app shell never renders against an empty name. Subsequent sign-ins skip the welcome screen.

**Header-smuggling defense:** every configured header is read via `r.Header.Values(name)` with a `len != 1` rejection. A request shipping two copies of the secret (one to pass the check, one carrying a smuggled value) is bounced. This applies to every header slot, not just secret.

## Why This Matters

- **Adding a new proxy is config, not code.** Three vendors are supported by exactly one middleware. The fourth will be too.
- **Misconfiguration fails loud and early.** Header typos, asymmetric env pairs, colliding header slots, malformed header names — all caught before the server binds. The class of bug where "the secret check looked enabled but was silently off" cannot occur.
- **No new-vendor edits to the security-critical path.** The constant-time compare, duplicate-header rejection, race-tolerant provisioning, and log sanitization are written once. New proxies inherit them automatically.
- **The trust boundary stays explicit.** Each example documents *its* boundary (shared secret + secret rotation, tailnet ACL, exe.dev's edge) so operators know what is actually enforcing identity when the application-layer check is off.
- **The application-layer check can legally be off.** When all three optional checks are unconfigured (Tailscale-style or exe.dev-style), the server emits a startup `WARN` naming each disabled check so the operator sees the regression at boot. The trust boundary becomes the network ingress; the WARN is the signal that's now the only thing holding.

## When to Apply

Reach for this pattern when:

- You're behind (or expect to be behind) **more than one** identity-injecting reverse proxy.
- The proxies disagree on which checks exist (some have shared secrets, some don't; some emit roles as CSV, some as JSON).
- The middleware would otherwise grow a mode-per-vendor switch.

Skip it when there is exactly one upstream and no plausible second — the per-provider middleware is fine, and the extra config surface is not free. You can always promote to this pattern later when the second provider lands.

## Examples

The three env blocks below all run through the same `ProxyMiddleware`. The middleware code is unchanged across them — only the env vars differ.

**forge-proxy** — all three optional checks on:

```env
DEUCE_AUTH_MODE=proxy
DEUCE_PROXY_HEADER_EMAIL=X-Forge-Email
DEUCE_PROXY_HEADER_NAME=X-Forge-Name
DEUCE_PROXY_HEADER_AVATAR=X-Forge-Avatar
DEUCE_PROXY_HEADER_SECRET=X-Forge-Proxy-Secret
DEUCE_PROXY_SECRET=<32 random bytes from `openssl rand -hex 32`>
DEUCE_PROXY_HEADER_CONTRACT_VERSION=X-Forge-Contract-Version
DEUCE_PROXY_CONTRACT_VERSION=1
DEUCE_PROXY_HEADER_ROLES=X-Forge-Roles
DEUCE_PROXY_ROLES_FORMAT=csv
DEUCE_PROXY_REQUIRED_ROLE=member
```

**Tailscale Serve** — no secret (the tailnet plus a loopback bind is the boundary), roles via JSON-object grant:

```env
DEUCE_AUTH_MODE=proxy
DEUCE_PROXY_HEADER_EMAIL=Tailscale-User-Login
DEUCE_PROXY_HEADER_NAME=Tailscale-User-Name
DEUCE_PROXY_HEADER_AVATAR=Tailscale-User-Profile-Pic
DEUCE_PROXY_HEADER_ROLES=Tailscale-App-Capabilities
DEUCE_PROXY_ROLES_FORMAT=json-object
DEUCE_PROXY_REQUIRED_ROLE=example.com/cap/deuce/access
```

**exe.dev** — email only; no name header → welcome screen handles display name on first sign-in:

```env
DEUCE_AUTH_MODE=proxy
DEUCE_PROXY_HEADER_EMAIL=X-ExeDev-Email
```

Three deployments, three trust boundaries, one middleware, zero new Go code per vendor.

## Related

- `server/internal/auth/proxy.go` — the middleware implementation
- `server/internal/config/config.go` — `Validate()` with symmetric-pair detection, header-slot collision detection, RFC 7230 header-name validation
- `server/internal/handler/users.go` — `UpdateMe` handler backing the welcome screen's `PATCH /api/me`
- `src/components/auth/WelcomeView.tsx` and `src/app/App.tsx` — frontend gate for empty-name users
- `CLAUDE.md` "Hosted deployment checklist (proxy mode)" section — operational guardrails kept in sync with the env-var contract
- `.env.example` — three worked env blocks (forge-proxy, Tailscale Serve, exe.dev) corresponding to the configurations above
- Planning context: `docs/plans/2026-05-26-002-feat-unified-proxy-auth-plan.md`
