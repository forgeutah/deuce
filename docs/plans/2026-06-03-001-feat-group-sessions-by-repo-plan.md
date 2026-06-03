---
title: "feat: Group sessions by repository instead of project"
type: feat
status: completed
date: 2026-06-03
depth: lightweight
origin: none (direct request)
---

# feat: Group sessions by repository instead of project

## Summary

The session sidebar currently groups sessions by **project**, showing the project
name as each collapsible group header. Because the seed `acme-dashboard` project
maps to `github.com/acmecorp/dashboard`, users perceive an "acme" group that does
not match how they think about their work — by repository. This plan regroups the
sidebar **by repository**, keyed on each project's `repoUrl`, and labels each group
`owner/repo` (e.g. `acmecorp/dashboard`). Sessions whose projects share a `repoUrl`
merge into one group.

The change is frontend-only and contained almost entirely to
[SessionSidebar.tsx](src/components/layout/SessionSidebar.tsx). The data needed
already exists — `Project.repoUrl` ([types/index.ts:30](src/types/index.ts#L30)) —
so no backend, schema, or API changes are required.

---

## Problem Frame

- **Today:** Grouping is derived in the component by iterating `projects` and
  filtering sessions on `session.projectId === project.id`
  ([SessionSidebar.tsx:274-277](src/components/layout/SessionSidebar.tsx#L274-L277)).
  The group header renders `project.name`
  ([SessionSidebar.tsx:233](src/components/layout/SessionSidebar.tsx#L233)).
- **Desired:** Group by repository. The grouping key becomes the project's
  `repoUrl`; the header shows a derived `owner/repo` label; sessions from any
  projects sharing a `repoUrl` appear under one header.
- **Why it matters:** Repositories are the user's mental unit of work. Project
  names (`acme-dashboard`) drift from repo identity (`acmecorp/dashboard`) and read
  as an org/team grouping the user did not ask for.

---

## Scope Boundaries

**In scope**
- Regroup the session sidebar by `repoUrl`, merging projects that share a repo.
- Derive and render an `owner/repo` group label from a repo URL.
- Handle missing/empty/unparseable `repoUrl` and orphan sessions gracefully.

**Out of scope (non-goals)**
- Adding a `repoUrl`/`repo` column to the `sessions` table or session API
  responses — the existing project lookup is sufficient.
- Changing how sessions are created or which project a session is assigned to
  ([CreateSessionDialog.tsx](src/components/session/CreateSessionDialog.tsx) is
  untouched).
- Backend session queries, migrations, or seed data changes.

### Deferred to Follow-Up Work
- Normalizing seed project names to match repo names (cosmetic; not required for
  repo grouping to work).
- Showing the team/org as a second grouping tier above repos.

---

## Key Technical Decisions

1. **Group key = `repoUrl`, derived via project lookup (not a new Session field).**
   Sessions reference a `projectId`; the project carries `repoUrl`. Build a
   `projectId → repoUrl` map, then group sessions by the resolved `repoUrl`. This
   honors the "merge projects sharing a repo" decision and avoids any schema/API
   change.

2. **Label format = `owner/repo`** (user-confirmed). Derive from the repo URL by
   taking the last two path segments, stripping a trailing `.git`. Support both
   HTTPS (`https://github.com/acmecorp/dashboard`) and SSH-style
   (`git@github.com:acmecorp/dashboard.git`) forms. Falls back to the raw string
   when only one segment is parseable, and to a fixed `No repository` label when
   `repoUrl` is empty.

3. **Extract label derivation into a pure helper** (`repoGroupLabel`) in
   [src/lib/repo.ts](src/lib/repo.ts). Pure string-in/string-out keeps it unit-
   testable independent of React and isolates the URL-parsing edge cases from the
   component.

4. **Group ordering = most-recent activity first.** Order repo groups by the max
   `lastActivityAt` of their sessions (descending), matching the existing
   within-group sort. Deterministic and keeps active repos at the top. (Current
   behavior orders by `projects` array position; that ordering is not meaningful
   to preserve once we merge by repo.)

5. **Orphan sessions** (a `projectId` with no matching project, or a project with
   empty `repoUrl`) collapse into a single `No repository` group rather than being
   silently dropped. Today's project-driven iteration drops them; surfacing them
   under a clear fallback is strictly better and costs nothing.

---

## Implementation Units

### U1. Add `repoGroupLabel` repo-URL helper

**Goal:** A pure function that turns a repo URL into an `owner/repo` display label,
with well-defined fallbacks.

**Requirements:** Supports KTD #2 (label format) and KTD #3 (testable isolation).

**Dependencies:** none.

**Files:**
- `src/lib/repo.ts` (create)
- `src/lib/repo.test.ts` (create — see Test expectation note)

**Approach:**
- Export `repoGroupLabel(repoUrl: string): string`.
- Normalize: trim; if empty → return `"No repository"`.
- Strip a trailing `.git`. Handle SSH form by splitting on `:` after an `@host`
  segment; handle HTTPS form via path segments.
- Take the last two non-empty path segments and join as `owner/repo`. If only one
  segment is available, return it as-is. If nothing parseable remains, return the
  original trimmed input.
- Keep it dependency-free (no `URL` reliance that throws on SSH form — parse
  defensively with string ops).

**Patterns to follow:** Other small pure utilities in [src/lib/](src/lib/) (e.g.
[src/lib/utils.ts](src/lib/utils.ts)) — named exports, no side effects.

**Test scenarios:**
- HTTPS URL `https://github.com/acmecorp/dashboard` → `acmecorp/dashboard`.
- HTTPS URL with trailing `.git` `https://github.com/forgeutah/forge-api.git` →
  `forgeutah/forge-api`.
- HTTPS URL with trailing slash → no empty segment, correct `owner/repo`.
- SSH form `git@github.com:acmecorp/dashboard.git` → `acmecorp/dashboard`.
- Empty string → `No repository`.
- Single-segment / unparseable input (e.g. `dashboard`) → returns input unchanged.

**Test expectation:** The repo has no frontend test runner yet (CLAUDE.md: "no test
suite yet"). Write `src/lib/repo.test.ts` as Vitest-style specs so the cases are
captured and runnable the moment a runner is added; verification for now is via
`npx tsc --noEmit` plus the manual sidebar check in U2. Do not block this unit on a
test runner that does not exist.

**Verification:** `npx tsc --noEmit` passes; the helper returns the expected label
for each scenario above when exercised manually or once a runner exists.

---

### U2. Regroup the sidebar by repository

**Goal:** Replace project-based grouping with repo-based grouping in the sidebar,
merging projects that share a `repoUrl`, ordered by recent activity, with
`owner/repo` headers.

**Requirements:** Implements KTD #1, #2, #4, #5. This is the user-visible change.

**Dependencies:** U1.

**Files:**
- `src/components/layout/SessionSidebar.tsx` (modify)

**Approach:**
- Build a `projectId → repoUrl` lookup from `projects`.
- Reduce `filteredSessions` into groups keyed by resolved `repoUrl` (empty/missing
  → a single `No repository` bucket per KTD #5).
- For each group compute its label via `repoGroupLabel(repoUrl)` and its sort key
  (max `lastActivityAt` across the group's sessions).
- Sort groups by that key descending (KTD #4); keep the existing within-group sort
  by `lastActivityAt` descending.
- Rename `ProjectGroup` → `RepoGroup`; change its prop from `project: Project` to
  `label: string` (and a stable `key`). Render `label` in place of `project.name`
  at [SessionSidebar.tsx:233](src/components/layout/SessionSidebar.tsx#L233).
- Update the render loop ([SessionSidebar.tsx:318-329](src/components/layout/SessionSidebar.tsx#L318-L329))
  to map over repo groups. Preserve the existing empty-state message
  ([SessionSidebar.tsx:330-334](src/components/layout/SessionSidebar.tsx#L330-L334)).
- Remove the now-unused `Project` import if nothing else needs it.

**Patterns to follow:** Existing `RepoGroup`/`SessionCard` structure, collapsible
state (`useState(true)`), and Tailwind classes already in the file — only the
grouping derivation and header label change.

**Test scenarios:**
- Seed data renders three repo groups labeled `forgeutah/forge-api`,
  `forgeutah/forge-web`, `acmecorp/dashboard`; the former "acme-dashboard" header
  no longer appears.
- Two projects pointing at the same `repoUrl` (temporarily add one in seed, or
  reason through) merge into one group containing all their sessions.
- A session whose project has empty `repoUrl` appears under `No repository`, not
  dropped.
- Search filter still narrows sessions and empty groups do not render (the
  `projectSessions.length > 0` guard equivalent is preserved per group).
- Groups appear ordered with the most-recently-active repo first.

**Test expectation:** No runnable UI test harness yet. Verify via `npm run dev` and
visual inspection of the sidebar against the scenarios above, plus
`npx tsc --noEmit` and `npm run lint`.

**Verification:** Sidebar shows `owner/repo` group headers; sessions merge by repo;
`No repository` fallback works; `npx tsc --noEmit` and `npm run lint` pass.

---

## System-Wide Impact

- **Frontend only.** No backend, DB, API, or WebSocket changes. `Session.projectId`
  and `Project.repoUrl` are consumed read-only; their shapes are unchanged.
- **No data migration.** Existing sessions/projects render under the new grouping
  immediately.
- **Other consumers unaffected.** Only [SessionSidebar.tsx](src/components/layout/SessionSidebar.tsx)
  derives grouping; [CreateSessionDialog.tsx](src/components/session/CreateSessionDialog.tsx)
  uses `projectId` for creation only and needs no change.

---

## Risks & Dependencies

- **Low risk.** Presentation-layer regrouping of data that already exists.
- **Edge case — empty `repoUrl`:** `Project.repoUrl` defaults to `''` in the schema;
  the `No repository` fallback (KTD #5) covers this so no group key is ever blank.
- **No test runner:** the frontend has no test suite, so U1's spec file is not
  executable today. Mitigation: keep the helper pure and verify behavior via type
  check + manual sidebar inspection; the spec is ready for the eventual runner.

---

## Verification Strategy

1. `npx tsc --noEmit` — type check passes.
2. `npm run lint` — no new lint errors (e.g. unused `Project` import removed).
3. `npm run dev` — sidebar shows `owner/repo` headers, the old `acme-dashboard`
   header is gone, sessions group/merge by repo, recent-activity ordering holds,
   and search still filters.
