package sshproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
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
// Pure unit tests: buildSFTPCmd argv shape.
// ----------------------------------------------------------------------

func TestBuildSFTPCmd_ArgvIsDockerExecIWithSFTPServerE(t *testing.T) {
	cmd := buildSFTPCmd(context.Background(), "/usr/bin/docker", "session-container")

	// argv[0] is the binary path; argv[1:] is what we passed.
	wantArgs := []string{"/usr/bin/docker", "exec", "-i", "session-container", "/usr/lib/openssh/sftp-server", "-e"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Errorf("argv mismatch:\n got %#v\nwant %#v", cmd.Args, wantArgs)
	}
}

func TestBuildSFTPCmd_NeverUsesPTYFlag(t *testing.T) {
	// SFTP is binary framing; -it would CRLF-translate and corrupt
	// packets. The regression risk is that someone "fixes" interactive
	// SFTP by adding -t, so we lock the constraint down explicitly.
	cmd := buildSFTPCmd(context.Background(), "docker", "alice")
	for _, a := range cmd.Args {
		if a == "-it" || a == "-t" {
			t.Errorf("SFTP argv must not include %q (PTY would corrupt binary framing): %#v", a, cmd.Args)
		}
	}
}

func TestBuildSFTPCmd_NoShellWrapper(t *testing.T) {
	// Argv must hit sftp-server directly, not via /bin/sh -c. A shell
	// wrapper would buffer stdout and add a syscall layer for no gain.
	cmd := buildSFTPCmd(context.Background(), "docker", "alice")
	for _, a := range cmd.Args {
		if a == "/bin/sh" || a == "/bin/bash" || a == "-c" {
			t.Errorf("SFTP argv must not invoke a shell, got: %#v", cmd.Args)
		}
	}
}

func TestBuildSFTPCmd_EmptyBinUsesDefault(t *testing.T) {
	cmd := buildSFTPCmd(context.Background(), "", "alice")
	if got := cmd.Args[0]; got != defaultDockerBin && !strings.HasSuffix(got, defaultDockerBin) {
		t.Errorf("empty bin should fall back to %q, got %q", defaultDockerBin, got)
	}
}

func TestBuildSFTPCmd_SetsPgid(t *testing.T) {
	// Setpgid: true is required for the channel-close cleanup path —
	// kill(-pid) reaches the docker CLI and its grandchildren.
	cmd := buildSFTPCmd(context.Background(), "docker", "alice")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Errorf("expected Setpgid: true, got %#v", cmd.SysProcAttr)
	}
}

// ----------------------------------------------------------------------
// Subsystem dispatch: integration through a stub docker bin.
// ----------------------------------------------------------------------

// TestSubsystem_SFTPReplyTrueThenChildExits exercises the happy-ish path
// where the client requests `subsystem sftp` and the server replies true.
// We can't easily run a real sftp-server in CI, so the docker stub tries
// to exec the literal `/usr/lib/openssh/sftp-server` path — on Linux CI
// this typically exists (if openssh is installed) and runs as a fresh
// sftp-server connected to its own filesystem; on macOS it almost
// certainly does NOT exist and the stub fails with exit 127. Either way
// the SSH-level contract holds: subsystem reply is true, the channel
// closes cleanly with an exit-status request.
func TestSubsystem_SFTPRequestRepliedTrue(t *testing.T) {
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
	exitStatus := make(chan uint32, 1)
	go func() {
		for req := range reqs {
			if req.Type == "exit-status" && len(req.Payload) >= 4 {
				exitStatus <- binary.BigEndian.Uint32(req.Payload[:4])
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}()

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "sftp"}))
	if err != nil {
		t.Fatalf("subsystem request: %v", err)
	}
	if !ok {
		t.Fatal("expected subsystem sftp to be accepted")
	}

	// Half-close our write side so the in-container sftp-server (or its
	// surrogate) sees EOF and exits. Then drain stdout and wait for the
	// exit-status reply.
	_ = ch.CloseWrite()
	_, _ = io.Copy(io.Discard, ch)

	select {
	case <-exitStatus:
		// Pass: child exited cleanly (or with non-zero), we got the
		// status. We don't assert the specific code because it depends
		// on whether /usr/lib/openssh/sftp-server happens to exist on
		// the test host.
	case <-time.After(5 * time.Second):
		t.Error("did not receive exit-status within 5s of half-closing channel")
	}
}

