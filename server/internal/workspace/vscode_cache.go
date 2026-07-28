package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// vscodeServerDirName is the directory VS Code Remote-SSH installs its server
// payload into, inside the container's home. Roughly 120MB, re-downloaded on
// every container recreate unless it is carried across.
const vscodeServerDirName = ".vscode-server"

// validWorkspaceID bounds what may become a path segment under the cache
// root. Workspace IDs are Deuce-generated, so this is defence in depth
// against a future ID scheme rather than untrusted input — but it is what
// stops "../.." from escaping the cache directory.
var validWorkspaceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

// validAbsPath rejects a home directory that does not look like one, so a
// surprising `getent` result cannot send `docker cp` somewhere unexpected.
var validAbsPath = regexp.MustCompile(`^/[a-zA-Z0-9_./-]*$`)

// ErrVSCodeCacheDisabled is returned when no cache directory is configured.
var ErrVSCodeCacheDisabled = errors.New("vscode-server cache directory not configured")

// SetVSCodeCacheDir turns on the ~/.vscode-server cache. Empty (the default)
// disables it, and every cache operation becomes a no-op.
func (m *Manager) SetVSCodeCacheDir(dir string) {
	m.vscodeCacheDir = dir
}

// VSCodeCacheEnabled reports whether a cache directory is configured.
func (m *Manager) VSCodeCacheEnabled() bool {
	return m.vscodeCacheDir != ""
}

// cachePathFor returns the host directory holding a workspace's cached
// ~/.vscode-server tree.
//
// The cache is keyed by workspace, not by user. A workspace's container is
// already shared by every member of its session, so a per-workspace cache
// adds no new reach. Keying it per user and restoring into a shared
// container would be a wider win but would copy one user's extension state
// — including whatever credentials their extensions have stored — into a
// container other session members hold a shell on.
func (m *Manager) cachePathFor(workspaceID string) (string, error) {
	if m.vscodeCacheDir == "" {
		return "", ErrVSCodeCacheDisabled
	}
	if !validWorkspaceID.MatchString(workspaceID) {
		return "", fmt.Errorf("workspace id %q is not usable as a cache path segment", workspaceID)
	}
	return filepath.Join(m.vscodeCacheDir, workspaceID), nil
}

// containerHome resolves the home directory of the user `docker exec` runs
// as, from the passwd database rather than $HOME — `docker exec` does not
// set $HOME, so reading it would yield root's home or nothing at all.
func (m *Manager) containerHome(ctx context.Context, container, user string) (string, error) {
	args := []string{"exec"}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, container, "sh", "-c", `getent passwd "$(id -un)" | cut -d: -f6`)

	out, err := m.runner(ctx, "docker", args...)
	if err != nil {
		return "", fmt.Errorf("resolve container home: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if home == "" || !validAbsPath.MatchString(home) {
		return "", fmt.Errorf("container returned an implausible home directory %q", home)
	}
	return home, nil
}

// resolveContainerTarget resolves the container name, its exec user and that
// user's home in one go — the three things every cache operation needs.
func (m *Manager) resolveContainerTarget(ctx context.Context, workspaceID string) (container, user, home string, err error) {
	if m.resolveTargetHook != nil {
		return m.resolveTargetHook(ctx, workspaceID)
	}
	container, err = m.ContainerName(ctx, workspaceID)
	if err != nil {
		return "", "", "", err
	}
	user, err = m.ContainerUser(ctx, container)
	if err != nil {
		// Not fatal on its own: an empty user means "leave the image's
		// USER alone", which is the same fallback docker exec already uses.
		slog.Debug("could not resolve container user for vscode cache", "workspace", workspaceID, "error", err)
		user = ""
	}
	home, err = m.containerHome(ctx, container, user)
	if err != nil {
		return "", "", "", err
	}
	return container, user, home, nil
}

