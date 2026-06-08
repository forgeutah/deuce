---
module: session-authorization
date: 2026-06-08
problem_type: architecture_pattern
component: authentication
severity: critical
category: architecture-patterns
applies_when:
  - "Broadening a resource's visibility, listing, or ID enumerability (e.g. membership-scoped to team/org-scoped)"
  - "A resource-scoped route's safety has depended on IDs being hard to discover rather than an explicit check"
  - "Adding a self-serve join/discovery endpoint that widens who can enumerate resource IDs"
  - "Designing read vs write/live access boundaries for a nested, multi-tenant resource"
related_components:
  - server/internal/handler
  - server/internal/ws
  - server/internal/workspace
  - server/internal/auth
tags:
  - authorization
  - multi-tenant
  - access-control
  - security-by-obscurity
  - idor
  - existence-oracle
  - websocket-terminal
---

# Broadening a visibility boundary is a security change: audit every resource-scoped route with concentric gates

## Context

In Deuce, session visibility moved from **membership-scoped** to **team-scoped**: `ListSessionsForUser` (`server/internal/db/queries/sessions.sql`) now returns every session whose project belongs to a team the user is on, via `sessions -> projects -> team_members`. A self-serve `JoinSession` / `JoinTeam` endpoint was added so users can join any team. Combined, these two changes made every session ID enumerable by anyone — the list query no longer hides sessions, and team membership is a click away.

Before this change, several resource-scoped routes had **zero per-session authorization**. They were safe only because session IDs were not discoverable (the list query was the de facto access filter). Once IDs became enumerable, these became directly exploitable: the terminal WebSocket (`/ws/terminal/{sessionID}` — a live shell into a container), workspace `start/stop/rebuild/delete`, file read, `UpdateSession`, `UpdateSessionAgents`, `StopAgent`, and `vscode-uri`. None of these were flagged by feature tests — the tests use legitimately-scoped IDs. They were caught only by an adversarial review pass that enumerated every route in `server/internal/server/server.go` and asked "what gate protects this?"

This learning distills the implementation plan `docs/plans/2026-06-08-001-feat-team-scoped-sessions-and-join-to-participate-plan.md` (KTD1 "Two gates, not one"; risk R-A "audit `server.go` routes for any session-scoped route not covered").

## Guidance

The resolution is **two concentric gates** (`server/internal/handler/session_members.go`):

```go
// READ boundary — team membership. Visible to anyone on the session's team.
func (h *Handler) requireSessionTeamMember(w, r, sessionID, userID) bool {
    member, err := h.queries.IsSessionTeamMember(r.Context(), ...) // session->project->team->team_members
    if err != nil { writeError(w, 500, "DB_ERROR", ...); return false }
    if !member  { writeError(w, 403, "FORBIDDEN", "not a team member"); return false }
    return true
}

// WRITE + LIVE boundary — session membership. Gates mutations and live streams.
func (h *Handler) requireSessionMember(w, r, sessionID, userID) bool {
    member, err := h.queries.IsSessionMember(r.Context(), ...)
    if err != nil { writeError(w, 500, "DB_ERROR", ...); return false }
    if !member  { writeError(w, 403, "FORBIDDEN", "not a session member"); return false }
    return true
}
```

When you broaden any list or visibility scope, treat it as a security change and run this checklist:

1. **Enumerate every `<resource>`-scoped route** — open the router and list every route that takes the resource ID as a path param, **including routes outside the resource's subtree**. In Deuce the terminal WS lives at `/ws/terminal/{sessionID}`, completely outside the `/api/sessions/{sessionID}` subtree, so a subtree middleware would have missed it — and it was one of the routes with zero authz.
2. **Assert an explicit gate on each one** and classify it: read paths get the outer (broader) gate, write/live paths get the inner gate. "It was safe because the ID wasn't discoverable" is no longer a defense.
3. **Run the read gate BEFORE the existence lookup** so an out-of-scope or nonexistent resource returns `403`, not a `404` that leaks existence (an enumeration oracle):

   ```go
   if !h.requireSessionTeamMember(w, r, sessionID, userID) { return } // 403 first
   session, err := h.queries.GetSession(r.Context(), sessionID)       // lookup after
   ```

