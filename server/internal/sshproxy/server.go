package sshproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// Server is an SSH proxy listener that routes connections into Docker
// containers via `docker exec`. It is internal to Deuce: it calls
// db.Queries and workspace.Manager directly. See server/internal/sshproxy
// package docs for the full design.
type Server struct {
	cfg        Config
	signer     ssh.Signer
	queries    *db.Queries
	workspaces *workspace.Manager

	// resolveContainerHook lets tests inject a deterministic container
	// resolver without spinning up Docker or the workspace.Manager. The
	// production path falls back to a queries+workspaces lookup; tests
	// can swap this for a stub. Always nil in production builds.
	resolveContainerHook func(ctx context.Context, sessionID string) (string, error)

	// dockerBin is the docker CLI binary used by session-channel handlers.
	// Empty means defaultDockerBin ("docker"). Tests override this to
	// point at a fake helper script. Per-Server to avoid races between
	// concurrent test instances.
	dockerBin string

	mu            sync.Mutex
	listener      net.Listener
	closing       bool // set under mu by Shutdown to gate wg.Add in the accept loop (U9)
	inFlightPerIP map[string]int

	// activeConns tracks every authenticated SSH connection, keyed by a
	// per-conn UUID generated at handshake-success time. The value is
	// the connection's ssh.Permissions.Extensions snapshot, which carries
	// session-id / user-id / key-id / fp. sync.Map keeps reads
	// (ActiveSessionCount, future drain hooks) off the main mutex.
	//
	// IMPORTANT: only authenticated connections live here. Failed-auth
	// connections never make it past the handshake and so never insert.
	activeConns sync.Map // map[uuid.UUID]map[string]string

	// metrics is the package-private metrics surface. Counters use
	// sync/atomic so the hot path stays lock-free.
	metrics *Metrics

	wg   sync.WaitGroup
	done chan struct{}
}

// New constructs an SSH proxy server, loading or generating the host key
// and validating config. Returns an error if the config is invalid or
// host-key load/generate fails. The returned Server is not yet listening
// — call ListenAndServe to start.
func New(cfg Config, queries *db.Queries, workspaces *workspace.Manager) (*Server, error) {
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	signer, err := loadOrGenerateHostKey(cfg.HostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	return &Server{
		cfg:           cfg,
		signer:        signer,
		queries:       queries,
		workspaces:    workspaces,
		inFlightPerIP: make(map[string]int),
		metrics:       newMetrics(),
		done:          make(chan struct{}),
	}, nil
}

// HostKeyFingerprint returns the SHA256 fingerprint of the server's
// host key, suitable for displaying to operators (e.g., for known_hosts
// distribution).
func (s *Server) HostKeyFingerprint() string {
	return ssh.FingerprintSHA256(s.signer.PublicKey())
}

// ListenAndServe binds to the configured address and serves SSH
// connections until the listener is closed (e.g., via Shutdown).
// Returns net.ErrClosed when the listener is closed externally.
//
// The accept loop applies a per-source-IP cap on concurrent in-progress
// handshakes (default 8) and a pre-handshake deadline (default 10s) on
// the raw net.Conn before invoking ssh.NewServerConn. A panic in the
// per-connection handler is recovered and logged; the listener survives.
func (s *Server) ListenAndServe(addr string) error {
	defer recoverPanic("ssh accept loop")

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	slog.Info("ssh proxy listening", "addr", addr, "host_key_fp", s.HostKeyFingerprint())

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				slog.Info("ssh listener closed")
				return nil
			}
			slog.Warn("ssh accept failed", "error", err)
			continue
		}

		// Overload protection BEFORE per-IP admit and BEFORE entering
		// the handshake. runtime.NumGoroutine() is cheap; once it
		// crosses cfg.GoroutineCap, refuse new connections without
		// spawning the handler goroutine. The refusal is a synchronous
		// close + a "server overloaded" line written to the wire so a
		// human dialing in sees feedback (real SSH clients won't see
		// it because they expect the server banner first, but it
		// distinguishes the failure mode in tcpdump and `nc`).
		if runtime.NumGoroutine() > s.cfg.GoroutineCap {
			slog.Warn("ssh accept: goroutine cap reached",
				"goroutines", runtime.NumGoroutine(),
				"cap", s.cfg.GoroutineCap,
				"remote", conn.RemoteAddr().String(),
			)
			s.metrics.incAcceptOverloaded()
			_, _ = conn.Write([]byte("server overloaded\r\n"))
			_ = conn.Close()
			continue
		}

		if !s.admitNewConnection(conn) {
			_ = conn.Close()
			continue
		}

		// Guard wg.Add against a concurrent Shutdown calling wg.Wait.
		// Without this the race detector flags Add ↔ Wait under -race
		// even when the counter is provably non-zero. U10 will replace
		// this with proper accept-loop tracking.
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			// Release the in-flight slot we just took, then bail.
			s.releaseInFlight(conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.handleRawConn(conn)
	}
}

