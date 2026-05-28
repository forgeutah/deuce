package sshproxy

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testConfig builds a Config pointed at a temporary host-key path so
// each test gets an isolated key file.
func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	return Config{
		HostKeyPath:        filepath.Join(dir, ".deuce", "ssh_host_ed25519_key"),
		ServerVersion:      "SSH-2.0-Deuce_test",
		HandshakeTimeout:   2 * time.Second,
		MaxHandshakesPerIP: 8,
		MaxChannelsPerConn: 8,
	}
}

func TestNew_GeneratesHostKey_Mode0600_ParentDir0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}

	// Loose umask so the explicit Chmod regression triggers.
	origUmask := syscall.Umask(0)
	defer syscall.Umask(origUmask)

	cfg := testConfig(t)
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	keyInfo, err := os.Stat(cfg.HostKeyPath)
	if err != nil {
		t.Fatalf("stat host key: %v", err)
	}
	if got := keyInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("host key file mode: want 0600, got %o", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(cfg.HostKeyPath))
	if err != nil {
		t.Fatalf("stat host key dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("host key dir mode: want 0700, got %o (umask-respecting MkdirAll regression)", got)
	}

	if fp := s.HostKeyFingerprint(); !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("HostKeyFingerprint: want SHA256: prefix, got %q", fp)
	}
}

func TestNew_LoadsExistingHostKey_SameFingerprintAcrossBoots(t *testing.T) {
	cfg := testConfig(t)
	s1, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	s2, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	if s1.HostKeyFingerprint() != s2.HostKeyFingerprint() {
		t.Errorf("fingerprints differ across boots: %q vs %q",
			s1.HostKeyFingerprint(), s2.HostKeyFingerprint())
	}
}

func TestNew_RefusesPermissiveHostKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}

	cfg := testConfig(t)
	// First boot creates the key with 0600.
	if _, err := New(cfg, nil, nil); err != nil {
		t.Fatalf("first New: %v", err)
	}
	// Loosen the mode and retry.
	if err := os.Chmod(cfg.HostKeyPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := New(cfg, nil, nil)
	if !errors.Is(err, errPermissiveHostKeyMode) {
		t.Errorf("expected errPermissiveHostKeyMode, got %v", err)
	}
}

func TestNew_FailsWhenHostKeyDirUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses mode checks; can't test unwritable dir")
	}

	// Create a parent directory that the test can't write into.
	parent := t.TempDir()
	roDir := filepath.Join(parent, "readonly")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("mkdir readonly: %v", err)
	}
	cfg := testConfig(t)
	cfg.HostKeyPath = filepath.Join(roDir, "subdir", "key")

	_, err := New(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected New to fail when host-key dir is unwritable")
	}
}

func TestValidate_RejectsEmptyHostKeyPath(t *testing.T) {
	cfg := Config{HostKeyPath: ""}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Error("expected Validate to reject empty HostKeyPath")
	}
}

// TestListenAndServe_AcceptsAndReleasesIPs is a smoke test: bind on
// :0, dial a connection, let the handshake-with-stub-auth fail, and
// confirm the in-flight counter releases.
func TestListenAndServe_ReleasesInFlightAfterClose(t *testing.T) {
	cfg := testConfig(t)
	cfg.HandshakeTimeout = 500 * time.Millisecond
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var serveErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = s.ListenAndServe("127.0.0.1:0")
	}()

	addr := waitForListener(t, s)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Don't speak SSH — let the handshake deadline fire.
	_ = conn.Close()

	// Shutdown should drain quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	wg.Wait()
	if serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
		t.Errorf("ListenAndServe returned unexpected error: %v", serveErr)
	}

	s.mu.Lock()
	leftover := len(s.inFlightPerIP)
	s.mu.Unlock()
	if leftover != 0 {
		t.Errorf("inFlightPerIP should be empty after Shutdown, got %d entries", leftover)
	}
}

// TestListenAndServe_PerIPCapRejectsExcess admits exactly
// MaxHandshakesPerIP simultaneous connections from the same IP and
// rejects the next one with an immediate close.
func TestListenAndServe_PerIPCapRejectsExcess(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxHandshakesPerIP = 2
	cfg.HandshakeTimeout = 3 * time.Second
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

	// Open two slots and HOLD them (don't close).
	held := []net.Conn{}
	for i := 0; i < cfg.MaxHandshakesPerIP; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	// Give the accept loop a moment to register the in-flight counts.
	time.Sleep(50 * time.Millisecond)

	// Third dial should be admitted by TCP but immediately closed by
	// our cap check. Verify by reading and seeing EOF/RST quickly.
	c3, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("third dial: %v", err)
	}
	defer c3.Close()
	_ = c3.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	n, readErr := c3.Read(buf)
	if n != 0 || readErr == nil {
		t.Errorf("expected rejected connection to close immediately; read n=%d err=%v", n, readErr)
	}
}

func TestRecoverPanic_LogsAndSurvives(t *testing.T) {
	// Just exercise the helper directly — if it didn't catch a panic
	// the test process would exit.
	defer recoverPanic("test scope")
	panic("synthetic panic — should be recovered")
}

// waitForListener spins briefly until the server has installed its
// listener, then returns the bound address.
func waitForListener(t *testing.T, s *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ln := s.listener
		s.mu.Unlock()
		if ln != nil {
			return ln.Addr().String()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not bind within deadline")
	return ""
}

// TestSSHServerVersionHandshake confirms the SSH banner string is
// sent correctly, as a sanity check against the configured
// ServerVersion. We're explicitly NOT testing auth here — that lives
// in auth_test.go (U7).
func TestSSHServerVersionHandshake(t *testing.T) {
	cfg := testConfig(t)
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

	// Read the SSH version banner from the server. The first line on
	// connect is the server version per RFC 4253 §4.2.
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 256)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read banner: %v", err)
	}
	banner := string(buf[:n])
	if !strings.HasPrefix(banner, "SSH-2.0-Deuce_test") {
		t.Errorf("banner: want prefix %q, got %q", "SSH-2.0-Deuce_test", banner)
	}
}

// quietClient is a helper used by TestSSHServerVersionHandshake to
// satisfy x/crypto/ssh's ClientConfig type without unused imports if
// future tests need it.
var _ = ssh.ClientConfig{}