// TestSubsystem_UnknownSubsystemStillRejected confirms the U8 behavior
// for non-sftp subsystems survives the U9 rewrite — the channel must
// remain usable for a subsequent exec.
func TestSubsystem_UnknownSubsystemStillRejected(t *testing.T) {
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

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "publickey-hostbound@openssh.com"}))
	if err != nil {
		t.Fatalf("subsystem send: %v", err)
	}
	if ok {
		t.Error("non-sftp subsystem should still be rejected")
	}

	// Channel must still serve an exec.
	ok, err = ch.SendRequest("exec", true, ssh.Marshal(execPayload{Command: "echo survived"}))
	if err != nil || !ok {
		t.Fatalf("post-reject exec failed: ok=%v err=%v", ok, err)
	}
	out, _ := io.ReadAll(ch)
	if got := strings.TrimRight(string(out), "\n"); got != "survived" {
		t.Errorf("post-reject stdout: want %q, got %q", "survived", got)
	}
}

// TestSubsystem_SFTPRejectedAfterPTYReq locks down the "no PTY for SFTP"
// invariant: if the client buffered a pty-req on this channel, an SFTP
// subsystem request must be rejected. A PTY would CRLF-translate the
// binary SFTP framing and corrupt the first packets.
func TestSubsystem_SFTPRejectedAfterPTYReq(t *testing.T) {
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

	// Send pty-req first.
	ok, err := ch.SendRequest("pty-req", true, ssh.Marshal(ptyReqPayload{
		Term: "xterm-256color", Cols: 80, Rows: 24,
	}))
	if err != nil || !ok {
		t.Fatalf("pty-req: ok=%v err=%v", ok, err)
	}

	// Now request SFTP — must be rejected.
	ok, err = ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "sftp"}))
	if err != nil {
		t.Fatalf("subsystem send: %v", err)
	}
	if ok {
		t.Error("SFTP subsystem must be rejected after pty-req on same channel")
	}
}

// TestSubsystem_MissingSFTPServerPropagatesNonZeroExitStatus is the
// "container without sftp-server" scenario. The docker stub will try to
// exec the literal /usr/lib/openssh/sftp-server path. If it doesn't
// exist on the test host (very likely on macOS, sometimes on minimal
// Linux), the stub exits with a non-zero code (typically 127 from sh
// "command not found"). The server's exit-status reply must propagate
// that.
func TestSubsystem_MissingSFTPServerPropagatesNonZeroExitStatus(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	if _, err := exec.LookPath("/usr/lib/openssh/sftp-server"); err == nil {
		t.Skip("sftp-server is installed on this host; the 'missing' scenario can't run here")
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
	exitStatus := make(chan uint32, 1)
	go func() {
		for req := range reqs {
			if req.Type == "exit-status" && len(req.Payload) >= 4 {
				exitStatus <- binary.BigEndian.Uint32(req.Payload[:4])
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}()

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "sftp"}))
	if err != nil || !ok {
		t.Fatalf("subsystem: ok=%v err=%v", ok, err)
	}

	_ = ch.CloseWrite()
	_, _ = io.Copy(io.Discard, ch)

	select {
	case code := <-exitStatus:
		if code == 0 {
			t.Errorf("expected non-zero exit-status when sftp-server is missing, got 0")
		}
	case <-time.After(5 * time.Second):
		t.Error("did not receive exit-status within 5s")
	}
}

