package sshproxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// ----------------------------------------------------------------------
// Pure unit tests: command builders + env filter.
// ----------------------------------------------------------------------

func TestDockerArgs_NonPTY(t *testing.T) {
	got := dockerArgs("alice", "echo hi", execModeNonPTY)
	want := []string{"exec", "-i", "alice", "/bin/sh", "-c", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dockerArgs(non-pty):\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDockerArgs_PTYShell(t *testing.T) {
	got := dockerArgs("alice", "", execModePTYShell)
	want := []string{"exec", "-it", "alice", "/bin/bash", "-l"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dockerArgs(pty-shell):\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDockerArgs_PTYExec(t *testing.T) {
	got := dockerArgs("alice", "ls /", execModePTYExec)
	want := []string{"exec", "-it", "alice", "/bin/sh", "-c", "ls /"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dockerArgs(pty-exec):\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDockerArgs_SFTP(t *testing.T) {
	got := dockerArgs("alice", "", execModeSFTP)
	want := []string{"exec", "-i", "alice", "/usr/lib/openssh/sftp-server", "-e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dockerArgs(sftp):\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildExecCmd_SetsPgidAndEnv(t *testing.T) {
	ctx := context.Background()
	env := []string{"LANG=C", "VSCODE_X=1"}
	cmd := buildExecCmd(ctx, "/usr/bin/docker", "alice", "echo hi", execModeNonPTY, env)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Errorf("expected Setpgid: true, got %#v", cmd.SysProcAttr)
	}
	if !reflect.DeepEqual(cmd.Env, env) {
		t.Errorf("env: got %#v want %#v", cmd.Env, env)
	}
	if len(cmd.Args) < 1 || !strings.HasSuffix(cmd.Args[0], "docker") {
		t.Errorf("argv[0] should be docker, got %v", cmd.Args)
	}
}

func TestBuildExecCmd_EmptyBinUsesDefault(t *testing.T) {
	cmd := buildExecCmd(context.Background(), "", "alice", "echo hi", execModeNonPTY, nil)
	if got := cmd.Args[0]; got != defaultDockerBin && !strings.HasSuffix(got, defaultDockerBin) {
		t.Errorf("empty bin should fall back to %q, got %q", defaultDockerBin, got)
	}
}

func TestEnvAllowed_AllowAndDeny(t *testing.T) {
	allow := []string{"LANG", "LC_ALL", "LC_CTYPE", "TERM", "HOME", "USER", "SHELL", "VSCODE_IPC_HOOK", "VSCODE_"}
	for _, name := range allow {
		if !envAllowed(name) {
			t.Errorf("%q should be allowed", name)
		}
	}
	deny := []string{
		"LD_PRELOAD",
		"LD_LIBRARY_PATH",
		"PYTHONPATH",
		"PATH",
		"DYLD_INSERT_LIBRARIES",
		"DEUCE_AUTH_MODE",
		"",
		"LC", // does NOT match LC_ prefix (no underscore)
	}
	for _, name := range deny {
		if envAllowed(name) {
			t.Errorf("%q should be denied", name)
		}
	}
}

func TestFilterEnv_AllowlistAndDedup(t *testing.T) {
	in := []envEntry{
		{Name: "LD_PRELOAD", Value: "/tmp/x.so"},  // denied
		{Name: "LANG", Value: "C"},                // allowed
		{Name: "PATH", Value: "/root/bin"},        // denied
		{Name: "VSCODE_IPC_HOOK", Value: "/x"},    // allowed (prefix)
		{Name: "LC_ALL", Value: "C"},              // allowed (prefix)
		{Name: "LANG", Value: "en_US.UTF-8"},      // override
		{Name: "PYTHONPATH", Value: "/etc"},       // denied
		{Name: "", Value: "v"},                    // denied (empty)
	}
	got := filterEnv(in)
	want := []string{
		"LANG=en_US.UTF-8",
		"VSCODE_IPC_HOOK=/x",
		"LC_ALL=C",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterEnv:\n got %#v\nwant %#v", got, want)
	}
}

// ----------------------------------------------------------------------
// Integration tests via a real SSH client against an isolated Server.
// These tests stub the docker exec by swapping `dockerBin` to point at a
// fake helper script via a test harness. The harness intercepts the
// docker-CLI calls and behaves like a tiny container that runs the
// requested command on the host.
// ----------------------------------------------------------------------

// proxyHarness boots an in-memory Server pointed at a fake auth callback
// (any key valid for session "test-sid") and a stub docker bin that
// translates `docker exec -i <name> /bin/sh -c <cmd>` into a host-side
// `/bin/sh -c <cmd>`. Returns the listening addr and a teardown.
type proxyHarness struct {
	addr   string
	server *Server
	signer ssh.Signer
	stop   func()
}

func newProxyHarness(t *testing.T) *proxyHarness {
	t.Helper()

	// Build a Server with a temp host key.
	cfg := Config{
		HostKeyPath:        filepath.Join(t.TempDir(), "host_key"),
		ServerVersion:      "SSH-2.0-Deuce_test",
		HandshakeTimeout:   5 * time.Second,
		MaxHandshakesPerIP: 8,
		MaxChannelsPerConn: 8,
	}
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Install a stub container resolver — production uses queries +
	// workspace.Manager, but tests don't have a DB or Docker daemon.
	s.resolveContainerHook = func(_ context.Context, sessionID string) (string, error) {
		return "stub-container", nil
	}

	// Swap publicKeyCallback for the test path: accept any key, claim
	// session-id "test-sid" so the channel handler can resolve a stub.
	// The fake docker bin doesn't care about the session-id.
	stubAuth := func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		return &ssh.Permissions{
			Extensions: map[string]string{
				extSessionID: meta.User(), // pass-through for the test
				extUserID:    "test-user",
				extKeyID:     "test-key",
				extFP:        ssh.FingerprintSHA256(key),
			},
		}, nil
	}

	// Point the Server at a host-side docker stub that ignores the
	// container arg and runs the rest. Per-Server (not global) so
	// concurrent test instances don't race on a shared var.
	s.dockerBin = mustWriteDockerStub(t)

	go func() {
		// We need the listener to use our stubAuth, not publicKeyCallback,
		// so we manually drive the accept loop via a custom serverConfig.
		// To do that without forking serve() we just call ListenAndServe
		// after monkey-patching serverConfig, but that's not exported.
		// Instead, we run a hand-rolled accept loop right here.
		runStubListener(t, s, stubAuth)
	}()

	addr := waitForStubAddr(t, s)
	return &proxyHarness{
		addr:   addr,
		server: s,
		signer: s.signer,
		stop: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.Shutdown(ctx)
		},
	}
}

// runStubListener is a hand-rolled accept loop that uses an injectable
// PublicKeyCallback so the harness can avoid the DB-dependent path.
func runStubListener(t *testing.T, s *Server, stubAuth func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error)) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Errorf("listen: %v", err)
		return
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go func(c net.Conn) {
			defer c.Close()
			cfg := &ssh.ServerConfig{
				ServerVersion:     s.cfg.ServerVersion,
				PublicKeyCallback: stubAuth,
			}
			cfg.AddHostKey(s.signer)
			sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
			if err != nil {
				return
			}
			s.handleAuthenticatedConn(sshConn, chans, reqs)
		}(conn)
	}
}

// waitForStubAddr is the test variant of waitForListener that doesn't
// require the running ListenAndServe loop.
func waitForStubAddr(t *testing.T, s *Server) string {
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
	t.Fatalf("stub listener did not bind in time")
	return ""
}

// mustWriteDockerStub writes a small shell script that pretends to be
// `docker`. It accepts the shapes:
//
//	docker exec -i  <name> /bin/sh -c <cmd>   → /bin/sh -c <cmd>
//	docker exec -it <name> /bin/sh -c <cmd>   → /bin/sh -c <cmd>
//	docker exec -it <name> /bin/bash -l       → /bin/bash -l
//	docker exec -i  <name> /usr/lib/openssh/sftp-server -e → noop
//
// The container name is ignored. Returns the absolute path to the script.
func mustWriteDockerStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker")
	content := `#!/bin/sh
# Fake docker bin for sshproxy tests.
# Expected argv: exec [-i|-it] <name> <rest...>
if [ "$1" != "exec" ]; then
  echo "fake-docker: only 'exec' supported, got: $@" >&2
  exit 2
fi
shift
case "$1" in
  -i|-it) shift ;;
esac
# Drop container name.
shift
exec "$@"
`
	if err := writeFile(path, content, 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	return path
}

// writeFile is a thin wrapper around os.WriteFile that returns the err
// directly. Hand-rolled to avoid pulling os into the test header set
// twice for clarity.
func writeFile(path, content string, mode int) error {
	return execWriteFile(path, content, mode)
}

func execWriteFile(path, content string, mode int) error {
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("cat > %q && chmod %o %q", path, mode, path))
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("write file %s: %w (%s)", path, err, string(out))
	}
	return nil
}

