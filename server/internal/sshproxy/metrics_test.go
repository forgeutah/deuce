package sshproxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// TestActiveSessionCount_EmptyServer covers the zero case: a fresh
// Server with no inserts returns 0 for any session ID.
func TestActiveSessionCount_EmptyServer(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n := s.ActiveSessionCount(uuid.New()); n != 0 {
		t.Errorf("ActiveSessionCount on empty server: want 0, got %d", n)
	}
}

// TestActiveSessionCount_MatchesBySessionID inserts a mix of conn
// records directly into activeConns and asserts the count is the
// number whose ext session-id matches.
func TestActiveSessionCount_MatchesBySessionID(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	target := uuid.New()
	other := uuid.New()

	// 3 conns targeting `target`, 2 targeting `other`.
	for i := 0; i < 3; i++ {
		s.activeConns.Store(uuid.New(), map[string]string{
			extSessionID: target.String(),
			extUserID:    uuid.NewString(),
		})
	}
	for i := 0; i < 2; i++ {
		s.activeConns.Store(uuid.New(), map[string]string{
			extSessionID: other.String(),
			extUserID:    uuid.NewString(),
		})
	}
	// One entry with nil extensions — should be skipped.
	s.activeConns.Store(uuid.New(), map[string]string(nil))

	if got := s.ActiveSessionCount(target); got != 3 {
		t.Errorf("ActiveSessionCount(target): want 3, got %d", got)
	}
	if got := s.ActiveSessionCount(other); got != 2 {
		t.Errorf("ActiveSessionCount(other): want 2, got %d", got)
	}
	if got := s.ActiveSessionCount(uuid.New()); got != 0 {
		t.Errorf("ActiveSessionCount(unknown): want 0, got %d", got)
	}
}

// TestMetricsSnapshot_StartsZeroAndIncrements asserts the counter
// helpers behave like atomic increments and that snapshot() reflects
// the live state.
func TestMetricsSnapshot_StartsZeroAndIncrements(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	snap := s.Metrics()
	if snap.ConnectionsTotalOK != 0 || snap.ConnectionsTotalFail != 0 ||
		snap.SessionsActive != 0 || snap.ChannelsOpenSession != 0 ||
		snap.ChannelsOpenOther != 0 || snap.AuthAttemptsOK != 0 ||
		snap.AuthAttemptsFail != 0 || snap.AcceptOverloaded != 0 {
		t.Errorf("counters should start at 0, got %#v", snap)
	}
	if snap.GoroutinesSSH <= 0 {
		t.Errorf("GoroutinesSSH should reflect runtime.NumGoroutine() > 0, got %d", snap.GoroutinesSSH)
	}

	s.metrics.incConnectionOK()
	s.metrics.incConnectionOK()
	s.metrics.incConnectionFail()
	s.metrics.incSessionsActive()
	s.metrics.incChannelOpenSession()
	s.metrics.incChannelOpenOther()
	s.metrics.incAuthAttemptOK()
	s.metrics.incAuthAttemptFail()
	s.metrics.incAuthAttemptFail()
	s.metrics.incAcceptOverloaded()

	snap = s.Metrics()
	if snap.ConnectionsTotalOK != 2 {
		t.Errorf("ConnectionsTotalOK: want 2, got %d", snap.ConnectionsTotalOK)
	}
	if snap.ConnectionsTotalFail != 1 {
		t.Errorf("ConnectionsTotalFail: want 1, got %d", snap.ConnectionsTotalFail)
	}
	if snap.SessionsActive != 1 {
		t.Errorf("SessionsActive: want 1, got %d", snap.SessionsActive)
	}
	if snap.ChannelsOpenSession != 1 {
		t.Errorf("ChannelsOpenSession: want 1, got %d", snap.ChannelsOpenSession)
	}
	if snap.ChannelsOpenOther != 1 {
		t.Errorf("ChannelsOpenOther: want 1, got %d", snap.ChannelsOpenOther)
	}
	if snap.AuthAttemptsOK != 1 {
		t.Errorf("AuthAttemptsOK: want 1, got %d", snap.AuthAttemptsOK)
	}
	if snap.AuthAttemptsFail != 2 {
		t.Errorf("AuthAttemptsFail: want 2, got %d", snap.AuthAttemptsFail)
	}
	if snap.AcceptOverloaded != 1 {
		t.Errorf("AcceptOverloaded: want 1, got %d", snap.AcceptOverloaded)
	}

	s.metrics.decSessionsActive()
	if got := s.Metrics().SessionsActive; got != 0 {
		t.Errorf("SessionsActive after dec: want 0, got %d", got)
	}
}

