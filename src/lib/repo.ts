// Repo-identity helpers for grouping sessions by repository.
//
// Sessions reference a project, and a project carries a `repoUrl`. The session
// sidebar groups by that URL and labels each group `owner/repo`. URLs arrive in
// either HTTPS (`https://github.com/acmecorp/dashboard`) or SSH
// (`git@github.com:acmecorp/dashboard.git`) form, with optional `.git` suffix or
// trailing slash. These helpers are pure string-in/string-out so the parsing
// edge cases stay isolated from the component and remain unit-testable.

/** Label shown for sessions whose project has no resolvable repository. */
export const NO_REPO_LABEL = "No repository";

/**
 * Derive an `owner/repo` display label from a repository URL.
 *
 * - Empty/whitespace input → `NO_REPO_LABEL`.
 * - HTTPS and SSH forms both yield `owner/repo`.
 * - A trailing `.git` and trailing slashes are stripped.
 * - When only one path segment is parseable, that segment is returned as-is.
 * - When nothing parseable remains, the trimmed input is returned unchanged.
 */
export function repoGroupLabel(repoUrl: string): string {
  const trimmed = repoUrl.trim();
  if (!trimmed) return NO_REPO_LABEL;

  // Normalize SSH form (`git@host:owner/repo`) to a path, and drop an HTTPS
  // scheme so `://` does not produce empty segments. Parse defensively with
  // string ops rather than `new URL()`, which throws on the SSH form. We take
  // the last two path segments, so any leading host (`github.com`) falls off
  // naturally — no explicit host-stripping needed.
  const sshMatch = /^[^@/]+@[^:/]+:(.+)$/.exec(trimmed);
  const path = sshMatch
    ? sshMatch[1]
    : trimmed.replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, "");

  const segments = path
    .replace(/\.git$/i, "")
    .split("/")
    .map((s) => s.trim())
    .filter(Boolean);

  if (segments.length === 0) return trimmed;
  if (segments.length === 1) return segments[0];
  return segments.slice(-2).join("/");
}