// admitNewConnection enforces the per-source-IP handshake cap. Returns
// false if the cap is exceeded; the caller must close the conn.
func (s *Server) admitNewConnection(c net.Conn) bool {
	ip, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		// Unparseable remote address — be conservative and admit, but log.
		slog.Warn("could not parse remote addr", "addr", c.RemoteAddr().String())
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlightPerIP[ip] >= s.cfg.MaxHandshakesPerIP {
		slog.Warn("rejecting connection: too many in-flight handshakes from IP",
			"ip", ip, "limit", s.cfg.MaxHandshakesPerIP)
		return false
	}
	s.inFlightPerIP[ip]++
	return true
}

// releaseInFlight decrements the in-flight counter for the given IP.
// Safe to call even if the IP was not previously counted.
func (s *Server) releaseInFlight(addr net.Addr) {
	ip, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.inFlightPerIP[ip]; ok {
		if n <= 1 {
			delete(s.inFlightPerIP, ip)
		} else {
			s.inFlightPerIP[ip] = n - 1
		}
	}
}

// handleRawConn runs the SSH handshake under a deadline, then hands off
// to the channel-handler. A panic anywhere in this goroutine is caught
// and logged; it never propagates to the listener.
func (s *Server) handleRawConn(c net.Conn) {
	defer s.wg.Done()
	defer recoverPanic("ssh connection handler")
	defer s.releaseInFlight(c.RemoteAddr())
	defer c.Close()

	if err := c.SetDeadline(time.Now().Add(s.cfg.HandshakeTimeout)); err != nil {
		slog.Warn("set handshake deadline failed", "error", err)
		return
	}

	cfg := s.serverConfig()
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		// Auth failures and pre-handshake timeouts land here. AuthLogCallback
		// (set by U7) logs the auth-attempt details; this branch is the
		// terminal log line.
		slog.Debug("ssh handshake failed", "remote", c.RemoteAddr().String(), "error", err)
		s.metrics.incConnectionFail()
		return
	}

	// Clear the handshake deadline so the established connection can be
	// long-lived (VS Code Remote-SSH sessions run for hours).
	if err := c.SetDeadline(time.Time{}); err != nil {
		slog.Warn("clear handshake deadline failed", "error", err)
	}

	// Register in activeConns under a per-conn UUID. Storing
	// Extensions (and not the underlying ssh.Conn) keeps the value
	// small and makes future drain-before-destroy hooks match purely
	// by session-id. Increment the connection / sessions_active
	// counters here so they only count fully-authenticated conns.
	connID := uuid.New()
	exts := copyExtensions(sshConn.Permissions)
	s.activeConns.Store(connID, exts)
	s.metrics.incConnectionOK()
	s.metrics.incSessionsActive()
	defer func() {
		s.activeConns.Delete(connID)
		s.metrics.decSessionsActive()
	}()

	// U8: hand off to the authenticated-connection handler. It enforces
	// the channel-type allowlist, per-connection channel caps, and
	// spawns one goroutine per accepted session channel.
	s.handleAuthenticatedConn(sshConn, chans, reqs)
}

