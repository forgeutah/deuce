// Repo-identity helpers for grouping sessions by repository.
//
// Sessions reference a project, and a project carries a `repoUrl`. The session
// sidebar groups sessions by repository and labels each group `owner/repo`.
// URLs arrive in either HTTPS (`https://github.com/acmecorp/dashboard`) or SSH
// (`git@github.com:acmecorp/dashboard.git`) form, with optional `.git` suffix,
// trailing slash, or query/fragment. These helpers are pure
// string-in/string-out so the parsing edge cases stay isolated from the
// component and remain unit-testable.
//
// Two values are derived from a URL:
//   - the *group key* (`host/owner/repo`) used to merge sessions — both clone
//     forms of one repo normalize to the same key, so they group together;
//   - the *display label* (`owner/repo`) shown in the group header.
// This app is GitHub-shaped, so `host/owner/repo` is the assumed path contract;
// deeper paths (e.g. GitLab subgroups) keep only their last two segments.

/** Label shown for sessions whose project has no resolvable repository. */
export const NO_REPO_LABEL = "No repository";

/**
 * Normalize a repo URL to its path segments, host first.
 *
 * Strips query/fragment, scheme or `git@host:` SSH prefix, a trailing `.git`,
 * and trailing slashes. HTTPS and SSH forms of the same repo yield identical
 * segments (e.g. both `github.com`, `acmecorp`, `dashboard`). Returns `[]` for
 * empty/whitespace input.
 */
function repoSegments(repoUrl: string): string[] {
  const trimmed = repoUrl.trim();
  if (!trimmed) return [];

  // Drop a query string or fragment before structural parsing.
  const noQuery = trimmed.split(/[?#]/, 1)[0];

  // SSH form `git@host:owner/repo` → rewrite to `host/owner/repo` so it
  // normalizes to the same segments as the HTTPS form. Otherwise drop a leading
  // `scheme://` so `://` does not produce empty segments. `new URL()` is avoided
  // because it throws on the SSH form.
  const sshMatch = /^[^@/]+@([^:/]+):(.+)$/.exec(noQuery);
  const path = sshMatch
    ? `${sshMatch[1]}/${sshMatch[2]}`
    : noQuery.replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, "");

  return path
    .replace(/\/+$/, "") // trailing slashes, before the anchored `.git` strip
    .replace(/\.git$/i, "")
    .split("/")
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * Canonical key used to merge sessions into one repository group. Both clone
 * forms of a repo produce the same key; different hosts stay distinct.
 * Lower-cased because hosts and GitHub owner/repo names are case-insensitive.
 * Returns `""` for input with no resolvable repository (the "No repository"
 * bucket).
 */
export function repoGroupKey(repoUrl: string): string {
  return repoSegments(repoUrl).join("/").toLowerCase();
}

/**
 * Derive an `owner/repo` display label from a repository URL.
 *
 * - Empty/whitespace input → `NO_REPO_LABEL`.
 * - HTTPS and SSH forms both yield `owner/repo` (the last two path segments).
 * - When only one segment is parseable, that segment is returned as-is.
 */
export function repoGroupLabel(repoUrl: string): string {
  const segments = repoSegments(repoUrl);
  if (segments.length === 0) return NO_REPO_LABEL;
  if (segments.length === 1) return segments[0];
  return segments.slice(-2).join("/");
}
