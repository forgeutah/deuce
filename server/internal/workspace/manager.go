package workspace

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrContainerNotRunning is returned by ContainerName when no Docker
// container matches the given DevPod workspace ID.
var ErrContainerNotRunning = errors.New("container not running for workspace")

// ErrInvalidContainerName is returned by ContainerName when the resolved
// container name fails the Docker naming-rules regex. Defense-in-depth
// against a future Docker output-format change or label-injection attack
// producing an argv that would otherwise be passed to `docker exec`.
var ErrInvalidContainerName = errors.New("docker returned invalid container name")

// validContainerName matches Docker's own container-name rules:
// first char [a-zA-Z0-9], then up to 254 of [a-zA-Z0-9_.-].
var validContainerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

// LogFunc receives each line of output from a DevPod command.
type LogFunc func(line string)

// commandRunner is the seam BulkContainerStatus uses to invoke `docker ps`.
// Production wires it to exec.CommandContext + CombinedOutput; tests swap in
// a fake so they don't shell out. Mirrors the per-instance hook pattern at
// sshproxy.Server.resolveContainerHook.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ContainerState is the running/stopped distinction the reconciler needs.
// Containers that don't appear in `docker ps -a` are absent and not part of
// the returned map — callers detect absence by missing-key lookup.
type ContainerState string

const (
	ContainerRunning ContainerState = "running"
	ContainerStopped ContainerState = "stopped"
)

type Manager struct {
	bin      string
	provider string
	runner   commandRunner
}

func NewManager(bin, provider string) *Manager {
	if bin == "" {
		bin = "devpod"
	}
	return &Manager{bin: bin, provider: provider, runner: defaultCommandRunner}
}

// Available checks if the devpod binary is installed and accessible.
func (m *Manager) Available() bool {
	_, err := exec.LookPath(m.bin)
	return err == nil
}

// EnsureDockerProvider checks if a docker provider exists and adds one if not.
func (m *Manager) EnsureDockerProvider(ctx context.Context) error {
	// List existing providers
	cmd := exec.CommandContext(ctx, m.bin, "provider", "list", "--output", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("failed to list devpod providers", "error", err)
		// Fall through to try adding anyway
	} else if strings.Contains(string(output), "docker") {
		slog.Info("docker provider already exists")
		return nil
	}

	// Add docker provider
	slog.Info("adding docker provider to devpod")
	addCmd := exec.CommandContext(ctx, m.bin, "provider", "add", "docker")
	addOutput, err := addCmd.CombinedOutput()
	if err != nil {
		// May already exist — check if the error says so
		if strings.Contains(string(addOutput), "already exists") {
			slog.Info("docker provider already exists")
			return nil
		}
		return fmt.Errorf("failed to add docker provider: %w: %s", err, string(addOutput))
	}

	slog.Info("docker provider added successfully")

	// Set as default if no provider is configured
	if m.provider == "" {
		m.provider = "docker"
	}

	return nil
}

// Create starts a new DevPod workspace from a git repo URL.
// Output is streamed line-by-line to logFn (if non-nil).
// This blocks until the workspace is ready or fails.
func (m *Manager) Create(ctx context.Context, workspaceID, repoURL string, logFn LogFunc) error {
	args := []string{"up", repoURL, "--id", workspaceID, "--ide", "none"}
	if m.provider != "" {
		args = append(args, "--provider", m.provider)
	}

	slog.Info("starting devpod workspace", "id", workspaceID, "repo", repoURL)
	cmd := exec.CommandContext(ctx, m.bin, args...)

	// Merge stderr into stdout so we capture everything
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("devpod start: %w", err)
	}

	// Stream output line by line
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // handle long lines
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("devpod output", "id", workspaceID, "line", line)
		if logFn != nil {
			logFn(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("ERROR: devpod up failed: %v", err))
		}
		return fmt.Errorf("devpod up failed: %w", err)
	}

	slog.Info("devpod workspace ready", "id", workspaceID)
	return nil
}

// Stop halts a running workspace (can be resumed later).
func (m *Manager) Stop(ctx context.Context, workspaceID string) error {
	cmd := exec.CommandContext(ctx, m.bin, "stop", workspaceID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("devpod stop failed: %w: %s", err, string(output))
	}
	return nil
}

