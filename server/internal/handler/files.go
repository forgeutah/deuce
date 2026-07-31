package handler

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// File status codes returned to the frontend. Porcelain v1 has more states;
// these collapse to the four-letter UI vocabulary defined in the plan.
const (
	statusModified  = "M"
	statusUntracked = "U"
	statusAdded     = "A"
	statusDeleted   = "D"
)

// Walk prune list — directories never descended into during tree build or repo
// discovery. `.git` is in the list so its internals stay out of the tree, but
// repo discovery records the parent directory as a repo root before pruning.
var walkPruneDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".turbo":       {},
	"target":       {},
	"__pycache__":  {},
	".venv":        {},
}

// Filename / path-segment deny-list for the content endpoint. Walk endpoint is
// not affected — users see these in the tree but cannot fetch their contents.
var deniedFileNames = map[string]struct{}{
	".env":       {},
	"id_rsa":     {},
	"id_ed25519": {},
	"id_ecdsa":   {},
}

var deniedFileSuffixes = []string{".pem", ".key"}

var deniedPathSegments = map[string]struct{}{
	".ssh": {},
	".git": {},
}

// inFlightWalks bounds concurrent walks per session — a second request for the
// same session returns 429 rather than spawning another git-status subprocess
// per repo.
var inFlightWalks sync.Map // map[uuid.UUID]struct{}

type fileNodeResponse struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Path       string             `json:"path"`
	Type       string             `json:"type"`
	Children   []fileNodeResponse `json:"children,omitempty"`
	GitStatus  string             `json:"gitStatus,omitempty"`
	IsRepoRoot bool               `json:"isRepoRoot,omitempty"`
}