// copyExtensions returns a shallow copy of perms.Extensions so the
// value stored in activeConns is independent of the live ssh.Permissions
// map. Returns nil if perms or its Extensions field is nil — callers
// must tolerate that (real handshake success always populates it via
// publicKeyCallback, but defensive copies make the unit tests cleaner).
func copyExtensions(perms *ssh.Permissions) map[string]string {
	if perms == nil || perms.Extensions == nil {
		return nil
	}
	out := make(map[string]string, len(perms.Extensions))
	for k, v := range perms.Extensions {
		out[k] = v
	}
	return out
}

// ActiveSessionCount returns the number of authenticated SSH
// connections currently bound to the given session ID. Walks
// activeConns under sync.Map's lockless read path; safe to call from
// any goroutine. Returns 0 when no connections target the session
// (or when sessionID is the zero UUID).
//
// Intended for future drain-before-destroy hooks in the session
// teardown path. The count is a point-in-time read; concurrent
// connect / disconnect events may shift the result by the time the
// caller acts on it.
func (s *Server) ActiveSessionCount(sessionID uuid.UUID) int {
	want := sessionID.String()
	n := 0
	s.activeConns.Range(func(_, v any) bool {
		exts, ok := v.(map[string]string)
		if !ok {
			return true
		}
		if exts[extSessionID] == want {
			n++
		}
		return true
	})
	return n
}

// Metrics returns a point-in-time snapshot of the proxy's counters.
// Safe to call concurrently; the returned MetricsSnapshot is a value
// copy (mutations don't affect the live counters). The GoroutinesSSH
// gauge is sampled from runtime.NumGoroutine() at snapshot time.
func (s *Server) Metrics() MetricsSnapshot {
	return s.metrics.snapshot()
}

// serverConfig builds the per-connection ssh.ServerConfig. Wires the
// session-member-scoped publicKeyCallback (U7) and the structured-log
// AuthLogCallback. Password / keyboard-interactive / GSSAPI auth methods
// are NOT installed — omission is the off-switch in crypto/ssh.
func (s *Server) serverConfig() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		ServerVersion: s.cfg.ServerVersion,
		// 10 accommodates typical agents (5–10 keys) without the user
		// having to pin IdentityFile in ~/.ssh/config. Still bounds the
		// post-revoke window to one connection's worth of rejected
		// handshakes, which is the only security-relevant property of
		// this knob (revocation propagates on the NEXT new connection;
		// existing sessions persist regardless of this value).
		MaxAuthTries:      10,
		PublicKeyCallback: s.publicKeyCallback,
		AuthLogCallback:   s.authLogCallback,
		BannerCallback: func(conn ssh.ConnMetadata) string {
			return "Deuce session proxy. Connections logged.\n"
		},
	}
	cfg.Config.KeyExchanges = []string{"curve25519-sha256", "curve25519-sha256@libssh.org"}
	cfg.Config.Ciphers = []string{"chacha20-poly1305@openssh.com", "aes256-gcm@openssh.com"}
	cfg.Config.MACs = []string{"hmac-sha2-256-etm@openssh.com", "hmac-sha2-512-etm@openssh.com"}
	cfg.AddHostKey(s.signer)
	return cfg
}

// Shutdown closes the listener and waits for in-flight connections to
// drain. Returns when all connections are closed OR when ctx is done.
// Safe to call multiple times.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.listener = nil
	// Setting closing=true under mu happens-before any future Add in
	// the accept loop (which also takes mu), so wg.Wait below cannot
	// race with Add on a new connection.
	s.closing = true
	s.mu.Unlock()

	if ln != nil {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Warn("ssh listener close failed", "error", err)
		}
	}

	allDone := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(allDone)
	}()

	select {
	case <-allDone:
		slog.Info("ssh proxy drained cleanly")
		return nil
	case <-ctx.Done():
		slog.Warn("ssh proxy shutdown timed out", "active_conns_pending", true)
		return ctx.Err()
	}
}

// recoverPanic is the common boundary used by every long-lived goroutine
// in the package. Recovered panics are logged with the supplied scope
// label so operators can see which subsystem misbehaved.
func recoverPanic(scope string) {
	if r := recover(); r != nil {
		slog.Error("ssh_panic", "scope", scope, "panic", fmt.Sprintf("%v", r))
	}
}