// Delete permanently removes a workspace.
func (m *Manager) Delete(ctx context.Context, workspaceID string) error {
	cmd := exec.CommandContext(ctx, m.bin, "delete", workspaceID, "--force", "--ignore-not-found")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("devpod delete failed: %w: %s", err, string(output))
	}
	return nil
}

// Status returns the workspace status ("Running", "Stopped", "NotFound", etc.)
func (m *Manager) Status(ctx context.Context, workspaceID string) (string, error) {
	cmd := exec.CommandContext(ctx, m.bin, "status", workspaceID, "--timeout", "10s")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "NotFound", nil
	}
	return strings.TrimSpace(string(output)), nil
}

// Exists checks if a workspace with the given ID already exists.
func (m *Manager) Exists(ctx context.Context, workspaceID string) bool {
	status, _ := m.Status(ctx, workspaceID)
	return status != "NotFound"
}

// SSHCommand returns an unstarted *exec.Cmd for `devpod ssh <workspaceID>`.
// The caller is responsible for attaching a PTY and starting the command.
func (m *Manager) SSHCommand(ctx context.Context, workspaceID string) *exec.Cmd {
	return exec.CommandContext(ctx, m.bin, "ssh", workspaceID)
}

// ExecInWorkspace returns an unstarted *exec.Cmd that runs a command inside
// the DevPod workspace via `devpod ssh --command "..."`.
// Unlike SSHCommand, this is non-interactive and leaves stdin free for piping.
// Use envVars to pass environment variables into the container via --set-env.
func (m *Manager) ExecInWorkspace(ctx context.Context, workspaceID, command string, envVars ...string) *exec.Cmd {
	args := []string{"ssh", workspaceID, "--command", command}
	for _, env := range envVars {
		args = append(args, "--set-env", env)
	}
	return exec.CommandContext(ctx, m.bin, args...)
}

// ContainerName resolves a DevPod workspace ID to the running Docker
// container's name, suitable for use with `docker exec`. Returns
// ErrContainerNotRunning when no container matches, ErrInvalidContainerName
// when Docker returns something that fails the name-validation regex.
//
// Only safe for the docker DevPod provider; remote providers (k8s, AWS,
// SSH) put the container on a different host where `docker exec` doesn't
// reach. The caller is responsible for the provider check.
//
// Lookup strategy: DevPod's docker provider does NOT set a
// `devpod.workspace=<id>` label of its own. Containers are created by
// the embedded devcontainer CLI, which labels them with
// `dev.containers.id=<context>-<truncated-id>-<hash>`. The same value
// is persisted by DevPod as the `uid` field of
// ~/.devpod/contexts/default/workspaces/<id>/workspace.json. We read
// that file to get the uid, then filter docker ps by the matching
// `dev.containers.id` label.
//
// Context is hardcoded to "default" — Deuce never invokes `devpod` with
// `--context`, so workspaces always land there. Revisit if multi-context
// support is ever introduced.
func (m *Manager) ContainerName(ctx context.Context, workspaceID string) (string, error) {
	uid, err := readWorkspaceUID(workspaceID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No on-disk state means DevPod never finished creating
			// this workspace (or it's been destroyed). Either way, no
			// running container exists.
			return "", ErrContainerNotRunning
		}
		return "", fmt.Errorf("read workspace uid: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "label=dev.containers.id="+uid,
		"--format", "{{.Names}}",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker ps failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", ErrContainerNotRunning
	}
	name := strings.TrimSpace(lines[0])

	if !validContainerName.MatchString(name) {
		slog.Warn("docker ps returned name failing validation", "workspace", workspaceID, "name", name)
		return "", ErrInvalidContainerName
	}

	if len(lines) > 1 {
		slog.Warn("multiple containers match workspace label", "workspace", workspaceID, "count", len(lines))
	}

	return name, nil
}

