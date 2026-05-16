---
date: 2026-05-14
topic: patch-stream-primitive
---

# Patch Stream Primitive

## Summary

A parallel event stream and dedicated table that captures every change a session produces — agent edits, human edits, and system-originated changes — broadcast over the existing per-session WebSocket subscription with explicit supersession links and a per-turn lifecycle. This is the foundation idea #1 from `docs/ideation/2026-05-14-changes-tab-vs-editor-ideation.md` sits on; the Changes view, hunk threads, PR derivation, plan-step linking, and parity-by-build CI lint are deliberately deferred to follow-up brainstorms that consume this primitive.

---

## Problem Frame

Today Deuce has zero visibility into code changes that happen inside a session. Sessions carry chat, plan, files (read-only with git status), and terminal — but the actual unit of work an agent or human produces (a coherent change to the codebase) has no first-class representation. Cross-functional teammates (per STRATEGY.md: engineer + designer + PM) cannot see scope as it forms; they only see the diff after it lands in a PR, which is exactly the wasted-cycles failure mode the strategy is built to prevent.

The downstream features the team wants to build — a Changes view that doubles as the audit log, anchored hunk threads that survive iteration, intent-only edits that re-prompt to a v2 patch, a PR header strip that derives from a contiguous patch range, plan-step ↔ patch linking, agent-callable parity for every UI gesture — are all consumers of the same underlying object: "here is a coherent change to the workspace, by this actor, at this point in time." Without that object existing as a first-class primitive, each feature would invent its own representation, the team's mental model would fracture across surfaces, and the "agents and humans coordinate around code" property the product is built on would be built piecemeal and inconsistently.

The cost of getting this primitive wrong is not the build effort to migrate it later — it is the conceptual drag of every downstream feature being shaped against the wrong shape. The cost of getting it right once is small relative to the value it unlocks.

---

## Actors

- A1. **Agent (in-session)**: Produces patches as a side-effect of completing a turn in response to a user prompt. In v0 this is the simulated agent (canned responses). When real agents land, they will emit patches via the same contract.
- A2. **Human teammate (in-session)**: Indirectly produces patches when their local-IDE edits flow back into the session (deferred — see Scope Boundaries). In v0, humans do not produce patches; they observe them.
- A3. **System**: Produces patches for non-actor changes the workspace picks up later (git pulls, CI auto-formatting commits, rebase results). Origin type exists in v0; producer wiring is deferred.
- A4. **Downstream consumer (UI)**: Subscribes to patch events on the per-session WebSocket channel and renders them. In v0, this is a minimum-demonstration renderer in chat. The full Changes view is a separate brainstorm.
- A5. **Downstream consumer (future features)**: PR composer, hunk-anchored threads, plan-step linker, time-travel scrubber, parity-by-build registry. None exist in v0; the primitive is shaped to support them without re-design.

---

## Key Flows

- F1. **Per-turn patch emission (agent path)**
  - **Trigger:** An agent's turn completes in a session (the agent has finished responding to a prompt).
  - **Actors:** A1, A4
  - **Steps:**
    1. At turn start, the workspace HEAD is captured.
    2. The agent does work (in v0, returns a canned response; in v1, runs Claude Code in the devcontainer).
    3. At turn end, the change set is computed against the captured workspace HEAD.
    4. If non-empty, a patch is created with origin = agent, the captured workspace HEAD as its parent reference, and (if applicable) the prior patch this iteration replaces.
    5. The patch is persisted and broadcast to all clients subscribed to the session.
    6. The chat surface renders a minimum-demonstration marker showing the patch landed.
  - **Outcome:** A patch row exists, downstream consumers have received the broadcast, and the chat surface visibly reflects that work was done.
  - **Covered by:** R1, R2, R3, R5, R7, R10, R12