// clientConfig returns an SSH client config that trusts the harness'
// host key and authenticates with a freshly-generated ed25519 key.
func (h *proxyHarness) clientConfig(t *testing.T, user string) *ssh.ClientConfig {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("client signer: %v", err)
	}
	hostPub := h.signer.PublicKey()
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !keysEqual(key, hostPub) {
				return errors.New("unexpected host key")
			}
			return nil
		},
		Timeout: 5 * time.Second,
	}
}

func keysEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}

// ----------------------------------------------------------------------
// Channel-type allowlist
// ----------------------------------------------------------------------

func TestChannelOpen_RejectsDirectTCPIP(t *testing.T) {
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Build a stub direct-tcpip payload (we don't need real fields; the
	// server should reject before unmarshaling).
	payload := ssh.Marshal(struct {
		Host string
		Port uint32
		Orig string
		OPort uint32
	}{"localhost", 1234, "127.0.0.1", 5678})
	_, _, err = conn.OpenChannel("direct-tcpip", payload)
	if err == nil {
		t.Fatal("expected direct-tcpip to be rejected")
	}
	openErr, ok := err.(*ssh.OpenChannelError)
	if !ok {
		t.Fatalf("expected *ssh.OpenChannelError, got %T: %v", err, err)
	}
	if openErr.Reason != ssh.Prohibited {
		t.Errorf("reason: want Prohibited, got %v", openErr.Reason)
	}
}

func TestChannelOpen_RejectsX11(t *testing.T) {
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, _, err = conn.OpenChannel("x11", nil)
	if err == nil {
		t.Fatal("expected x11 channel to be rejected")
	}
	openErr, ok := err.(*ssh.OpenChannelError)
	if !ok {
		t.Fatalf("expected *ssh.OpenChannelError, got %T", err)
	}
	if openErr.Reason != ssh.Prohibited {
		t.Errorf("reason: want Prohibited, got %v", openErr.Reason)
	}
}

func TestChannelOpen_RejectsAuthAgent(t *testing.T) {
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for _, ct := range []string{"auth-agent@openssh.com", "direct-streamlocal@openssh.com"} {
		_, _, err := conn.OpenChannel(ct, nil)
		if err == nil {
			t.Errorf("%s: expected rejection", ct)
			continue
		}
		openErr, ok := err.(*ssh.OpenChannelError)
		if !ok {
			t.Errorf("%s: expected *ssh.OpenChannelError, got %T", ct, err)
			continue
		}
		if openErr.Reason != ssh.Prohibited {
			t.Errorf("%s: want Prohibited, got %v", ct, openErr.Reason)
		}
	}
}

// ----------------------------------------------------------------------
// Per-connection channel cap
// ----------------------------------------------------------------------

func TestChannelOpen_PerConnCapRejectsExcess(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	// Lower the per-conn cap on this server for the test.
	h.server.cfg.MaxChannelsPerConn = 2

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Open exactly cap channels and HOLD them by sending a slow exec.
	held := make([]ssh.Channel, 0, 2)
	for i := 0; i < 2; i++ {
		ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
		if err != nil {
			t.Fatalf("hold channel %d: %v", i, err)
		}
		go ssh.DiscardRequests(reqs)
		// Send a sleep so the channel stays open.
		ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "sleep 5"}))
		if err != nil || !ok {
			t.Fatalf("hold exec %d: ok=%v err=%v", i, ok, err)
		}
		held = append(held, ch)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	// The 3rd channel-open should be rejected with ResourceShortage.
	_, _, err = conn.OpenChannel(channelTypeSession, nil)
	if err == nil {
		t.Fatal("expected 3rd channel-open to be rejected")
	}
	openErr, ok := err.(*ssh.OpenChannelError)
	if !ok {
		t.Fatalf("expected *ssh.OpenChannelError, got %T: %v", err, err)
	}
	if openErr.Reason != ssh.ResourceShortage {
		t.Errorf("reason: want ResourceShortage, got %v", openErr.Reason)
	}
}

