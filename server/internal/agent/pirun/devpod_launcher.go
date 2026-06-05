package pirun

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/forgeutah/deuce/server/internal/workspace"
)

// DevpodLauncher launches Pi inside a session's DevPod container over
// `devpod ssh --command "pi --mode rpc"` (Topology A). It is the production
// Launcher; tests use an in-memory fake.
type DevpodLauncher struct {
	wm       *workspace.Manager
	provider string // default "anthropic"
	model    string // e.g. "claude-haiku-4-5"
}

// NewDevpodLauncher builds a launcher. provider/model select the Pi backend;
// v1 runs Claude models through Pi (per-agent model selection is deferred).
func NewDevpodLauncher(wm *workspace.Manager, provider, model string) *DevpodLauncher {
	if provider == "" {
		provider = "anthropic"
	}
	return &DevpodLauncher{wm: wm, provider: provider, model: model}
}

// Launch starts a long-lived `pi --mode rpc` process in the container. The
// command is prefixed with workspace.ClaudePathPrefix-style PATH handling so the
// pi binary on the user's local bin is found in a non-interactive shell.
func (l *DevpodLauncher) Launch(ctx context.Context, workspaceID string, env []string) (Handle, error) {
	args := []string{"pi", "--mode", "rpc", "--provider", l.provider}
	if l.model != "" {
		args = append(args, "--model", l.model)
	}
	piCmd := workspace.ClaudePathPrefix + join(args)

	cmd := l.wm.ExecInWorkspace(ctx, workspaceID, piCmd, env...)
	// Setpgid so Stop can signal the whole process group, reaching the devpod
	// ssh child and the in-container pi (mirrors sshproxy.session).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pi: %w", err)
	}

	// Drain stderr to the log so it never blocks the child and surfaces errors.
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			slog.Debug("pi stderr", "workspace", workspaceID, "line", sc.Text())
		}
	}()

	h := &devpodHandle{cmd: cmd, stdin: stdin, stdout: stdout, done: make(chan struct{})}
	return h, nil
}

type devpodHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	done     chan struct{}
	waitErr  error
	waitOnce sync.Once
}

func (h *devpodHandle) Stdin() io.WriteCloser { return h.stdin }
func (h *devpodHandle) Stdout() io.Reader     { return h.stdout }

// Wait reaps the process with cmd.Process.Wait() — NOT cmd.Wait() — so the
// stdout pipe is not closed under the pump mid-stream (the truncation bug fixed
// in sshproxy commit 0650ab6, KTD4). The pump calls Wait only after stdout EOFs.
func (h *devpodHandle) Wait() error {
	h.waitOnce.Do(func() {
		if h.cmd.Process != nil {
			state, err := h.cmd.Process.Wait()
			h.cmd.ProcessState = state
			if err != nil {
				h.waitErr = err
			} else if state != nil && !state.Success() {
				h.waitErr = fmt.Errorf("pi exited: %s", state.String())
			}
		}
		_ = h.stdout.Close()
		_ = h.stdin.Close()
		close(h.done)
	})
	<-h.done
	return h.waitErr
}

// Stop escalates SIGTERM → SIGKILL on the process group, then waits for the
// pump's Wait to complete (bounded by ctx).
func (h *devpodHandle) Stop(ctx context.Context) error {
	if h.cmd.Process == nil {
		return nil
	}
	signalGroup(h.cmd, syscall.SIGTERM)

	t := time.NewTimer(stopGrace)
	defer t.Stop()
	select {
	case <-h.done:
		return nil
	case <-t.C:
		signalGroup(h.cmd, syscall.SIGKILL)
	case <-ctx.Done():
		signalGroup(h.cmd, syscall.SIGKILL)
		return ctx.Err()
	}

	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil && pgid > 0 {
		_ = syscall.Kill(-pgid, sig)
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}

func join(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