// TestAuthLogCallback_BumpsCounters drives the callback directly with a
// synthetic ConnMetadata and verifies the counters move. This avoids
// having to spin up a real handshake just to exercise the metrics path.
func TestAuthLogCallback_BumpsCounters(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta := newMeta("dc-" + uuid.NewString())
	s.authLogCallback(meta, "publickey", nil)
	s.authLogCallback(meta, "publickey", errAuthFakeFail)
	s.authLogCallback(meta, "publickey", errAuthFakeFail)

	snap := s.Metrics()
	if snap.AuthAttemptsOK != 1 {
		t.Errorf("AuthAttemptsOK: want 1, got %d", snap.AuthAttemptsOK)
	}
	if snap.AuthAttemptsFail != 2 {
		t.Errorf("AuthAttemptsFail: want 2, got %d", snap.AuthAttemptsFail)
	}
}

// errAuthFakeFail is a sentinel "auth failed" error used only by tests.
var errAuthFakeFail = sentinelError("auth failed for test")

type sentinelError string

func (s sentinelError) Error() string { return string(s) }

// TestListenAndServe_OverloadDisconnects shrinks GoroutineCap to a
// value below the current runtime.NumGoroutine() and confirms the next
// accept disconnects without running the handshake — connections_total
// stays at 0, accept_overloaded increments by 1.
func TestListenAndServe_OverloadDisconnects(t *testing.T) {
	cfg := testConfig(t)
	// Set the cap to 1: runtime.NumGoroutine() is always > 1 during a
	// test run (at minimum: main + the accept loop), so every accept
	// trips the overload path.
	cfg.GoroutineCap = 1
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.ListenAndServe("127.0.0.1:0")
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		wg.Wait()
	}()

	addr := waitForListener(t, s)

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	// The server writes "server overloaded\r\n" then closes. We just
	// confirm the read returned something OR EOF — both are fine; the
	// load-bearing assertion is the counter below.
	_ = n

	// Give the accept loop a moment to record the rejection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Metrics().AcceptOverloaded >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := s.Metrics()
	if snap.AcceptOverloaded < 1 {
		t.Errorf("AcceptOverloaded: want >= 1, got %d", snap.AcceptOverloaded)
	}
	if snap.ConnectionsTotalOK != 0 {
		t.Errorf("ConnectionsTotalOK should stay 0 when overloaded, got %d", snap.ConnectionsTotalOK)
	}
}

// TestListenAndServe_BelowCapAcceptsNormally uses a generous cap and
// confirms that a connection that fails to speak SSH (handshake
// timeout) bumps connections_total{fail} — proving the overload check
// did NOT short-circuit.
func TestListenAndServe_BelowCapAcceptsNormally(t *testing.T) {
	cfg := testConfig(t)
	cfg.GoroutineCap = 100_000 // effectively unlimited
	cfg.HandshakeTimeout = 200 * time.Millisecond
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.ListenAndServe("127.0.0.1:0")
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		wg.Wait()
	}()

	addr := waitForListener(t, s)

	// Dial and stay silent. The handshake deadline will fire and the
	// connection lands in incConnectionFail().
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
	// Drain whatever the server sends (banner) so the handshake actually
	// starts.
	_, _ = c.Read(make([]byte, 256))

	// Wait for the failed-handshake counter to bump (post-deadline).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Metrics().ConnectionsTotalFail >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = c.Close()

	snap := s.Metrics()
	if snap.ConnectionsTotalFail < 1 {
		t.Errorf("ConnectionsTotalFail: want >= 1, got %d", snap.ConnectionsTotalFail)
	}
	if snap.AcceptOverloaded != 0 {
		t.Errorf("AcceptOverloaded should be 0 below cap, got %d", snap.AcceptOverloaded)
	}
}