// SaveVSCodeServer copies the container's ~/.vscode-server tree out to the
// host cache so the next container for this workspace can start from it.
//
// Call it before an action that destroys the container (stop, rebuild). A
// missing directory is not an error — it just means VS Code was never opened
// against this workspace.
func (m *Manager) SaveVSCodeServer(ctx context.Context, workspaceID string, logFn LogFunc) error {
	dst, err := m.cachePathFor(workspaceID)
	if err != nil {
		return err
	}

	// The user is only needed to resolve home; docker cp itself runs as root.
	container, _, home, err := m.resolveContainerTarget(ctx, workspaceID)
	if err != nil {
		return err
	}

	src := container + ":" + filepath.Join(home, vscodeServerDirName)

	// Stage into a sibling directory and swap, so an interrupted copy cannot
	// leave a half-written tree that a later restore would push into a
	// container as if it were complete.
	//
	// Two concurrent saves for the same workspace would contend over the
	// staging directory, but the worst case is that one of them fails and
	// logs: the promoted cache is only ever replaced by a completed rename,
	// never by a partial copy.
	staging := dst + ".partial"
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear staging dir: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	if out, err := m.runner(ctx, "docker", "cp", "-a", src, staging); err != nil {
		_ = os.RemoveAll(staging)
		// The overwhelmingly common case: VS Code was never opened against
		// this workspace, so there is simply nothing to cache. Treating it
		// as an error would warn on every stop of every such workspace.
		if isMissingContainerPath(string(out)) {
			slog.Debug("no vscode-server directory to cache", "workspace", workspaceID)
			return nil
		}
		return fmt.Errorf("docker cp out: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if err := os.RemoveAll(dst); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("clear cache dir: %w", err)
	}
	if err := os.Rename(staging, dst); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("promote staging dir: %w", err)
	}

	slog.Info("cached vscode-server payload", "workspace", workspaceID, "path", dst)
	if logFn != nil {
		logFn("Saved the VS Code server payload for reuse on the next container")
	}
	return nil
}

// isMissingContainerPath reports whether a `docker cp` failure was just an
// absent source path. Docker phrases this as
//
//	Error response from daemon: Could not find the file /home/vscode/.vscode-server in container <id>
//
// Note "Could not find" — matching on "not found" silently misses it, which
// is how this turned into a warning on every stop of a workspace that had
// never been opened in VS Code.
func isMissingContainerPath(dockerOutput string) bool {
	s := strings.ToLower(dockerOutput)
	return strings.Contains(s, "could not find the file") ||
		strings.Contains(s, "no such container:path") ||
		strings.Contains(s, "not found in container")
}

// RestoreVSCodeServer copies a previously cached ~/.vscode-server tree back
// into a freshly created container, so VS Code Remote-SSH finds its server
// already installed instead of downloading it again.
//
// A cache miss is not an error — the first session for a workspace has
// nothing to restore.
func (m *Manager) RestoreVSCodeServer(ctx context.Context, workspaceID string, logFn LogFunc) error {
	src, err := m.cachePathFor(workspaceID)
	if err != nil {
		return err
	}
	payload := filepath.Join(src, vscodeServerDirName)
	if _, statErr := os.Stat(payload); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat cached payload: %w", statErr)
	}

	container, user, home, err := m.resolveContainerTarget(ctx, workspaceID)
	if err != nil {
		return err
	}

	// Copy the directory itself into the home, reproducing the original
	// layout. -a preserves the uids recorded when it was copied out, which
	// are this same container user's.
	if out, err := m.runner(ctx, "docker", "cp", "-a", payload, container+":"+home); err != nil {
		return fmt.Errorf("docker cp in: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Belt and braces: if the image's uid numbering changed between the two
	// containers, the restored tree would land unwritable for the session
	// user and VS Code would fail in a confusing way rather than re-download.
	if user != "" {
		target := filepath.Join(home, vscodeServerDirName)
		if out, chErr := m.runner(ctx, "docker", "exec", "--user", "root", container,
			"chown", "-R", user+":"+user, target); chErr != nil {
			slog.Warn("could not chown restored vscode-server payload",
				"workspace", workspaceID, "error", chErr, "output", strings.TrimSpace(string(out)))
		}
	}

	slog.Info("restored vscode-server payload from cache", "workspace", workspaceID)
	if logFn != nil {
		logFn("Restored the cached VS Code server payload — no re-download needed")
	}
	return nil
}

// PurgeVSCodeServer removes a workspace's cached payload. Called when the
// workspace is deleted, so caches do not outlive what they belong to — the
// retention policy the plan's System-Wide Impact section asks for.
func (m *Manager) PurgeVSCodeServer(workspaceID string) error {
	dir, err := m.cachePathFor(workspaceID)
	if err != nil {
		if errors.Is(err, ErrVSCodeCacheDisabled) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("purge vscode-server cache: %w", err)
	}
	if err := os.RemoveAll(dir + ".partial"); err != nil {
		return fmt.Errorf("purge vscode-server staging: %w", err)
	}
	slog.Info("purged vscode-server cache", "workspace", workspaceID)
	return nil
}
