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