// TestRealHandshake_IncrementsConnectionsOK drives a full SSH client/
// server handshake using a stubbed PublicKeyCallback (no DB needed),
// then closes the client. Asserts that connections_total{ok} and
// sessions_active have moved correctly, and ActiveSessionCount
// reflects the live connection while open.
func TestRealHandshake_IncrementsConnectionsOK(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	targetSession := uuid.New()

	// Hand-rolled accept loop that mirrors the production handleRawConn
	// path enough to exercise activeConns + metrics. We can't use
	// ListenAndServe directly because publicKeyCallback hits the DB;
	// the simpler path is to bind a listener, accept once, run
	// ssh.NewServerConn with a stub auth, and reproduce the bookkeeping.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	clientSigner := mustEd25519Signer(t)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		serverCfg := &ssh.ServerConfig{
			ServerVersion: s.cfg.ServerVersion,
			PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				return &ssh.Permissions{
					Extensions: map[string]string{
						extSessionID: targetSession.String(),
						extUserID:    uuid.NewString(),
						extKeyID:     uuid.NewString(),
						extFP:        ssh.FingerprintSHA256(key),
					},
				}, nil
			},
			AuthLogCallback: s.authLogCallback,
		}
		serverCfg.AddHostKey(s.signer)
		sshConn, chans, reqs, err := ssh.NewServerConn(c, serverCfg)
		if err != nil {
			s.metrics.incConnectionFail()
			return
		}
		// Same bookkeeping as handleRawConn.
		connID := uuid.New()
		s.activeConns.Store(connID, copyExtensions(sshConn.Permissions))
		s.metrics.incConnectionOK()
		s.metrics.incSessionsActive()

		// While the conn is live, ActiveSessionCount must see it.
		if got := s.ActiveSessionCount(targetSession); got != 1 {
			t.Errorf("ActiveSessionCount during open conn: want 1, got %d", got)
		}

		// Drain channels until the client closes.
		go ssh.DiscardRequests(reqs)
		for range chans {
		}
		_ = sshConn.Close()

		s.activeConns.Delete(connID)
		s.metrics.decSessionsActive()
	}()

	clientCfg := &ssh.ClientConfig{
		User:            "dc-" + uuid.NewString(),
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         3 * time.Second,
	}
	clientConn, err := ssh.Dial("tcp", ln.Addr().String(), clientCfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	// Give the server goroutine a moment to record the conn.
	time.Sleep(50 * time.Millisecond)

	// Now close — the server should drain and decrement counters.
	_ = clientConn.Close()
	<-serverDone

	snap := s.Metrics()
	if snap.ConnectionsTotalOK != 1 {
		t.Errorf("ConnectionsTotalOK: want 1, got %d", snap.ConnectionsTotalOK)
	}
	if snap.SessionsActive != 0 {
		t.Errorf("SessionsActive after close: want 0, got %d", snap.SessionsActive)
	}
	if snap.AuthAttemptsOK != 1 {
		t.Errorf("AuthAttemptsOK: want 1, got %d", snap.AuthAttemptsOK)
	}
	if got := s.ActiveSessionCount(targetSession); got != 0 {
		t.Errorf("ActiveSessionCount after close: want 0, got %d", got)
	}
}

// TestShutdown_DrainsActiveConnsBookkeeping confirms that after
// Shutdown returns, activeConns has no leftover entries from
// conn-handler goroutines (the defer in handleRawConn removes them).
// Counters survive Shutdown.
func TestShutdown_PreservesCountersAndClearsActive(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Pre-load some counters so we can prove they survive Shutdown.
	s.metrics.incConnectionOK()
	s.metrics.incConnectionFail()
	s.metrics.incAuthAttemptOK()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.ListenAndServe("127.0.0.1:0")
	}()
	_ = waitForListener(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	wg.Wait()

	// activeConns should be empty: no real handshakes happened.
	count := 0
	s.activeConns.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("activeConns should be empty after Shutdown, got %d entries", count)
	}

	snap := s.Metrics()
	if snap.ConnectionsTotalOK != 1 {
		t.Errorf("ConnectionsTotalOK should survive Shutdown: want 1, got %d", snap.ConnectionsTotalOK)
	}
	if snap.ConnectionsTotalFail != 1 {
		t.Errorf("ConnectionsTotalFail should survive Shutdown: want 1, got %d", snap.ConnectionsTotalFail)
	}
	if snap.AuthAttemptsOK != 1 {
		t.Errorf("AuthAttemptsOK should survive Shutdown: want 1, got %d", snap.AuthAttemptsOK)
	}
}

// mustEd25519Signer returns a fresh ssh.Signer for use by test clients.
func mustEd25519Signer(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 generate: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey: %v", err)
	}
	return signer
}