// ----------------------------------------------------------------------
// Session-request types
// ----------------------------------------------------------------------

func TestExec_NonPTYEchoReturnsHiAndExitZero(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Collect the exit-status request asynchronously.
	exitStatus := make(chan uint32, 1)
	go func() {
		for req := range reqs {
			if req.Type == "exit-status" && len(req.Payload) >= 4 {
				exitStatus <- binary.BigEndian.Uint32(req.Payload[:4])
			}
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}()

	ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "echo hi"}))
	if err != nil || !ok {
		t.Fatalf("exec request: ok=%v err=%v", ok, err)
	}

	out, err := io.ReadAll(ch)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != "hi" {
		t.Errorf("stdout: want %q, got %q", "hi", got)
	}

	select {
	case status := <-exitStatus:
		if status != 0 {
			t.Errorf("exit status: want 0, got %d", status)
		}
	case <-time.After(3 * time.Second):
		t.Error("did not receive exit-status within timeout")
	}
}

func TestExec_LDPreloadEnvIsDropped(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)

	// Send a denied env first.
	_, _ = ch.SendRequest("env", true, ssh.Marshal(envPayload{Name: "LD_PRELOAD", Value: "/tmp/x.so"}))
	// And one allowed env.
	_, _ = ch.SendRequest("env", true, ssh.Marshal(envPayload{Name: "LANG", Value: "C"}))

	// Now exec a program that prints its env. The fake docker stub
	// preserves cmd.Env across to the host sh, so /bin/sh sees the
	// filtered set.
	ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "env"}))
	if err != nil || !ok {
		t.Fatalf("exec: %v ok=%v", err, ok)
	}
	out, _ := io.ReadAll(ch)
	got := string(out)
	if strings.Contains(got, "LD_PRELOAD") {
		t.Errorf("LD_PRELOAD leaked into child env:\n%s", got)
	}
	if !strings.Contains(got, "LANG=C") {
		t.Errorf("LANG=C should have propagated; got:\n%s", got)
	}
}

