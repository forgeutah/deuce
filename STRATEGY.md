---
name: Deuce
last_updated: 2026-05-11
---

# Deuce Strategy

## Target problem

Small cross-functional teams (engineer + designer + PM) are losing alignment because AI coding agents privatize the planning phase: one person scopes and prompts in a solo agent session, and the rest of the team only sees the work after the diff lands — too late to shape scope, design, or intent. The result is wasted cycles building the wrong thing and a team that's no longer a team for the work that matters most.

## Our approach

Treat agents as teammates rather than personal tools. Build one shared workspace — humans (engineer, designer, PM) and agents together in the same channel — that carries the team across scope, plan, design, build, and test, so the team accomplishes more of the *right* things instead of more things faster.

## Who it's for

**Primary:** Small cross-functional product teams — anywhere from 2 humans + agents up to ~8 people — where every member spans more than one discipline and AI agents are doing most of the building. They're hiring Deuce to **stay aligned with their teammates (human and agent) across scope, plan, design, build, and test, so the team ships the right thing once instead of redirecting at PR review.**

## Key metrics

- **Multi-human sessions / week** — Sessions with ≥2 humans active. Tests whether teams actually show up together vs. one person driving solo. (Leading. Measured from session activity in the DB.)
- **Plan-before-build rate** — % of sessions where a plan exists before the first agent build action. Tests whether the upstream phases actually move into Deuce. (Leading. Not yet instrumented — needs defining.)
- **Course-corrections caught pre-build** — Signal of redirection happening during scope/plan/design instead of at PR review. (Lagging. Not yet instrumented — needs defining; candidate signals include plan edits before agent runs, self-reported redirections.)
- **Weekly retained teams** — Teams that return week-over-week. Tests whether the workspace became part of how teams actually work. (Lagging. Measured from session activity.)

## Tracks

### Agent Team

LLM integrations, agent harnesses, and the plan tab — the "how agents think and act as teammates" layer.

_Why it serves the approach:_ Agent behavior has to be shaped by shared team context (plan, channel history, design intent) rather than isolated CLI prompts; this track owns that wiring.

### Chat & Presence

Real-time backend and chat UI — the channel itself, where humans and agents are visibly co-present.

_Why it serves the approach:_ Makes the "shared" in "shared workspace" actually visible and continuous. Without this, agents become tools again.

### Coding & Preview

File system browsing, collaborative VS Code, GitHub PR integration, live UI previews, screenshots, and annotations. **Every collaborative surface here must be agent-callable** — agents do most of the design and build work, so they need the same handles humans do.

_Why it serves the approach:_ Carries the build–design–test phases into the workspace itself instead of leaving them in private editors and async PRs. Agent-native parity is a hard constraint, not a stretch goal.
