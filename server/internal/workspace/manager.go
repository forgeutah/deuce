package workspace

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// LogFunc receives each line of output from a DevPod command.
type LogFunc func(line string)

type Manager struct {
	bin      string
	provider string
}

func NewManager(bin, provider string) *Manager {
	if bin == "" {
		bin = "devpod"
	}
	return &Manager{bin: bin, provider: provider}
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

// InstallTools installs Claude Code inside the DevPod workspace.
// Output is streamed line-by-line to logFn (if non-nil).
func (m *Manager) InstallTools(ctx context.Context, workspaceID string, logFn LogFunc) error {
	if logFn != nil {
		logFn("Checking for Claude Code installation...")
	}

	// Check if claude is already installed
	checkCmd := m.ExecInWorkspace(ctx, workspaceID, "claude --version")
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

	installCmd := m.ExecInWorkspace(ctx, workspaceID, "npm install -g @anthropic-ai/claude-code")
	stdout, err := installCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	installCmd.Stderr = installCmd.Stdout

	if err := installCmd.Start(); err != nil {
		if logFn != nil {
			logFn("WARNING: Failed to install Claude Code (npm/node may not be available)")
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