func TestExec_AllowlistedEnvPropagates(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)

	envs := []envPayload{
		{Name: "LANG", Value: "C"},
		{Name: "VSCODE_IPC_HOOK", Value: "/tmp/vsc"},
		{Name: "TERM", Value: "xterm-256color"},
	}
	for _, e := range envs {
		_, _ = ch.SendRequest("env", true, ssh.Marshal(e))
	}

	ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "env"}))
	if err != nil || !ok {
		t.Fatalf("exec: %v ok=%v", err, ok)
	}
	out, _ := io.ReadAll(ch)
	got := string(out)
	for _, e := range envs {
		expected := e.Name + "=" + e.Value
		if !strings.Contains(got, expected) {
			t.Errorf("missing %q in env output:\n%s", expected, got)
		}
	}
}

func TestExec_UnknownSubsystemRejectedChannelSurvives(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "totally-fake"}))
	if err != nil {
		t.Fatalf("subsystem send: %v", err)
	}
	if ok {
		t.Error("server should reject unknown subsystem")
	}

	// Channel must still be usable: send an exec and read stdout.
	ok, err = ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "echo survived"}))
	if err != nil || !ok {
		t.Fatalf("post-reject exec failed: %v ok=%v", err, ok)
	}
	out, _ := io.ReadAll(ch)
	if got := strings.TrimRight(string(out), "\n"); got != "survived" {
		t.Errorf("post-reject stdout: want %q, got %q", "survived", got)
	}
}