- F2. **Re-prompt produces a superseding patch**
  - **Trigger:** A user prompts an agent to revise prior work (e.g., "tighten this loop").
  - **Actors:** A1, A4
  - **Steps:**
    1. The agent recognizes its turn is iterating on a prior patch (in v0 this is set explicitly by the simulated handler; in v1 the agent receives the prior patch's identifier in its context).
    2. F1 runs.
    3. The new patch's supersession link points to the prior patch.
    4. The broadcast carries the supersession link so consumers can render iteration history.
  - **Outcome:** A v2 patch exists linked to v1; consumers can collapse to the latest by default and reveal the chain on demand.
  - **Covered by:** R4, R6

- F3. **Stale patch detection**
  - **Trigger:** A consumer renders a patch whose parent reference no longer matches the workspace's current HEAD.
  - **Actors:** A4
  - **Steps:**
    1. The consumer compares the patch's recorded workspace HEAD to the current workspace HEAD.
    2. If they differ and no supersession chain bridges the gap, the patch is flagged as stale.
  - **Outcome:** Stale patches are observable but not auto-removed; the user decides what to do with them.
  - **Covered by:** R9

---

## Requirements

**Storage and shape**
- R1. Patches are stored in a dedicated table, not embedded in messages or activity items.
- R2. Each patch carries: a stable identifier, the session it belongs to, the workspace HEAD captured at turn start, the actor's origin type (agent / human / system), the unified-diff hunks describing the change, and timestamps.
- R3. Each patch carries a nullable link to a prior patch it supersedes. A null link means the patch is not iterating on prior work.
- R4. Each patch carries a nullable flag indicating whether it has been promoted to a git commit (set when promotion happens; null until then). The promotion mechanism itself is out of scope (see Scope Boundaries).
- R5. The hunks travel as standard unified-diff data. No severity, grouping, or intent tags are computed at patch time.

**Lifecycle**
- R6. A patch is created at turn end, computed against the workspace HEAD captured at turn start. If the change set is empty, no patch is emitted.
- R7. A patch's supersession link is set by the producer at emission time. The system does not infer supersession from file overlap, temporal proximity, or any other heuristic.
- R8. Once written, patches are append-only. Iterations produce new patches with supersession links; the system does not mutate prior patches.

**Staleness**
- R9. A patch is considered stale when the workspace HEAD has moved past the patch's recorded HEAD and no supersession chain connects the patch to the current state. Staleness is derivable; it is not stored.

**Broadcast**
- R10. When a patch is created, it is broadcast on the existing per-session WebSocket subscription as a new event type, alongside the current event types.
- R11. The broadcast payload contains enough information for consumers to render and route without an additional fetch.

**Producers**
- R12. The simulated-agent handler emits a patch when it completes a turn that contains a (canned) change set. Seed data includes a representative set of patches sufficient to exercise the rendering and broadcast paths.
- R13. The patch-emission interface is documented as the contract real agents will plug into. Real-agent producer wiring itself is out of scope (see Dependencies).

**Demonstration UI**
- R14. The chat surface renders a minimum marker when a patch lands, sufficient to confirm the patch arrived and contains the right shape (file count, hunk count, origin, supersession indicator). The full Changes view is out of scope.

---

## Acceptance Examples

- AE1. **Covers R6, R10.** Given an agent in a session has been prompted with `@Coder add a hello endpoint`, when the agent's turn completes and the canned response includes a change to one file, then a patch is persisted and a broadcast event is delivered to all clients subscribed to that session.
- AE2. **Covers R6.** Given an agent in a session has been prompted with `@Coder explain the auth flow`, when the agent's turn completes and the canned response contains no file changes, then no patch is emitted and no broadcast is sent.
- AE3. **Covers R3, R7.** Given a patch v1 exists in a session, when the simulated handler emits a patch v2 with supersession set to v1, then v2's supersession link references v1 and the broadcast event carries the link.
- AE4. **Covers R9.** Given a patch was created when workspace HEAD was `abc123`, when the workspace HEAD has since moved to `def456` and no supersession chain bridges them, then a consumer that compares the patch's recorded HEAD to the current HEAD can mark it stale.
- AE5. **Covers R8.** Given a patch v1 exists, when an iteration produces a patch v2 with supersession set to v1, then v1's row is unchanged in the table.
- AE6. **Covers R12.** Given the seed data is loaded into a fresh database, when a session is opened, then at least one patch exists in the seed-loaded session and is renderable end-to-end through broadcast and chat-surface marker.

---

## Success Criteria

- A teammate can join a session, see at least one patch land in chat, and understand from the marker alone that work was done, by whom, and roughly what changed (file count, hunk count) — without the full Changes view being built yet.
- A downstream brainstorm (Changes view, PR derivation, hunk threads, plan-step linking) can be planned and implemented against the patch primitive without proposing changes to its shape, lifecycle, or broadcast contract.
- The real-agents-in-devcontainers integration plugs into the documented patch-emission contract without re-design.
- A re-prompt produces a v2 patch whose supersession link is correctly set, and a consumer can traverse the v1→v2 chain.

---

## Scope Boundaries

- The Changes view UI (idea #2 from the ideation) — separate brainstorm; consumes this primitive. The v0 chat-surface marker is the minimum demonstration UI, not the Changes view.
- Hunk-anchored threads (idea #4) — separate brainstorm; depends on this primitive plus a threading feature.
- Intent-only edits and the "no textarea, ever" ergonomics decision (idea #3) — separate brainstorm; uses the supersession link but does not shape the primitive.
- PR header strip and PR derivation from a contiguous patch range (idea #5) — separate brainstorm; the deferred commit-promotion flag (R4) is the v0 hook.
- Plan-step ↔ patch linking (idea #7) — separate brainstorm.
- Parity-by-build CI lint (idea #6) — separate brainstorm; the patch-emission interface is a candidate for the first registered operation.
- Severity, grouping, or intent tags computed at patch time — render-layer concern; defer until a consumer needs them.
- Time-travel scrubber, agent replay, cross-session patch indexing — speculative consumers; defer until each has a concrete use-case.
- Threading on messages — separate feature; the primitive does not depend on it. When threading lands, it can become a way to auto-derive supersession links, but the explicit producer-set link stays.
- Human FS-watch producer for local IDE edits — deferred to the intent-only-edits brainstorm, which decides whether the deeplink-to-VS-Code path is the canonical human-edit channel.
- Real-agent integration code — handled in `docs/brainstorms/2026-05-08-real-agents-in-devcontainers-brainstorm.md` (currently Draft). This brainstorm specifies the contract; the integration is its own work.
- Producer code paths for the system origin type (git pulls, CI commits picked up later) — origin type exists in v0 so consumers do not have to be revised when those producers land, but no producer for them ships in v0.

---

## Key Decisions

- **Patches are a parallel event stream + table, not message attachments.** The seductive option was to ride on the existing JSONB `expandable_content` field. We chose the table because the "single source of truth" framing is load-bearing for downstream features, and because patches without a chat author (system origin, future human FS-watch) are real and near-term — a table makes them natural; embedding in messages forces synthetic-message awkwardness forever.
- **Per-turn lifecycle, not per-write or per-commit.** Per-write streaming would create storage bloat and force aggregation everywhere. Git-commits-only would break the "see what's happening live" property and force commit ceremony on every actor. Per-turn captures a unit of intent, which is what reviewers want to see.
- **Explicit supersession via a producer-set link, not derived from chat threading.** Threading does not exist on messages today, and making the primitive depend on a feature that has not been built would block everything downstream. Explicit links work today and remain the canonical mechanism even after threading lands (threading then becomes a way to auto-derive the link).
- **Commit modeling deferred.** A nullable promotion flag on each patch is enough to unblock the future PR brainstorm. A separate `commits` table is not needed in v0 and would be premature.
- **Seed + simulated producers for v0; real-agent integration parallels.** Decouples this work from the real-agents milestone. The primitive can land, downstream features can be planned and built against it, and real-agent wiring becomes an integration task when that brainstorm ships.
- **Standard unified-diff hunks, no derived tags at patch time.** Severity / grouping / intent are render-layer concerns; computing them at patch time would either commit to one rendering model prematurely or duplicate logic later.

---

## Dependencies / Assumptions

- The existing per-session WebSocket subscription (`server/internal/ws/hub.go`) supports adding new event types without architectural changes. Verified.
- The simulated-agent handler (`server/internal/handler/messages.go` per CLAUDE.md) is the v0 producer entry point. Verified the file exists and produces canned responses.
- The workspace's host-FS access pattern (per `docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md`) is the data plane any future patch-computation code will use. Verified.
- The `messages.expandable_content` JSONB column already supports referencing patches by identifier from chat. Verified — the column exists; the `"diff"` variant is already declared in `src/types/index.ts`.
- The real-agents-in-devcontainers brainstorm is currently Draft. This primitive ships independently of that work; integration is sequenced when that brainstorm ships.

---

## Outstanding Questions

### Resolve Before Planning

- *(None — all scope-shaping calls are resolved in Key Decisions or Scope Boundaries.)*

### Deferred to Planning

- [Affects R2][Technical] Exact column types, indexes, and table name conventions for the patches table — settle in `cd server && make generate` flow per CLAUDE.md.
- [Affects R10][Technical] Exact WebSocket event type name and payload shape — follow the convention established in `server/internal/ws/events.go`.
- [Affects R12][Technical] Shape and quantity of seed patches — enough to exercise broadcast, supersession link rendering, and stale detection in the demonstration UI; specific content is a planning call.
- [Affects R14][Design] Exact visual treatment of the chat-surface marker — small enough to be the minimum demonstration without becoming the Changes view.
- [Affects R6][Technical][Needs research] How turn boundaries are detected for autonomous / background agent work that lands later. In v0 the simulated handler defines its own turn boundaries; for real agents in v1, planning should investigate whether Claude Code's headless mode provides a usable signal or whether Deuce has to wrap its own boundary.