// WorkspaceUID exposes the DevPod uid for workspaceID to callers outside
// this package (specifically the reconciler, which cross-references uids
// against `docker ps` output). The exists return distinguishes "no on-disk
// state" — meaning DevPod has no record of this workspace — from genuine
// read/parse errors. Tests that need to fake this should mock the
// reconciler's workspaceUIDReader interface, not this method.
func (m *Manager) WorkspaceUID(workspaceID string) (uid string, exists bool, err error) {
	uid, err = readWorkspaceUID(workspaceID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return uid, true, nil
}

// BulkContainerStatus runs a single `docker ps -a` to collect the state of
// every DevPod-managed container on the host, keyed by the `dev.containers.id`
// label value. The reconciler maps each session's workspace uid (from
// WorkspaceUID) into this map to derive truth state.
//
// Containers not in the returned map are absent from docker entirely. The
// reconciler treats absent + on-disk-metadata as `stopped` (devpod knows
// about it, will restart on `up`) and absent + no-metadata as `missing`.
//
// Errors from docker are returned to the caller without partial-result
// fallback — the reconciler must skip the tick on docker daemon failures
// (per R6 in the brainstorm) rather than mass-flip rows to `missing`.
func (m *Manager) BulkContainerStatus(ctx context.Context) (map[string]ContainerState, error) {
	output, err := m.runner(ctx, "docker", "ps", "-a",
		"--filter", "label=dev.containers.id",
		"--format", `{{.Label "dev.containers.id"}}	{{.State}}`,
	)
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	result := make(map[string]ContainerState)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		uid, state, ok := strings.Cut(line, "\t")
		if !ok || uid == "" {
			slog.Warn("docker ps returned malformed line, skipping", "line", line)
			continue
		}
		if _, dup := result[uid]; dup {
			slog.Warn("multiple containers share dev.containers.id label, keeping last", "uid", uid)
		}
		// Docker's State field is one of: created, restarting, running, removing, paused, exited, dead.
		// Anything not "running" is collapsed to ContainerStopped — the reconciler only needs the
		// running/not-running distinction at the application layer.
		if state == "running" {
			result[uid] = ContainerRunning
		} else {
			result[uid] = ContainerStopped
		}
	}
	return result, nil
}

// readWorkspaceUID returns the DevPod-assigned uid for workspaceID by
// parsing ~/.devpod/contexts/default/workspaces/<id>/workspace.json.
// The uid is the value the embedded devcontainer CLI uses for the
// `dev.containers.id` label on the running container.
//
// Returns os.ErrNotExist (wrapped) when the workspace directory doesn't
// exist — distinguishable from JSON-parse errors so callers can map
// "never created" to ErrContainerNotRunning.
func readWorkspaceUID(workspaceID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	path := filepath.Join(home, ".devpod", "contexts", "default", "workspaces", workspaceID, "workspace.json")
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var ws struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(f).Decode(&ws); err != nil {
		return "", fmt.Errorf("decode workspace.json: %w", err)
	}
	if ws.UID == "" {
		return "", fmt.Errorf("workspace.json has no uid")
	}
	return ws.UID, nil
}

// ClaudePathPrefix is prepended to every command that invokes the `claude`
// binary inside a DevPod workspace. The native installer drops the binary
// at $HOME/.local/bin/claude and updates the user's shell profile — but
// `devpod ssh --command` runs a non-interactive shell that does not source
// .bashrc/.profile, so $HOME/.local/bin is not on PATH by default.
const ClaudePathPrefix = `PATH="$HOME/.local/bin:$PATH" `

// InstallTools installs Claude Code inside the DevPod workspace using the
// native installer (https://claude.ai/install.sh) — single curl-piped
// script, no Node/npm needed. Output is streamed line-by-line to logFn
// (if non-nil).
func (m *Manager) InstallTools(ctx context.Context, workspaceID string, logFn LogFunc) error {
	if logFn != nil {
		logFn("Checking for Claude Code installation...")
	}

	// Check if claude is already installed
	checkCmd := m.ExecInWorkspace(ctx, workspaceID, ClaudePathPrefix+"claude --version")
	if output, err := checkCmd.CombinedOutput(); err == nil {
		version := strings.TrimSpace(string(output))
		slog.Info("claude code already installed", "workspace", workspaceID, "version", version)
		if logFn != nil {
			logFn(fmt.Sprintf("Claude Code already installed: %s", version))
		}
		return nil
	}

	// Install Claude Code
	if logFn != nil {
		logFn("Installing Claude Code...")
	}
	slog.Info("installing claude code in workspace", "workspace", workspaceID)

	installCmd := m.ExecInWorkspace(ctx, workspaceID, "curl -fsSL https://claude.ai/install.sh | bash")
	stdout, err := installCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	installCmd.Stderr = installCmd.Stdout

	if err := installCmd.Start(); err != nil {
		if logFn != nil {
			logFn("WARNING: Failed to install Claude Code (curl may not be available in this devcontainer)")
		}
		slog.Warn("failed to start claude code install", "workspace", workspaceID, "error", err)
		return nil // Non-fatal — workspace is still usable, just without agent support
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("claude install output", "workspace", workspaceID, "line", line)
		if logFn != nil {
			logFn(line)
		}
	}

	if err := installCmd.Wait(); err != nil {
		if logFn != nil {
			logFn("WARNING: Claude Code installation failed — agents will not be available")
		}
		slog.Warn("claude code install failed", "workspace", workspaceID, "error", err)
		return nil // Non-fatal
	}

	if logFn != nil {
		logFn("Claude Code installed successfully")
	}
	slog.Info("claude code installed successfully", "workspace", workspaceID)
	return nil
}

