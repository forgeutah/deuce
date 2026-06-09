---
date: 2026-06-08
topic: single-deuce-agent
focus: Collapse the 5 agent roles into one "Deuce" agent that supports skills + subagents; can the Pi harness support it?
mode: repo-grounded
---

# Ideation: One "Deuce" Agent With Skills + Subagents

## Verdict

**Yes — Pi can support a single "Deuce" modulated by skills + subagents, and the design is closer than it looks.** Under the Pi harness, an agent's `role` and `system_prompt` are fetched from the DB but **never forwarded to Pi** — Pi already runs as one generic coding agent regardless of which of the 5 rows triggered it. The 5 "agents" are largely a UI fiction at the execution layer, so the collapse removes a distinction the harness already ignores.

> **Validated 2026-06-08** (spike against real `pi` 0.74.2; see `docs/solutions/architecture-patterns/pi-loads-agent-skills-standard-in-rpc-mode.md`). The compound-engineering plugin was exported to Pi's agent layout (`server/tmp/agent/`) and tested:
>
> - **Skills load in `--mode rpc`.** All 36 exported skills appeared via the `get_commands` RPC, each invocable as `/skill:<name>`. `--mode rpc` is just an output format, orthogonal to skill loading.
> - **Pi implements the [Agent Skills standard](https://agentskills.io/specification)** (same as Claude Code) and auto-discovers from `~/.pi/agent/skills/` — the sibling of where Deuce already provisions `ask-user.ts`. So provisioning is the existing `InstallPiExtension` mechanism generalized to a tree.
> - **Deterministic invocation exists:** sending a `prompt` of `/skill:ce-commit …` expands the skill before processing — no reliance on the model choosing to `read` it. This *replaces* Idea 1's prompt-prepend hack.
> - **Correction to the constraint below:** `--system-prompt` / `--append-system-prompt` *are* real launch flags, so a per-session "Deuce" identity prompt is available at launch. There is still no *mid-session per-task* override via an RPC command — but per-task specialization rides `/skill:` expansion, which is cleaner.
> - **Still open:** live model triggering (needs an API key) and subagents (the `agents/*.md` personas run via the `pi-subagents` extension, which forks child Pi processes that bypass Deuce's `pirun.Key` supervisor + WS attribution — the Survivor 3 design question).

The one real harness constraint (as originally framed): Pi `--mode rpc` has no per-session *system-prompt* override — but the task `Prompt` does reach Pi, which is the seam skills ride (Idea 1). *(See the validation note above — launch-time `--append-system-prompt` and `/skill:` expansion both turned out to be available, softening this constraint.)*

## Grounding Context (Codebase)

- **Agent model:** `agents` table (`id, name, role(free-text, never enum), color, color_muted, provider, model, description, system_prompt, deleted_at`), 5 hardcoded seed rows (Coder/Reviewer/Planner/Tester/Designer). `session_agents` join table `(session_id, agent_id, status, claude_session_id, pi_session_id)` allows multiple agents per session. Frontend `AgentRole = string` (deliberately un-enumerated).
- **Role is already decoupled from execution.** Under Pi, `role`/`system_prompt` are fetched (`messages.go`) but never forwarded. `EnqueueParams` carries only `{SessionID, AgentID, RequestedBy, AnchorMessageID, Prompt, WorkspaceID}`. The `system_prompt` is silently dropped on the Pi path — a live design-debt bug (only the legacy `claude` executor consumed `--append-system-prompt`).
- **@mention flow:** frontend `ChatView.tsx` regex-parses `@Name`, matches `session.agents` by name → UUID → `sendMessage({content, mentions:[uuid]})`. Backend `messages.go` loops mentions and enqueues to the Pi runtime. A near-miss name (`@coder`) silently no-ops.
- **Pi harness topology:** Go drives a persistent `pi --mode rpc` process inside each session's DevPod container via `devpod ssh`. Process granularity is `pirun.Key{SessionID, AgentID}` — one process per (session, agent) pair, serial queue per key. JSONL protocol; custom `ask-user.ts` extension provisioned to `~/.pi/agent/extensions/` via `pi.registerTool()` is the only extension today and the natural skills seam.
- **No "skills" concept exists** in the codebase. **Pi has no native subagent dispatch** in RPC mode; community `@tintinweb/pi-subagents` spawns child Pi instances (foreground works in RPC, background/scheduled degraded). Subagents today = the server spawning multiple Pi processes under separate agent rows.
- **Coupling points for a collapse:** DB schema (agents/session_agents/seed), `pirun.Key`, `EnqueueParams.AgentID`, the `messages.go` mention loop, frontend mention parse + per-agent message border colors, `SummaryPanel`/`AgentTaskCard`/`AgentThreadDrawer`/`AgentSettingsDialog`, WS payloads carrying `agentId`.

### External context

- Causal-independence is the criterion for safe role→single-agent collapse (arXiv:2601.04748): safe when roles don't feed back mid-session; 15–40% accuracy drop when downstream depends on upstream. In Deuce the human drives next steps → roles largely decoupled.
- Skill shadowing (arXiv:2605.24050): flat libraries degrade up to 21% at scale from selection failure; fix is hierarchical category→skill routing.
- Claude Code: Skills = inline file-based bounded capability bundles; Subagents (Task tool) = spawned child sessions w/ own context + restricted tools for parallel/isolated work. OpenAI/CrewAI/AutoGen lesson: start single-agent + tools, split only at real limits; over-splitting → routing failures, duplicated instructions, drift.

### Past learnings (docs/solutions/)

- No direct prior art on agent-role consolidation, the Pi RPC harness, or skills/subagents — treat as greenfield, strong `/ce-compound` candidate once the design lands.
- Mirror the "one identity model, one user table, no second source of truth" precedent (`embedded-ssh-proxy-for-vscode-remote.md`) when attributing Deuce + subagents.
- Re-run the route-authorization audit (`broadening-resource-visibility-requires-per-route-authorization-audit.md`) for any new "invoke skill" / "spawn subagent" / "set active agent" endpoint; `UpdateSessionAgents`/`StopAgent` are already write/live-gated.

## Topic Axes

- Agent identity & data model
- Skills (capability units)
- Subagents (parallel dispatch)
- Invocation & UX
- Migration & harness fit

## Ranked Ideas

### 1. Skill specialization rides the prompt channel — the keystone that ships on Pi today
**Description:** Since Pi RPC won't honor a system-prompt override but `EnqueueParams.Prompt` *does* reach Pi, a "skill" becomes a preparation block prepended to the task prompt (and/or a `pi.registerTool()` extension, the same seam `ask-user.ts` uses). "Behave like a reviewer" stops being a dropped DB column and becomes injected instruction Pi actually executes.
**Axis:** Skills
**Basis:** `direct:` — `system_prompt` is fetched (`messages.go`) but silently dropped on the Pi path; `EnqueueParams.Prompt` is the one channel that reaches Pi; `ask-user.ts` proves the `registerTool` seam works.
**Rationale:** Simultaneously pays down the live "prompt does nothing" debt and is the load-bearing primitive every skill depends on. Without it, "skills" have nowhere to attach.
**Downsides:** Prompt-prepend is weaker than a true system prompt (model can drift); registerTool path needs per-container extension provisioning.
**Confidence:** 90% **Complexity:** Low–Medium **Status:** Unexplored

### 2. Strangler collapse: 5 rows → skill-presets → one runtime, then delete
**Description:** Don't big-bang the schema. Keep the 5 `agents` rows as thin UI presets that all resolve to one `Key{SessionID}` runtime "Deuce"; convert each role's prompt into a skill; let `@Coder` desugar to "Deuce + coder skill." Collapse the rows later once telemetry shows nobody depends on the distinction.
**Axis:** Agent identity & data model (+ migration)
**Basis:** `reasoned:` + `direct:` — grounding lists heavy coupling (DB schema, `pirun.Key`, `EnqueueParams.AgentID`, mention loop, WS `agentId` payloads, sidebar/colors); a strangler keeps every `agentId` contract intact while redefining what it resolves to, so historical sessions still render and rollback stays cheap.
**Rationale:** Turns a destructive migration into an additive, reversible one; the 5 proven role prompts become the seed skill library instead of being discarded.
**Downsides:** Temporary two-model period (rows that lie about being agents); telemetry needed to know when to cut.
**Confidence:** 85% **Complexity:** Medium **Status:** Unexplored

### 3. One Pi process per session; subagents as the parallelism escape hatch
**Description:** Drop `AgentID` from `pirun.Key` → one persistent `pi --mode rpc` per session. The concurrency you get today from 5 keyed processes relocates downward to subagents spawned on demand (foreground `@tintinweb/pi-subagents`, or the server spawning child Pi processes — which is literally what the 5-agent model already does). "Deuce" owns ordering/continuity of the channel; subagents are bounded parallel workers that report back as Deuce.
**Axis:** Subagents (parallel dispatch)
**Basis:** `direct:` + `external:` — today's concurrency comes from per-(session,agent) processes with serial per-key queues; Pi has no native subagent dispatch but the multi-process machinery already exists; Unix fork/exec (identity in parent, specialization in child) and Anthropic's "subagents are a performance tier, not a role taxonomy."
**Rationale:** The hardest part of subagents (lifecycle, isolation, message routing) is already built by the 5-agent model — collapsing roles frees that machinery to serve as the real parallelism tier.
**Downsides:** A single per-session process serializes one user's turns unless subagents are wired in; background/scheduled Pi subagents are documented as degraded in RPC.
**Confidence:** 75% **Complexity:** Medium–High **Status:** Unexplored

### 4. Skills as git-versioned files compiled from docs/solutions/ — the compounding flywheel
**Description:** A skill is a directory (`SKILL.md` + optional `tool.ts`) discovered from the DevPod filesystem where Pi already loads `~/.pi/agent/extensions/`. A "skill-compiler" reads existing `docs/solutions/` learnings (already carrying `module`/`tags`/`problem_type` frontmatter) and emits skills — so every documented fix permanently upgrades the one Deuce agent. Match the Claude Code skill manifest shape for ecosystem interop.
**Axis:** Skills (capability units)
**Basis:** `external:` + `direct:` — Claude Code models skills as file-based bounded bundles; Pi loads extensions from the filesystem; the repo already ships structured `docs/solutions/` plus recently-committed compound-engineering plans.
**Rationale:** Distribution/versioning/review come free from git; an open-source project gets a community skill library with zero new infra; solve-once → document → auto-compile → never re-learn.
**Downsides:** Skill-compiler is real work; manifest-compat is a moving target.
**Confidence:** 70% **Complexity:** Medium–High **Status:** Unexplored

### 5. Hierarchical skill routing + auto-selection from day one (avoid the 21% shadowing cliff)
**Description:** Make skill selection automatic and invisible (Deuce routes by message content — the user never re-picks a "role"), and structure skills as category→skill from the start (mirroring the `docs/solutions/` category folders). Optionally enforce conjunctive preconditions per skill so only 1–2 fire per turn.
**Axis:** Skills / Invocation & UX
**Basis:** `external:` — flat skill libraries degrade up to 21% from selection failure at scale; the documented fix is hierarchical routing (arXiv:2605.24050).
**Rationale:** The single-agent move invites skill proliferation; designing the two-tier router before the library grows avoids a painful re-shard and an accuracy regression that would make Deuce feel dumber than the 5-role version.
**Downsides:** Auto-selection hurts discoverability ("what can Deuce do?"); a routing hop adds latency/cost.
**Confidence:** 70% **Complexity:** Medium **Status:** Unexplored

### 6. Keep the menu, drop the entities — roles become slash-presets; provenance badges replace role colors
**Description:** Users may have liked five buttons, not five beings. Reframe roles as invocation presets (`/review`, `/plan`) over one Deuce. Replace the per-agent message border colors (meaningless when one agent speaks) with a provenance chip — "via review skill" / "subagent: tester" — so the visual signal tracks behavior invoked, not identity picked. Also fixes the silent near-miss `@mention` no-op.
**Axis:** Invocation & UX
**Basis:** `external:` + `direct:` — UX research (arXiv:2601.18275) found users liked named personas for engagement but suffered attribution confusion; role colors and `@Name→UUID` mention-matching are concrete coupling points today.
**Rationale:** Ship the collapse while preserving the discoverable-specialization users actually valued.
**Downsides:** Loses the multi-participant "team in a channel" feel that STRATEGY.md bets on — needs a deliberate product call.
**Confidence:** 70% **Complexity:** Medium **Status:** Unexplored

### 7. Causal-independence as the explicit "is this overkill?" boundary
**Description:** The principled answer to the "probably overkill" intuition: collapsing roles is safe exactly when they don't feed each other's output mid-session — and risky (15–40% accuracy drop) when downstream depends on upstream (Planner→Coder→Reviewer chains). Encode it as a per-skill property ("produces feedback consumed mid-session: yes/no") that doubles as the runtime rule for when Deuce runs inline vs. spawns an isolated subagent.
**Axis:** Migration & harness fit / Subagents
**Basis:** `external:` — arXiv:2601.04748 causal-independence criterion. In Deuce the human drives the next step, so roles are largely decoupled → collapse is mostly safe.
**Rationale:** Replaces taste ("feels like overkill") with a measurable boundary; the same formalism that licenses the migration becomes the dispatch policy for skills-vs-subagents.
**Downsides:** Classifying feedback-dependence per skill is fuzzy; over-applying it re-introduces complexity you're trying to shed.
**Confidence:** 65% **Complexity:** Medium **Status:** Unexplored

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | "Deuce always listening" / no @mentions / ambient presence-trigger | Interesting but a separate product bet (presence model); better as a brainstorm variant than bundled with the collapse |
| 2 | Delete `agents` table outright / Zero-Agent / implicit singleton | Right endpoint, but Idea 2 (strangler) reaches it more safely; merged |
| 3 | 1000 Deuces / identity-per-spawn ephemeral children | Captured by Idea 3's subagent model; standalone it's speculative UI churn |
| 4 | Stem-cell potency, pharmacokinetic half-life, improv "yes-and", orchestra conductor, pin-tumbler, Swiss-army, ED triage | Framing devices — substance folded into Ideas 1/3/5; analogies alone aren't actionable moves |
| 5 | Auto-derive behavior from repo context (kill system_prompt) | Overlaps Ideas 1/4; "environment is the prompt" is downside-mitigation, not its own move |
| 6 | skill.json = Claude Code manifest subset | Folded into Idea 4 as the interop sub-point |

All five axes have survivors; no deliberate coverage gaps.