4. **Prefer a router-subtree middleware over per-handler calls** so new routes inherit the gate by default. Deuce uses per-handler helper calls (acceptable, but the audit must be redone whenever a route is added). The trade-off: read vs write gates differ per route, so a single middleware can only enforce the *outer* (read) gate; write paths still need the inner gate explicitly.
5. **Keep membership-granting operations inside the outer boundary.** You cannot grant write access to someone who can't even read. `AddSessionMember` constrains its target to team members via `IsSessionTeamMember` before inserting (`NOT_TEAM_MEMBER` otherwise) — else you create the incoherent state of a session member who can't read the session.
6. **Add one test per gate** — a positive (in-scope caller succeeds) and a negative (out-of-scope caller gets 403, before any existence check). One test per gated route, not one test for the helper.

## Why This Matters

"Safe by obscurity" boundaries are invisible in the code — nothing marks a route as relying on an undiscoverable ID. When the obscurity is removed by an unrelated feature (a broader list query, a self-serve join), every such route silently becomes an open door, and **no existing test fails** because the tests use legitimately-scoped IDs. A live shell into another team's container, or a workspace-delete, is a full compromise of that session. The only reliable detection is an adversarial route enumeration; the only reliable prevention is making "every resource route has an explicit, classified gate" an invariant that survives future route additions.

## When to Apply

- Any time a **list/index query is broadened** (membership → team, team → org, private → public-within-tenant).
- Any time a **self-serve join / discovery endpoint** is added that makes previously-private IDs enumerable.
- Any time you add a route that **takes a resource ID as a path param** — classify it read vs write and gate it, even if "the list already filters these."
- During **security review of any PR that changes a visibility/scope boundary** — enumerate routes, don't just read the diff.

## Examples

**Concentric gates by route class** (from `server/internal/server/server.go` + handler call sites):

| Route | Gate | Class |
|---|---|---|
| `GET /sessions/{id}`, `/messages`, `/activities`, `/agent-runs`, `/files`, `/files/content`, `/vscode-uri`, `POST /join` | `requireSessionTeamMember` | read |
| `PATCH /sessions/{id}`, `POST /messages`, `PUT /agents`, `POST /agents/stop`, `POST /members`, `DELETE /members/{userID}`, `POST /workspace/{start,stop,rebuild,delete}`, `GET /ws/terminal/{id}` | `requireSessionMember` | write / live |

**Gate-before-lookup returning 403 not a 404 oracle** (`GetSession`, verified): the read gate runs first, so a session on another team is indistinguishable from a nonexistent one — both return `403`.

**Membership grant stays inside the read boundary** (`AddSessionMember`): resolves the target user, then requires `IsSessionTeamMember(sessionID, target.ID)` before the insert, rejecting with `NOT_TEAM_MEMBER` otherwise — you can't be made a writer of something you can't read.

**The route that proves the checklist** (`/ws/terminal/{sessionID}` in `server/internal/handler/terminal.go`): it sits outside the `/api/sessions/{sessionID}` subtree entirely. A subtree-middleware-only approach misses it; an explicit route enumeration catches it. It now calls `requireSessionMember` before the workspace lookup.

## See Also

- Plan: `docs/plans/2026-06-08-001-feat-team-scoped-sessions-and-join-to-participate-plan.md` (KTD1, U2, risk R-A).
- **Supersedes KTD14's snapshot gating:** the agent-runs *snapshot* read moves from session-gated to team-gated under the two-gate model, while the *live* AgentRunEvent stream stays session-gated. KTD14 is referenced in several plan docs but no prior solution doc encoded it.
- Related but distinct (authentication vs authorization): `docs/solutions/architecture-patterns/embedded-ssh-proxy-for-vscode-remote.md` (per-user SSH key auth at the transport); the forge-proxy / unified-proxy auth-mode plans (identity at the edge).
