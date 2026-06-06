package pirun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Key identifies one persistent Pi process. There is exactly one Pi RPC process
// per (session, agent) — the granularity the steerable model requires (KTD5).
type Key struct {
	SessionID string
	AgentID   string
}

func (k Key) String() string { return k.SessionID + "/" + k.AgentID }

// Handle is one launched Pi process's I/O plus lifecycle control. The real
// implementation (devpodLauncher) wraps `devpod ssh --command "pi --mode rpc"`;
// tests inject an in-memory fake. Stop must signal the process group
// (SIGTERM→SIGKILL) and Wait must reap WITHOUT closing Stdout under the pump
// (the documented truncation bug, KTD4).
type Handle interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stop(ctx context.Context) error
	Wait() error
}

// stderrReporter is optionally implemented by a Handle to surface recent
// process stderr in launch/handshake failure messages.
type stderrReporter interface {
	Stderr() string
}

// Launcher starts a Pi RPC process for a workspace. It is the single shell-out
// seam, mirroring workspace.commandRunner / sshproxy.resolveContainerHook so
// the supervisor is testable without a container.
type Launcher interface {
	Launch(ctx context.Context, workspaceID string, env []string) (Handle, error)
}

// Exit is emitted on the supervisor's Exits channel when a process dies (clean
// EOF, crash, or Stop). The scheduler (U7) owns the resulting task transition —
// the supervisor never touches task rows or broadcasts (KTD12).
type Exit struct {
	Key Key
	Err error // nil on clean exit
}

const (
	defaultReadinessTimeout = 15 * time.Second
	// defaultIdleTimeout must exceed the runtime's longest task timeout (the
	// awaiting-input ceiling, 30m) so a process holding a live-but-silent task
	// (an unanswered question) is never reaped out from under it; genuinely idle
	// processes (no active task) are still freed.
	defaultIdleTimeout = 35 * time.Minute
	stopGrace          = 5 * time.Second
)

// ErrSupervisorClosed is returned by Ensure/Send after Shutdown.
var ErrSupervisorClosed = errors.New("pirun: supervisor closed")

// ErrNoProcess is returned by Send when no live process exists for the key.
var ErrNoProcess = errors.New("pirun: no live process for key")

// Process is a live Pi RPC session. Events() streams decoded Pi events until
// the process exits, at which point the channel is closed.
type Process struct {
	Key Key

	h      Handle
	events chan Event

	writeMu sync.Mutex // serialize stdin writes

	idleMu       sync.Mutex
	idleTimer    *time.Timer
	idleDuration time.Duration

	exitOnce sync.Once
}

// Events returns the decoded event stream. It is closed when the process exits.
func (p *Process) Events() <-chan Event { return p.events }

// Send marshals and writes a command to the process stdin as one JSONL line.
func (p *Process) Send(cmd Command) error {
	line, err := Marshal(cmd)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.resetIdle()
	if _, err := p.h.Stdin().Write(append(line, '\n')); err != nil {
		return fmt.Errorf("pirun: write command: %w", err)
	}
	return nil
}

func (p *Process) resetIdle() {
	p.idleMu.Lock()
	defer p.idleMu.Unlock()
	if p.idleTimer != nil && p.idleDuration > 0 {
		p.idleTimer.Reset(p.idleDuration)
	}
}

// Supervisor owns the lifecycle of all Pi processes. Modeled on sshproxy.Server:
// a base context cancelled on Shutdown, a WaitGroup draining the per-process
// pumps, and a registry keyed by (session, agent).
type Supervisor struct {
	launcher Launcher
	apiKey   string

	readinessTimeout time.Duration
	idleTimeout      time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	procs  map[Key]*Process
	closed bool

	exits chan Exit
	wg    sync.WaitGroup
}

// NewSupervisor creates a supervisor. apiKey is forwarded into each Pi process's
// container environment (never persisted to the container fs — KTD11/S4).
func NewSupervisor(launcher Launcher, apiKey string) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		launcher:         launcher,
		apiKey:           apiKey,
		readinessTimeout: defaultReadinessTimeout,
		idleTimeout:      defaultIdleTimeout,
		ctx:              ctx,
		cancel:           cancel,
		procs:            make(map[Key]*Process),
		exits:            make(chan Exit, 64),
	}
}

// Exits returns the channel of process-exit signals consumed by the scheduler.
func (s *Supervisor) Exits() <-chan Exit { return s.exits }

// Get returns the live process for key, if any.
func (s *Supervisor) Get(key Key) (*Process, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[key]
	return p, ok
}

