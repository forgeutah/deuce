---
title: Pi loads Agent-Skills-standard skills in --mode rpc; provision them to ~/.pi/agent/skills and invoke via /skill: prompt expansion
date: 2026-06-08
category: architecture-patterns
module: agent/pirun
problem_type: architecture_pattern
component: agent-harness
severity: medium
applies_when:
  - "You want one generic Pi agent whose behavior is modulated by skills/subagents instead of multiple role-specific agent rows"
  - "You are deciding whether the compound-engineering plugin (or any Claude Code / Agent Skills bundle) can run on the Pi harness Deuce drives in --mode rpc"
  - "You need to know how to provision and invoke skills inside a per-session DevPod container over the existing devpod ssh channel"
related_components:
  - agent-harness
  - devpod
  - workspace-provisioning
tags:
  - pi
  - pi-rpc
  - agent-skills
  - skills
  - subagents
  - compound-engineering
  - claude-code-compat
  - append-system-prompt
---

# Pi loads Agent-Skills-standard skills in `--mode rpc`

## Context

Deuce models AI helpers as five role rows in an `agents` table (Coder/Reviewer/Planner/Tester/Designer), but under the Pi harness `role` and `system_prompt` are fetched and never forwarded — Pi runs as one generic coding agent regardless. The open question was whether collapsing to a single "Deuce" agent **modulated by skills and subagents** is viable on the Pi harness Deuce already drives (`pi --mode rpc` launched in each session's DevPod container — see [devpod_launcher.go](../../../server/internal/agent/pirun/devpod_launcher.go)).

The compound-engineering plugin was exported to Pi's agent-config layout (in `server/tmp/agent/`: `skills/<name>/SKILL.md`, `agents/<name>.md`, `AGENTS.md`, `install-manifest.json`). This documents what a spike against the real `pi` 0.74.2 binary established about whether that bundle works in RPC mode.

## Findings (verified)

1. **Pi implements the [Agent Skills standard](https://agentskills.io/specification)** — the same standard Claude Code uses. A skill is a directory with a `SKILL.md` (YAML frontmatter `name` + `description`, then markdown instructions). Bundles authored for Claude Code drop in unmodified.

2. **Skills auto-discover from `~/.pi/agent/skills/`** (Pi's documented global location), which is the exact sibling of `~/.pi/agent/extensions/` where Deuce already provisions `ask-user.ts` via `InstallPiExtension` ([manager.go](../../../server/internal/workspace/manager.go)). Pi can also be pointed at `~/.claude/skills` via the `skills` settings array, or given explicit `--skill <path>` (additive even with `--no-skills`). No launcher-flag change is required to pick up the global dir.

3. **`--mode rpc` is an output format, orthogonal to skill loading.** Skills load identically in text and rpc modes. Empirically: driving `pi --mode rpc --skill <bundle>/skills` and sending `{"type":"get_commands"}` returned all 36 exported skills as `skill:<name>` commands (`source: "skill"`), each invocable as `/skill:<name>`. This needs no API key — loading happens at startup before any model call.

4. **Two invocation paths, one deterministic:**
   - *Model-driven:* at startup Pi injects each skill's name+description into the system prompt (progressive disclosure per the spec); on a matching task the model uses `read` to load the full `SKILL.md`. Models "don't always do this."
   - *Deterministic (preferred for Deuce):* send `{"type":"prompt","message":"/skill:ce-commit ..."}` — **skill commands are expanded before the prompt is processed** (Pi rpc.md). Arguments after the command are appended as `User: <args>`. This does not depend on the model choosing to bite.
   - `get_commands` (not `get_state`) enumerates available skills/prompts/extension-commands — use it to populate UI or validate `@mention`→skill mapping. `get_state` does **not** list skills.

5. **`--system-prompt` / `--append-system-prompt` are real launch flags.** A single "Deuce" identity prompt can be set at process launch. (There is still no *mid-session per-task* system-prompt override via an RPC command — per-task specialization rides `/skill:` expansion instead, which is cleaner than prepending prompt text.)

## Not yet verified

- **Live model triggering** of the model-driven path needs an API key (absent in the spike env). The deterministic `/skill:` path sidesteps this.
- **Subagents.** The export's `agents/*.md` personas are consumed by the **`pi-subagents`** npm extension (provides the `subagent` tool), *not* Pi core — `AGENTS.md` lists it as required (`pi install npm:pi-subagents`), with `pi-ask-user` recommended (the published equivalent of Deuce's hand-rolled `ask-user.ts`). A `pi-subagents`-spawned subagent forks a child Pi instance **inside the container**, bypassing Deuce's `pirun.Key{SessionID, AgentID}` supervisor, serial queue, and WS `agentId` attribution. Whether subagents run under Pi (cheap, invisible to Deuce) or are orchestrated by Deuce (attributable, more work) is an open design decision.

## Integration path for Deuce

1. **Provision skills:** generalize `InstallPiExtension` ([manager.go](../../../server/internal/workspace/manager.go)) to push the `skills/` tree (and later `agents/`) into `~/.pi/agent/skills/` — same `mkdir -p` + base64-over-`devpod ssh` mechanism, a tar instead of a single file. Wire it into `provisionAgentTools` ([workspace.go](../../../server/internal/handler/workspace.go)) beside the ask-user install. No launcher change (auto-discovery).
2. **Invoke:** map `@Coder` → a `prompt` command carrying `/skill:coder`; enumerate with `get_commands` to validate.
3. **Identity:** add `--append-system-prompt` to the launch command ([devpod_launcher.go](../../../server/internal/agent/pirun/devpod_launcher.go)) for the single Deuce persona.

## Reproduce

`pi` 0.74.2; bundle at `server/tmp/agent`; harness `server/tmp/spike-pi-skills.sh`:

```bash
printf '%s\n' '{"type":"get_commands"}' \
  | pi --mode rpc --skill server/tmp/agent/skills --offline --no-session \
  | grep -o '"source":"skill"' | wc -l      # => 36
```

Pi's own docs are vendored with the npm package at
`@earendil-works/pi-coding-agent/docs/{skills,rpc,extensions}.md` — authoritative reference for discovery locations and the RPC command surface.

## Related

- [[broadening-resource-visibility-requires-per-route-authorization-audit]] — any new "invoke skill" / "spawn subagent" route must carry an explicit auth gate.
- [[embedded-ssh-proxy-for-vscode-remote]] — same host→container provisioning topology; "one identity model, one user table" precedent for attributing Deuce + subagents.
- Ideation: `docs/ideation/2026-06-08-single-deuce-agent-ideation.md`.
