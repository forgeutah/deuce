package terminal

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty/v2"
)

// replayBufferSize caps the recent-output buffer replayed to new clients.
// 100 KiB is enough for a shell banner plus a few screens of scrollback while
// keeping memory per terminal session bounded.
const replayBufferSize = 100 * 1024

// Client receives PTY output. Replayed (historical) output is delivered
// separately from live output so the client can tell the two apart.
//
// This distinction is load-bearing, not cosmetic. The replay buffer stores
// raw PTY bytes, which may include terminal *queries* the remote shell
// emitted earlier (OSC 11 background-colour, DA, DSR, ...). A terminal
// emulator receiving those queries answers them, and the answer would be
// written back to PTY stdin — landing in the shell's line buffer as garbage.
// Framing replay distinctly lets the client suppress responses until the
// replay boundary has passed.
type Client interface {
	// Write delivers live PTY output.
	io.Writer
	// WriteReplay delivers buffered historical output.
	WriteReplay(p []byte) error
	// ReplayComplete signals that no further replayed output will arrive.
	ReplayComplete() error
}

// Session represents a single PTY session attached to a devpod ssh process.
type Session struct {
	ptmx *os.File
	cmd  *exec.Cmd

	mu      sync.Mutex
	clients map[Client]bool
	replay  []byte        // recent PTY output, replayed to new clients
	done    chan struct{} // closed when the reader goroutine exits
}

// Manager tracks one PTY session per Deuce session ID.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager creates a new terminal session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// GetOrCreate returns an existing terminal session or spawns a new one.
// cmdFactory is called only when creating a new session and should return
// an unstarted *exec.Cmd (e.g., from workspace.Manager.SSHCommand).
func (m *Manager) GetOrCreate(sessionID string, cmdFactory func() *exec.Cmd) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}

	cmd := cmdFactory()
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	s := &Session{
		ptmx:    ptmx,
		cmd:     cmd,
		clients: make(map[Client]bool),
		done:    make(chan struct{}),
	}
	m.sessions[sessionID] = s

	// Fan-out goroutine: reads PTY stdout and writes to all connected clients.
	go s.readLoop(sessionID)

	slog.Info("terminal session created", "sessionID", sessionID, "pid", cmd.Process.Pid)
	return s, nil
}

// readLoop reads from the PTY and fans out to all connected writers.
// It exits when the PTY is closed or the process exits.
func (s *Session) readLoop(sessionID string) {
	defer close(s.done)

	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			s.mu.Lock()
			s.appendReplay(data)
			for w := range s.clients {
				if _, werr := w.Write(data); werr != nil {
					// Mark broken client for removal — don't block others
					delete(s.clients, w)
					slog.Debug("removed broken terminal client", "sessionID", sessionID, "error", werr)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				slog.Debug("terminal PTY read ended", "sessionID", sessionID, "error", err)
			}
			return
		}
	}
}

// Write sends data to the PTY stdin.
func (s *Session) Write(data []byte) (int, error) {
	return s.ptmx.Write(data)
}

// Resize changes the PTY window size.
func (s *Session) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// appendReplay records recent PTY output for replay to new clients.
// Caller must hold s.mu.
func (s *Session) appendReplay(data []byte) {
	s.replay = append(s.replay, data...)
	if len(s.replay) > replayBufferSize {
		trimmed := make([]byte, replayBufferSize)
		copy(trimmed, s.replay[len(s.replay)-replayBufferSize:])
		s.replay = trimmed
	}
}

// AddClient registers a client to receive PTY output and replays the
// recent buffer so it doesn't land on a blank terminal.
//
// The replay write, the replay-complete marker, and the registration all
// happen under a single hold of the session lock. That ordering matters:
// if the marker were sent after the lock was released, live output could
// interleave ahead of it, and the client would suppress the response to a
// genuinely live query.
//
// ReplayComplete is sent unconditionally, including when the buffer is
// empty — a client that never receives it has no way to know replay is
// over, and would stay muted until its own fallback timer expires.
func (s *Session) AddClient(c Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.replay) > 0 {
		if err := c.WriteReplay(s.replay); err != nil {
			return
		}
	}
	if err := c.ReplayComplete(); err != nil {
		return
	}
	s.clients[c] = true
}

// RemoveClient unregisters a client from PTY output.
func (s *Session) RemoveClient(c Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

// Done returns a channel that is closed when the PTY process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Close kills the PTY process and cleans up.
func (m *Manager) Close(sessionID string) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	s.ptmx.Close()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	s.cmd.Wait()
	slog.Info("terminal session closed", "sessionID", sessionID)
}

// CloseAll shuts down all terminal sessions. Called during server shutdown.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Close(id)
	}
}