// InstallPi installs the Pi coding agent (pi.dev) inside the DevPod workspace
// using its standalone installer (https://pi.dev/install.sh) — a single
// curl-piped script, no Node/npm required, mirroring InstallTools. Pi is the
// agent harness in Topology A; the supervisor launches it as `pi --mode rpc`.
// Output is streamed line-by-line to logFn (if non-nil). Non-fatal: a workspace
// without Pi is still usable, just without agent support (surfaced to the
// session per R3 rather than failing the workspace).
func (m *Manager) InstallPi(ctx context.Context, workspaceID string, logFn LogFunc) error {
	if logFn != nil {
		logFn("Checking for Pi installation...")
	}

	// Already installed?
	checkCmd := m.ExecInWorkspace(ctx, workspaceID, ClaudePathPrefix+"pi --version")
	if output, err := checkCmd.CombinedOutput(); err == nil {
		version := strings.TrimSpace(string(output))
		slog.Info("pi already installed", "workspace", workspaceID, "version", version)
		if logFn != nil {
			logFn(fmt.Sprintf("Pi already installed: %s", version))
		}
		return nil
	}

	if logFn != nil {
		logFn("Installing Pi...")
	}
	slog.Info("installing pi in workspace", "workspace", workspaceID)

	installCmd := m.ExecInWorkspace(ctx, workspaceID, "curl -fsSL https://pi.dev/install.sh | sh")
	stdout, err := installCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	installCmd.Stderr = installCmd.Stdout

	if err := installCmd.Start(); err != nil {
		if logFn != nil {
			logFn("WARNING: Failed to install Pi (curl may not be available in this devcontainer)")
		}
		slog.Warn("failed to start pi install", "workspace", workspaceID, "error", err)
		return nil // Non-fatal
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("pi install output", "workspace", workspaceID, "line", line)
		if logFn != nil {
			logFn(line)
		}
	}

	if err := installCmd.Wait(); err != nil {
		if logFn != nil {
			logFn("WARNING: Pi installation failed — agents will not be available")
		}
		slog.Warn("pi install failed", "workspace", workspaceID, "error", err)
		return nil // Non-fatal
	}

	if logFn != nil {
		logFn("Pi installed successfully")
	}
	slog.Info("pi installed successfully", "workspace", workspaceID)
	return nil
}

// InstallPiExtension writes a Pi extension file to the container's auto-discovery
// path (~/.pi/agent/extensions/<filename>) so Pi loads it on launch. Content is
// base64-encoded over the wire to avoid any shell-quoting hazards. Non-fatal:
// without the extension, agents simply lose the ask-user (awaiting-input)
// capability rather than failing the workspace.
func (m *Manager) InstallPiExtension(ctx context.Context, workspaceID, filename, content string, logFn LogFunc) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf(
		`mkdir -p "$HOME/.pi/agent/extensions" && printf %%s '%s' | base64 -d > "$HOME/.pi/agent/extensions/%s"`,
		encoded, filename,
	)
	out, err := m.ExecInWorkspace(ctx, workspaceID, cmd).CombinedOutput()
	if err != nil {
		if logFn != nil {
			logFn("WARNING: failed to install Pi ask-user extension")
		}
		slog.Warn("pi extension install failed", "workspace", workspaceID, "error", err, "output", strings.TrimSpace(string(out)))
		return nil // Non-fatal
	}
	if logFn != nil {
		logFn(fmt.Sprintf("Pi extension installed: %s", filename))
	}
	slog.Info("pi extension installed", "workspace", workspaceID, "filename", filename)
	return nil
}
