package workspace

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// validExecUser matches what `docker exec --user` accepts: a name or numeric
// id, optionally with a group. Deliberately strict — the value originates in
// a container label, which a session member controls through the
// devcontainer.json in their own repository. It reaches an argv slot rather
// than a shell, so this is defence in depth, not the only barrier; the
// leading-character rule is what stops a value from reading as a flag.
var validExecUser = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*(:[a-zA-Z0-9_][a-zA-Z0-9_.-]*)?$`)

// containerUserTTL bounds how long a resolved user is reused. The value can
// only change when the devcontainer is rebuilt, which yields a new container,
// but container names can be recycled — so the cache expires rather than
// living for the process lifetime.
const containerUserTTL = 5 * time.Minute

type containerUserEntry struct {
	user     string
	resolved time.Time
}

// ContainerUser resolves the user that `docker exec` should run as for the
// given container.
//
// This is not the same as the image's USER. Devcontainers routinely ship
// `USER root` and declare a separate `remoteUser` that the devcontainer
// tooling switches to; DevPod chowns the workspace tree to that user. A
// `docker exec` without --user therefore lands as root in a tree owned by
// someone else, and git refuses to operate on it:
//
//	fatal: detected dubious ownership in repository at '/workspaces/<repo>'
//
// Returns "" when no user is declared, which callers should treat as "leave
// the image's USER alone" rather than as an error.
//
// Note the `devpod.user` label is NOT this value — it reports root on images
// that declare a remoteUser, so using it would reproduce the bug above.
func (m *Manager) ContainerUser(ctx context.Context, container string) (string, error) {
	if !validContainerName.MatchString(container) {
		return "", ErrInvalidContainerName
	}

	m.userMu.Lock()
	entry, ok := m.userCache[container]
	m.userMu.Unlock()
	if ok && time.Since(entry.resolved) < containerUserTTL {
		return entry.user, nil
	}

	cmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", `{{index .Config.Labels "devcontainer.metadata"}}`,
		container,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	user := execUserFromMetadata(strings.TrimSpace(string(output)))

	m.userMu.Lock()
	if m.userCache == nil {
		m.userCache = make(map[string]containerUserEntry)
	}
	m.userCache[container] = containerUserEntry{user: user, resolved: time.Now()}
	m.userMu.Unlock()

	return user, nil
}

// execUserFromMetadata extracts the exec user from a `devcontainer.metadata`
// label. The label is the merged metadata array the devcontainer spec
// defines: later entries override earlier ones, and `remoteUser` outranks
// `containerUser`. Returns "" for absent, malformed, or implausible values —
// every failure degrades to "leave the image's USER alone".
func execUserFromMetadata(label string) string {
	if label == "" {
		return ""
	}

	var entries []struct {
		RemoteUser    *string `json:"remoteUser"`
		ContainerUser *string `json:"containerUser"`
	}
	if err := json.Unmarshal([]byte(label), &entries); err != nil {
		// Some tooling writes a bare object instead of the spec's array.
		var single struct {
			RemoteUser    *string `json:"remoteUser"`
			ContainerUser *string `json:"containerUser"`
		}
		if err := json.Unmarshal([]byte(label), &single); err != nil {
			slog.Debug("devcontainer.metadata is not valid JSON", "error", err)
			return ""
		}
		entries = append(entries, single)
	}

	var remote, container string
	for _, e := range entries {
		// An entry that omits the key must not clear a value an earlier
		// entry set, so only a present, non-empty string overrides.
		if e.RemoteUser != nil && *e.RemoteUser != "" {
			remote = *e.RemoteUser
		}
		if e.ContainerUser != nil && *e.ContainerUser != "" {
			container = *e.ContainerUser
		}
	}

	user := remote
	if user == "" {
		user = container
	}
	if user == "" {
		return ""
	}
	if !validExecUser.MatchString(user) {
		slog.Warn("devcontainer metadata declared an implausible exec user; ignoring", "user", user)
		return ""
	}
	return user
}