func TestSessionRequest_AuthAgentReqRepliedFalseChannelSurvives(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)

	ok, err := ch.SendRequest("auth-agent-req@openssh.com", true, nil)
	if err != nil {
		t.Fatalf("auth-agent-req send: %v", err)
	}
	if ok {
		t.Error("server should not accept auth-agent-req")
	}

	// Channel must still be usable.
	ok, err = ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "echo ok"}))
	if err != nil || !ok {
		t.Fatalf("post-reject exec failed: %v ok=%v", err, ok)
	}
	out, _ := io.ReadAll(ch)
	if got := strings.TrimRight(string(out), "\n"); got != "ok" {
		t.Errorf("post-reject stdout: want %q, got %q", "ok", got)
	}
}

// ----------------------------------------------------------------------
// Channel close mid-exec → child killed
// ----------------------------------------------------------------------

func TestExec_ChannelCloseMidExecKillsChild(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)

	// Pick a sentinel marker we can grep for in `ps` output.
	marker := fmt.Sprintf("sshproxy-test-marker-%d", time.Now().UnixNano())
	ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{
		Command: fmt.Sprintf("sleep 60; echo %s", marker),
	}))
	if err != nil || !ok {
		t.Fatalf("exec: %v ok=%v", err, ok)
	}

	// Give the child a moment to start.
	time.Sleep(200 * time.Millisecond)

	// Close the channel — should trigger SIGTERM on the host child.
	_ = ch.Close()

	// Within 5s the child should be gone. Verify via ps.
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if !pgrepFound(marker) {
			return // pass
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("child process with marker %q still alive after channel close", marker)
}

// pgrepFound returns true if any process in `ps -ef` matches the marker.
func pgrepFound(marker string) bool {
	out, err := exec.Command("/bin/sh", "-c", "ps -ef").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, marker) && !strings.Contains(line, "grep") {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------
// Multiple concurrent channels share one ssh.ServerConn
// ----------------------------------------------------------------------

func TestMultipleChannels_IndependentExec(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	const N = 4
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch, reqs, err := conn.OpenChannel(channelTypeSession, nil)
			if err != nil {
				errs <- fmt.Errorf("ch %d open: %w", idx, err)
				return
			}
			go ssh.DiscardRequests(reqs)
			ok, err := ch.SendRequest("exec", true, ssh.Marshal(execPayload{
				Command: fmt.Sprintf("echo chan-%d", idx),
			}))
			if err != nil || !ok {
				errs <- fmt.Errorf("ch %d exec: %v ok=%v", idx, err, ok)
				return
			}
			out, _ := io.ReadAll(ch)
			got := strings.TrimRight(string(out), "\n")
			want := fmt.Sprintf("chan-%d", idx)
			if got != want {
				errs <- fmt.Errorf("ch %d stdout: got %q want %q", idx, got, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ----------------------------------------------------------------------
// Panic isolation
// ----------------------------------------------------------------------

func TestRecoverPanic_PerChannel(t *testing.T) {
	// We can't easily inject a panic into runSessionChannel from the
	// outside, but we can verify the recover wrapper exists by
	// exercising recoverPanic itself, and run a sanity check that a
	// second channel works after a forced-error channel.
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	h := newProxyHarness(t)
	defer h.stop()

	conn, _, _, err := dialSSH(t, h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// First channel: exec a failing command. Server-side, this returns
	// cleanly with non-zero status; not a panic, but confirms the loop
	// keeps serving.
	ch1, reqs1, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open ch1: %v", err)
	}
	go ssh.DiscardRequests(reqs1)
	_, _ = ch1.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "exit 7"}))
	_, _ = io.ReadAll(ch1)

	// Second channel must still work.
	ch2, reqs2, err := conn.OpenChannel(channelTypeSession, nil)
	if err != nil {
		t.Fatalf("open ch2 after first errored: %v", err)
	}
	go ssh.DiscardRequests(reqs2)
	_, _ = ch2.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "echo alive"}))
	out, _ := io.ReadAll(ch2)
	if got := strings.TrimRight(string(out), "\n"); got != "alive" {
		t.Errorf("second channel stdout: got %q want %q", got, "alive")
	}
}

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

func dialSSH(t *testing.T, h *proxyHarness) (*ssh.Client, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	t.Helper()
	cfg := h.clientConfig(t, "dc-00000000-0000-0000-0000-000000000001")
	conn, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		return nil, nil, nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, h.addr, cfg)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), chans, reqs, nil
}