// Ensure returns the live process for key, launching one if absent. On launch
// it performs a readiness handshake (get_state round-trip) before returning, so
// callers never race the devpod-ssh tunnel setup (the U1 transport caveat). When
// sessionPath is non-empty it re-attaches to that prior Pi session via
// switch_session (KTD13 continuity), tolerating failure.
func (s *Supervisor) Ensure(ctx context.Context, key Key, workspaceID, sessionPath string) (*Process, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSupervisorClosed
	}
	if p, ok := s.procs[key]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	env := []string{}
	if s.apiKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+s.apiKey)
	}
	h, err := s.launcher.Launch(s.ctx, workspaceID, env)
	if err != nil {
		return nil, fmt.Errorf("pirun: launch %s: %w", key, err)
	}

	p := &Process{
		Key:          key,
		h:            h,
		events:       make(chan Event, 256),
		idleDuration: s.idleTimeout,
	}
	if s.idleTimeout > 0 {
		p.idleTimer = time.AfterFunc(s.idleTimeout, func() {
			slog.Info("pirun: reaping idle process", "key", key.String())
			s.Stop(key)
		})
	}

	// Register before starting the pump so a fast exit still finds the entry.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = h.Stop(ctx)
		return nil, ErrSupervisorClosed
	}
	// Double-check no concurrent Ensure won the race.
	if existing, ok := s.procs[key]; ok {
		s.mu.Unlock()
		_ = h.Stop(ctx)
		return existing, nil
	}
	s.procs[key] = p
	s.wg.Add(1)
	s.mu.Unlock()

	go s.pump(p)

	if err := s.handshake(ctx, p); err != nil {
		detail := ""
		if rep, ok := h.(stderrReporter); ok {
			if se := rep.Stderr(); se != "" {
				detail = " (pi stderr: " + se + ")"
			}
		}
		s.Stop(key)
		return nil, fmt.Errorf("pirun: readiness handshake %s: %w%s", key, err, detail)
	}
	if sessionPath != "" {
		if err := p.Send(SwitchSession{SessionPath: sessionPath}); err != nil {
			slog.Warn("pirun: switch_session failed, continuing with fresh session", "key", key.String(), "error", err)
		}
	}
	return p, nil
}

// handshake sends get_state and waits for its reply (or timeout). No run events
// are emitted before the first prompt, so consuming events here loses nothing.
func (s *Supervisor) handshake(ctx context.Context, p *Process) error {
	if err := p.Send(GetState{}); err != nil {
		return err
	}
	deadline := time.NewTimer(s.readinessTimeout)
	defer deadline.Stop()
	for {
		select {
		case ev, ok := <-p.events:
			if !ok {
				return errors.New("process exited during handshake")
			}
			if ev.Kind == KindCommandReply && ev.Command == "get_state" {
				return nil
			}
		case <-deadline.C:
			return errors.New("timed out waiting for get_state reply")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// pump decodes the process stdout into its events channel until EOF, then reaps
// the process, closes the channel, removes the registry entry, and emits Exit.
// It never writes task state — that is the scheduler's job (KTD12).
func (s *Supervisor) pump(p *Process) {
	defer s.wg.Done()

	_ = DecodeStream(p.h.Stdout(), func(ev Event) {
		p.resetIdle()
		select {
		case p.events <- ev:
		case <-s.ctx.Done():
		}
	})
	// Stdout EOF means the process is exiting; reap it (Wait, not a pipe-closing
	// variant — the pump has already drained, KTD4) and signal exit exactly once.
	exitErr := p.h.Wait()
	p.exitOnce.Do(func() {
		p.stopIdle()
		close(p.events)
	})

	s.mu.Lock()
	if cur, ok := s.procs[p.Key]; ok && cur == p {
		delete(s.procs, p.Key)
	}
	closed := s.closed
	s.mu.Unlock()

	if !closed {
		select {
		case s.exits <- Exit{Key: p.Key, Err: exitErr}:
		case <-s.ctx.Done():
		}
	}
}

func (p *Process) stopIdle() {
	p.idleMu.Lock()
	defer p.idleMu.Unlock()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
}

// Send ensures a write to the live process for key. Returns ErrNoProcess if the
// key has no process (the caller falls back to enqueue, R19).
func (s *Supervisor) Send(key Key, cmd Command) error {
	p, ok := s.Get(key)
	if !ok {
		return ErrNoProcess
	}
	return p.Send(cmd)
}

// Stop terminates the process for key (SIGTERM→SIGKILL) and removes it. The
// pump's reap path emits the Exit signal.
func (s *Supervisor) Stop(key Key) {
	p, ok := s.Get(key)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopGrace+2*time.Second)
	defer cancel()
	_ = p.h.Stop(ctx)
}

// Shutdown stops every process and waits for the pumps to drain, bounded by ctx.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	procs := make([]*Process, 0, len(s.procs))
	for _, p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()

	for _, p := range procs {
		sctx, cancel := context.WithTimeout(ctx, stopGrace+2*time.Second)
		_ = p.h.Stop(sctx)
		cancel()
	}
	s.cancel() // unblock any pump sends parked on ctx

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		slog.Warn("pirun: supervisor shutdown timed out")
		return ctx.Err()
	}
}
