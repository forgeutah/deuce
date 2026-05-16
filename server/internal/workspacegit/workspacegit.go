// Package workspacegit runs git operations against a DevPod workspace's host
// bind-mount path. Used by the patch-emission flow to capture the workspace
// HEAD at the start of an agent turn and compute the diff at turn end.
//
// All git invocations run on the host with cmd.Dir set to the resolved
// workspace path (via the workspacepath package), not via `devpod ssh`. See
// docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md
// for the rationale.
package workspacegit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/forgeutah/deuce/server/internal/workspacepath"
)

// ErrWorkspaceNotFound is returned when the resolved workspace path does not
// exist on disk. Callers that surface this to HTTP should map it to 404.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// FileHunks groups the hunks for a single file in a diff.
type FileHunks struct {
	Path  string `json:"path"`
	Hunks []Hunk `json:"hunks"`
}

// Hunk is one contiguous range of changed lines within a file.
type Hunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// CaptureHead returns the workspace's current HEAD SHA. Used at the start of
// an agent turn so the diff at turn end can be computed against a stable
// anchor.
func CaptureHead(ctx context.Context, workspaceName string) (string, error) {
	dir := workspacepath.Resolve(workspaceName)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return "", ErrWorkspaceNotFound
		}
		return "", fmt.Errorf("stat workspace: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DiffSince computes the working-tree diff against the given SHA, returning
// the parsed hunks plus convenient counts. An empty diff returns an empty
// slice and zero counts — callers use this to decide whether to emit a patch.
func DiffSince(ctx context.Context, workspaceName, sha string) ([]FileHunks, int, int, error) {
	dir := workspacepath.Resolve(workspaceName)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, 0, 0, ErrWorkspaceNotFound
		}
		return nil, 0, 0, fmt.Errorf("stat workspace: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-color", "--unified=3", sha)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("git diff: %w", err)
	}
	files := parseUnifiedDiff(string(out))
	hunkCount := 0
	for _, f := range files {
		hunkCount += len(f.Hunks)
	}
	return files, len(files), hunkCount, nil
}

// parseUnifiedDiff parses the output of `git diff --no-color --unified=N`
// into a structured shape. Recognized headers: `diff --git a/X b/X`,
// `--- a/X` / `+++ b/X` (or `/dev/null` for create/delete), and
// `@@ -X,Y +A,B @@`. Content lines starting with ' ', '+', or '-' are
// collected verbatim into the current hunk. Anything else (e.g., `index`,
// `new file mode`, `similarity index`) is ignored.
func parseUnifiedDiff(diff string) []FileHunks {
	if diff == "" {
		return []FileHunks{}
	}
	var files []FileHunks
	var current *FileHunks
	var currentHunk *Hunk

	scanner := bufio.NewScanner(strings.NewReader(diff))
	// Generated diffs can have very long lines (e.g., minified JS); bump
	// the buffer well past the default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	flushHunk := func() {
		if current != nil && currentHunk != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if current != nil {
			files = append(files, *current)
			current = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			current = &FileHunks{Path: parseDiffGitPath(line)}
		case strings.HasPrefix(line, "--- "):
			// "--- /dev/null" means new file; the +++ line below carries the path.
			// Prefer the path from +++ when it's not /dev/null.
		case strings.HasPrefix(line, "+++ "):
			if current == nil {
				continue
			}
			path := strings.TrimPrefix(line, "+++ ")
			if path != "/dev/null" {
				current.Path = stripABPrefix(path)
			}
		case strings.HasPrefix(line, "@@ "):
			flushHunk()
			if current == nil {
				continue
			}
			h, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			currentHunk = &h
		default:
			if currentHunk == nil {
				continue
			}
			if len(line) == 0 {
				// Blank line inside a hunk represents an unchanged blank line.
				currentHunk.Lines = append(currentHunk.Lines, " ")
				continue
			}
			switch line[0] {
			case ' ', '+', '-':
				currentHunk.Lines = append(currentHunk.Lines, line)
			default:
				// Unknown line shape — likely a header we don't recognize.
				// Ignore to keep the parser tolerant of git output variations.
			}
		}
	}
	flushFile()

	if files == nil {
		return []FileHunks{}
	}
	return files
}

// parseDiffGitPath extracts the path from a `diff --git a/X b/X` line.
// Falls back to the b/ side when both are present; falls back to whatever
// follows the last space if the format is unexpected.
func parseDiffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	// Split on " b/" to find the second path. Renames have different a/b paths;
	// we use the b path because it represents the post-change file.
	if idx := strings.Index(rest, " b/"); idx >= 0 {
		return strings.TrimSpace(rest[idx+len(" b/"):])
	}
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return ""
	}
	return stripABPrefix(parts[len(parts)-1])
}

// stripABPrefix removes the leading "a/" or "b/" git adds to diff paths.
func stripABPrefix(p string) string {
	switch {
	case strings.HasPrefix(p, "a/"):
		return p[2:]
	case strings.HasPrefix(p, "b/"):
		return p[2:]
	default:
		return p
	}
}

// parseHunkHeader parses `@@ -oldStart,oldLines +newStart,newLines @@ optional`.
// Single-line hunks omit the comma: `@@ -10 +10 @@`. Defaults are 1 when omitted.
func parseHunkHeader(line string) (Hunk, bool) {
	// Strip leading "@@ " and trailing " @@..."
	rest := strings.TrimPrefix(line, "@@ ")
	closeIdx := strings.Index(rest, " @@")
	if closeIdx < 0 {
		return Hunk{}, false
	}
	rest = rest[:closeIdx]
	// rest is now "-X,Y +A,B" or "-X +A" (single-line forms omit the comma).
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return Hunk{}, false
	}
	oldStart, oldLines, ok := parseRange(parts[0], '-')
	if !ok {
		return Hunk{}, false
	}
	newStart, newLines, ok := parseRange(parts[1], '+')
	if !ok {
		return Hunk{}, false
	}
	return Hunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
	}, true
}

// parseRange parses "-10,3" or "+5" forms used inside hunk headers.
func parseRange(s string, sign byte) (int, int, bool) {
	if len(s) == 0 || s[0] != sign {
		return 0, 0, false
	}
	body := s[1:]
	if idx := strings.Index(body, ","); idx >= 0 {
		start, err1 := strconv.Atoi(body[:idx])
		count, err2 := strconv.Atoi(body[idx+1:])
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return start, count, true
	}
	start, err := strconv.Atoi(body)
	if err != nil {
		return 0, 0, false
	}
	return start, 1, true
}