type fileContentResponse struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	IsBinary  bool   `json:"isBinary"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
}

func workspaceContentPath(workspaceName string) string {
	base := os.Getenv("DEVPOD_AGENT_CONTENT_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".devpod", "agent", "contexts", "default", "workspaces")
	}
	return filepath.Join(base, workspaceName, "content")
}

func acquireWalkLock(sessionID uuid.UUID) (bool, func()) {
	if _, loaded := inFlightWalks.LoadOrStore(sessionID, struct{}{}); loaded {
		return false, func() {}
	}
	return true, func() { inFlightWalks.Delete(sessionID) }
}

// ListFiles returns the workspace file tree with per-file git status.
// Sub-repos discovered inside the workspace get their own git-status scope
// and are marked with isRepoRoot=true.
func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Read gate: the file tree is session content, readable by team members.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}
	if session.WorkspaceStatus != "ready" {
		writeError(w, http.StatusConflict, "WORKSPACE_NOT_READY", "workspace is not ready")
		return
	}

	ok, release := acquireWalkLock(sessionID)
	if !ok {
		writeError(w, http.StatusTooManyRequests, "WALK_IN_FLIGHT", "a files request is already in flight for this session")
		return
	}
	defer release()

	rootPath := workspaceContentPath(session.Name)
	info, statErr := os.Stat(rootPath)
	if statErr != nil || !info.IsDir() {
		writeError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace content not found on host filesystem")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	repoRoots, err := discoverRepoRoots(ctx, rootPath)
	if err != nil {
		slog.Error("file tree walk failed", "sessionID", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "WALK_FAILED", "failed to walk workspace")
		return
	}

	statusByPath := make(map[string]string)
	for _, repoRoot := range repoRoots {
		if ctx.Err() != nil {
			break
		}
		if err := loadGitStatus(ctx, rootPath, repoRoot, statusByPath); err != nil {
			// One repo's git failure does not fail the whole response.
			slog.Warn("git status failed for repo", "sessionID", sessionID, "repo", repoRoot, "error", err)
		}
	}

	repoRootSet := make(map[string]struct{}, len(repoRoots))
	for _, r := range repoRoots {
		repoRootSet[r] = struct{}{}
	}

	tree, err := readDirTree(ctx, rootPath, "", repoRootSet, statusByPath)
	if err != nil {
		slog.Error("file tree build failed", "sessionID", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "WALK_FAILED", "failed to build file tree")
		return
	}
	if tree == nil {
		tree = []fileNodeResponse{}
	}

	writeJSON(w, http.StatusOK, tree)
}

// GetFileContent returns a single file's content with binary detection and a
// 1MB cap. Rejects absolute paths, traversal segments, and a deny-list of
// well-known credential filenames. Symlinks are resolved before reading and
// the resolved path is re-checked against the workspace root.
func (h *Handler) GetFileContent(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Read gate: file content is session content, readable by team members.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	pathQuery := r.URL.Query().Get("path")
	if pathQuery == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "missing path")
		return
	}
	if filepath.IsAbs(pathQuery) || strings.Contains(pathQuery, "..") {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
		return
	}

	if pathDenied(pathQuery) {
		writeError(w, http.StatusForbidden, "PATH_DENIED", "path matches deny-list")
		return
	}

	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}
	if session.WorkspaceStatus != "ready" {
		writeError(w, http.StatusConflict, "WORKSPACE_NOT_READY", "workspace is not ready")
		return
	}

	rootPath := workspaceContentPath(session.Name)
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to resolve workspace path")
		return
	}
	rootAbs = filepath.Clean(rootAbs)

	joined := filepath.Clean(filepath.Join(rootAbs, pathQuery))
	if !pathWithinRoot(joined, rootAbs) {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
		return
	}

	// Symlink-resolved re-check so a planted symlink can't escape the root.
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "FILE_NOT_FOUND", "file not found")
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
		return
	}
	if !pathWithinRoot(resolved, rootAbs) {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "FILE_NOT_FOUND", "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to stat file")
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "PATH_IS_DIRECTORY", "path is a directory")
		return
	}

	const maxBytes = 1024 * 1024
	const sniffBytes = 8192

	file, err := os.Open(resolved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "failed to open file")
		return
	}
	defer file.Close()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "failed to read file")
		return
	}
	buf = buf[:n]
	truncated := info.Size() > int64(n)

	isBinary := false
	sniffEnd := n
	if sniffEnd > sniffBytes {
		sniffEnd = sniffBytes
	}
	for i := 0; i < sniffEnd; i++ {
		if buf[i] == 0 {
			isBinary = true
			break
		}
	}

	content := ""
	if !isBinary {
		content = string(buf)
	}

	writeJSON(w, http.StatusOK, fileContentResponse{
		Path:      pathQuery,
		Content:   content,
		IsBinary:  isBinary,
		Truncated: truncated,
		Size:      info.Size(),
	})
}

func pathWithinRoot(candidate, root string) bool {
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(os.PathSeparator))
}

func pathDenied(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if _, denied := deniedPathSegments[seg]; denied {
			return true
		}
	}
	base := filepath.Base(p)
	if _, denied := deniedFileNames[base]; denied {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, suffix := range deniedFileSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// discoverRepoRoots returns workspace-relative paths of every directory
// containing a .git subdirectory, including "" for the root workspace itself
// when applicable. Sub-repo roots are reported regardless of any parent's
// .gitignore — they are always-visible boundary markers.
func discoverRepoRoots(ctx context.Context, rootPath string) ([]string, error) {
	var roots []string

	// Root-as-repo check.
	if info, err := os.Stat(filepath.Join(rootPath, ".git")); err == nil && info.IsDir() {
		roots = append(roots, "")
	}

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return nil
		}
		if path == rootPath || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" {
			parent := filepath.Dir(path)
			rel, relErr := filepath.Rel(rootPath, parent)
			if relErr == nil && rel != "." {
				roots = append(roots, filepath.ToSlash(rel))
			}
			return filepath.SkipDir
		}
		if _, prune := walkPruneDirs[name]; prune {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return roots, nil
}

// gitStatusArgs is the argv for the workspace git-status probe.
//
// core.fsmonitor is pinned off because the repositories this runs against are
// not trusted. Git treats fsmonitor as a command to execute, and it honours it
// from the repository's own .git/config — so a value planted inside a workspace
// would run here, in the server process, rather than in the workspace container
// where whoever planted it already had execution. On a deployment that mounts
// the Docker socket that is a path from workspace to host root.
//
// Git's own defence against this is the dubious-ownership check, which fires
// because DevPod clones as the devcontainer's remoteUser while this process
// runs as its own uid. Deuce has to suppress that check to read workspaces at
// all (see the safe.directory line in the Dockerfile), which means suppressing
// the protection it was providing. Pinning the setting on the command line
// takes precedence over repository config and restores it.
//
// Confine additions here to flags that are inert or safe by construction; a
// command-line -c is the only layer standing between untrusted repository
// config and this process.
var gitStatusArgs = []string{
	"-c", "core.fsmonitor=false",
	"status", "--porcelain=v1", "--untracked-files=normal",
}

// loadGitStatus runs `git status --porcelain=v1` in the given repo root and
// records workspace-relative paths in statusByPath. Errors from one repo do
// not abort the whole walk.
func loadGitStatus(ctx context.Context, rootPath, repoRoot string, statusByPath map[string]string) error {
	absRepoPath := filepath.Join(rootPath, repoRoot)
	cmd := exec.CommandContext(ctx, "git", gitStatusArgs...)
	cmd.Dir = absRepoPath

	out, err := cmd.Output()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		rest := line[3:]
		relPath := parsePorcelainPath(rest)
		if relPath == "" {
			continue
		}
		status := mapPorcelainStatus(xy)
		if status == "" {
			continue
		}
		var key string
		if repoRoot == "" {
			key = relPath
		} else {
			key = filepath.ToSlash(filepath.Join(repoRoot, relPath))
		}
		statusByPath[key] = status
	}
	return scanner.Err()
}

// parsePorcelainPath handles renames ("ORIG -> NEW" — we keep NEW) and quoted
// paths (porcelain wraps paths in double quotes with C-style escapes when they
// contain special characters).
func parsePorcelainPath(rest string) string {
	if idx := strings.Index(rest, " -> "); idx >= 0 {
		rest = rest[idx+4:]
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "\"") && strings.HasSuffix(rest, "\"") && len(rest) >= 2 {
		if unquoted, err := strconv.Unquote(rest); err == nil {
			return unquoted
		}
	}
	return rest
}

// mapPorcelainStatus collapses porcelain v1's two-character status to the
// four-letter UI vocabulary. Precedence: untracked > rename/add > delete >
// modify/unmerged. Edge cases (MD, AD, RD, etc.) prefer the staged state for
// consistency — a more nuanced mapping is out of scope for v1.
func mapPorcelainStatus(xy string) string {
	if xy == "??" {
		return statusUntracked
	}
	x, y := xy[0], xy[1]
	if x == 'R' || y == 'R' {
		return statusAdded
	}
	if x == 'A' || y == 'A' {
		return statusAdded
	}
	if x == 'D' || y == 'D' {
		return statusDeleted
	}
	if x == 'M' || y == 'M' || x == 'U' || y == 'U' {
		return statusModified
	}
	return ""
}

func readDirTree(ctx context.Context, rootPath, relPath string, repoRoots map[string]struct{}, statusByPath map[string]string) ([]fileNodeResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	absDir := filepath.Join(rootPath, relPath)
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}

	nodes := make([]fileNodeResponse, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if _, prune := walkPruneDirs[name]; prune {
				continue
			}
		}

		childRel := filepath.ToSlash(filepath.Join(relPath, name))

		node := fileNodeResponse{
			ID:   childRel,
			Name: name,
			Path: childRel,
		}

		if e.IsDir() {
			node.Type = "directory"
			if _, isRepo := repoRoots[childRel]; isRepo {
				node.IsRepoRoot = true
			}
			children, err := readDirTree(ctx, rootPath, childRel, repoRoots, statusByPath)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				// One unreadable subdirectory does not fail the whole tree.
				continue
			}
			node.Children = children
		} else {
			node.Type = "file"
			if status, ok := statusByPath[childRel]; ok {
				node.GitStatus = status
			}
		}

		nodes = append(nodes, node)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if (a.Type == "directory") != (b.Type == "directory") {
			return a.Type == "directory"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return nodes, nil
}
