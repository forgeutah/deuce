package sshproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
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

	mu             sync.Mutex
	listener       net.Listener
	inFlightPerIP  map[string]int

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

		if !s.admitNewConnection(conn) {
			_ = conn.Close()
			continue
		}

		s.wg.Add(1)
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
		return
	}

	// Clear the handshake deadline so the established connection can be
	// long-lived (VS Code Remote-SSH sessions run for hours).
	if err := c.SetDeadline(time.Time{}); err != nil {
		slog.Warn("clear handshake deadline failed", "error", err)
	}

	// U7/U8 wire up auth and channel handling. For U6 this is just a
	// no-op handshake-only path that discards channels and requests so
	// the connection drains cleanly.
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		// U6 stub: reject all channel types. U8 replaces this with the
		// session-channel allowlist.
		if err := newCh.Reject(ssh.UnknownChannelType, "session channel handling not yet implemented"); err != nil {
			slog.Debug("channel reject failed", "type", newCh.ChannelType(), "error", err)
		}
	}
	_ = sshConn.Close()
}

// serverConfig builds the per-connection ssh.ServerConfig. Wires the
// session-member-scoped publicKeyCallback (U7) and the structured-log
// AuthLogCallback. Password / keyboard-interactive / GSSAPI auth methods
// are NOT installed — omission is the off-switch in crypto/ssh.
func (s *Server) serverConfig() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		ServerVersion:     s.cfg.ServerVersion,
		MaxAuthTries:      3,
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