// TestSubsystem_ChannelCloseMidTransferKillsChild verifies the cleanup
// path: closing the SSH channel while the in-container sftp-server is
// "live" results in the docker exec child being killed within 5s.
//
// We use a special docker stub that recognizes the SFTP argv shape and
// substitutes a long sleep, so we can observe whether the host-side
// `docker` process actually exits when the channel closes.
func TestSubsystem_ChannelCloseMidTransferKillsChild(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps not available for child-process verification")
	}

	h := newProxyHarness(t)
	defer h.stop()

	// Swap the harness' docker stub for one that substitutes a sleep
	// for the sftp-server invocation. Marker token lets us locate the
	// child in `ps` output.
	marker := fmt.Sprintf("sshproxy-sftp-marker-%d", time.Now().UnixNano())
	h.server.dockerBin = mustWriteSFTPSleepDockerStub(t, marker)

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

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "sftp"}))
	if err != nil || !ok {
		t.Fatalf("subsystem: ok=%v err=%v", ok, err)
	}

	// Wait for the child to start — race-detector builds and parallel
	// tests can stretch the docker-stub fork/exec sequence to a few
	// seconds. Poll up to 10s.
	started := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if pgrepFound(marker) {
			started = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !started {
		t.Fatalf("expected sftp surrogate to be running with marker %q within 10s", marker)
	}

	// Close the channel — should trigger SIGTERM → SIGKILL on the host
	// child via process-group kill.
	_ = ch.Close()

	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if !pgrepFound(marker) {
			return // pass
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("docker exec child with marker %q still alive 7s after channel close", marker)
}

// TestSubsystem_SFTPStderrNotMirroredToChannelStderr verifies the
// sftp-specific stderr handling. The U8 exec path mirrors stderr to
// ch.Stderr(), but for SFTP we route it to io.Discard so the
// in-container sftp-server's `-e` debug stream doesn't leak into the
// client's binary protocol surface.
func TestSubsystem_SFTPStderrNotMirroredToChannelStderr(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}

	h := newProxyHarness(t)
	defer h.stop()

	// Stub that writes a sentinel to stderr then exits.
	sentinel := "SFTP_STDERR_LEAK_PROBE"
	h.server.dockerBin = mustWriteStderrProbeDockerStub(t, sentinel)

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

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(subsystemPayload{Subsystem: "sftp"}))
	if err != nil || !ok {
		t.Fatalf("subsystem: ok=%v err=%v", ok, err)
	}

	// Drain stderr and stdout concurrently. If the SFTP stderr leak
	// regression returns, sentinel will show up on ch.Stderr().
	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderrBuf, ch.Stderr())
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.Discard, ch)
	}()
	_ = ch.CloseWrite()
	wg.Wait()

	if strings.Contains(stderrBuf.String(), sentinel) {
		t.Errorf("sftp-server stderr leaked to SSH ch.Stderr(): %q", stderrBuf.String())
	}
}

// ----------------------------------------------------------------------
// Stubs
// ----------------------------------------------------------------------

// mustWriteSFTPSleepDockerStub writes a docker stub that recognizes the
// SFTP argv shape (`exec -i <name> /usr/lib/openssh/sftp-server -e`)
// and replaces sftp-server with a long-running surrogate script whose
// filename contains `marker`. Because Linux/macOS `ps -ef` reports the
// invoked script path in the cmdline, grepping the marker locates the
// running child process.
//
// Other argv shapes fall through to the regular passthrough.
//
// We use a separate surrogate script (not an inline `sh -c "sleep 60 #
// marker"`) because some shells strip trailing comments from argv before
// they reach the kernel, making the marker invisible to `ps`.
func mustWriteSFTPSleepDockerStub(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	surrogatePath := filepath.Join(dir, "sftp-surrogate-"+marker)
	// Plain `sleep 60` — NOT `exec sleep 60`. With exec, the kernel
	// replaces the shell's argv with the child's, which strips the
	// marker-bearing script path from `ps -ef` and the marker poll loop
	// times out. Keeping the script as the parent process preserves
	// the cmdline that pgrepFound greps for. The sleep child is still
	// in the same process group via Setpgid, so the kill-PGID assertion
	// (the actual subject of the test) still exercises the right path.
	surrogate := `#!/bin/sh
sleep 60
`
	if err := writeFile(surrogatePath, surrogate, 0o755); err != nil {
		t.Fatalf("write sftp surrogate: %v", err)
	}

	path := filepath.Join(dir, "docker")
	content := `#!/bin/sh
# Fake docker bin: sftp surrogate that sleeps so tests can observe
# whether channel-close kills the host-side child.
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
# If this looks like an SFTP invocation, replace with the marker-named
# surrogate so 'ps -ef' shows the marker in the cmdline.
case "$1" in
  *sftp-server)
    exec ` + surrogatePath + `
    ;;
esac
exec "$@"
`
	if err := writeFile(path, content, 0o755); err != nil {
		t.Fatalf("write sftp-sleep docker stub: %v", err)
	}
	return path
}

// mustWriteStderrProbeDockerStub writes a docker stub that, for SFTP
// invocations, prints `sentinel` to stderr then exits successfully.
// Used to verify stderr is discarded rather than mirrored to the SSH
// channel's stderr stream.
func mustWriteStderrProbeDockerStub(t *testing.T, sentinel string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker")
	content := `#!/bin/sh
if [ "$1" != "exec" ]; then exit 2; fi
shift
case "$1" in
  -i|-it) shift ;;
esac
shift
case "$1" in
  */sftp-server)
    printf '%s\n' '` + sentinel + `' >&2
    exit 0
    ;;
esac
exec "$@"
`
	if err := writeFile(path, content, 0o755); err != nil {
		t.Fatalf("write stderr-probe docker stub: %v", err)
	}
	return path
}
