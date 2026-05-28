package sshproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// SSH channel-type allowlist. "session" and "direct-tcpip" are
// accepted; any other channel-open is rejected with ssh.Prohibited.
// This blocks `x11`, `auth-agent@openssh.com`,
// `direct-streamlocal@openssh.com`, and everything else.
//
// direct-tcpip is needed by VS Code Remote-SSH to tunnel the
// in-container vscode-server's listening port back to the editor over
// the existing SSH connection. Destinations are restricted to loopback
// only — see validateLoopbackDest in tcpip.go.
const channelTypeSession = "session"

// SSH session-request types we recognize. Anything not in this set
// (notably `auth-agent-req@openssh.com`) is replied to with (false, nil)
// and otherwise ignored.
const (
	reqPTY          = "pty-req"
	reqEnv          = "env"
	reqShell        = "shell"
	reqExec         = "exec"
	reqSubsystem    = "subsystem"
	reqWindowChange = "window-change"
	reqSignal       = "signal"
)

// Lifetime channel cap per SSH connection. Above this, additional
// channel-open requests are rejected with ssh.ResourceShortage, even if
// earlier channels have since closed.
const lifetimeChannelCap = 64

// Process-wide cap on concurrent docker exec children. Defends against a
// resource-exhaustion path where many authenticated sessions each open
// the per-connection cap of channels. Above this, channel-open returns
// ssh.ResourceShortage.
const globalExecCap = 256

// processGroupKillGrace is how long the per-channel cleanup waits between
// SIGTERM and SIGKILL on the docker exec child.
const processGroupKillGrace = 5 * time.Second

// dockerExecActive tracks the number of currently-running docker exec
// children. Used to enforce globalExecCap across the whole process.
var dockerExecActive atomic.Int64

// SSH payload structs. crypto/ssh has no public typed structs for these
// request payloads, so we define them locally and decode with ssh.Unmarshal.
type ptyReqPayload struct {
	Term   string
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
	Modes  string
}

type execPayload struct {
	Command string
}

type subsystemPayload struct {
	Subsystem string
}

type envPayload struct {
	Name  string
	Value string
}

type windowChangePayload struct {
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

type signalPayload struct {
	Name string
}

// connState tracks per-ssh-connection channel counts. Lives on the stack
// of handleAuthenticatedConn, captured by the channel-open loop.
type connState struct {
	mu       sync.Mutex
	active   int
	lifetime int
}

// handleAuthenticatedConn is invoked after a successful SSH handshake.
// It enforces the channel-type allowlist, per-connection caps, and spawns
// a goroutine per accepted session channel. Each goroutine carries its
// own defer recover() so a panic in one channel never propagates to its
// peers or to the listener.
func (s *Server) handleAuthenticatedConn(sshConn *ssh.ServerConn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) {
	// Discard global requests: VS Code may send `keepalive@openssh.com`
	// or `no-more-sessions@openssh.com`; both are fine to drop.
	go ssh.DiscardRequests(reqs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := &connState{}
	sessionID := sshConn.Permissions.Extensions[extSessionID]
	userID := sshConn.Permissions.Extensions[extUserID]

	var channelWG sync.WaitGroup

	for newCh := range chans {
		switch newCh.ChannelType() {
		case channelTypeSession:
			if !s.admitChannel(state, sessionID, userID, newCh) {
				continue
			}
			ch, chanReqs, err := newCh.Accept()
			if err != nil {
				slog.Warn("ssh channel accept failed", "error", err, "session_id", sessionID)
				s.releaseChannel(state)
				continue
			}
			if s.metrics != nil {
				s.metrics.incChannelOpenSession()
			}
			channelWG.Add(1)
			go func() {
				defer channelWG.Done()
				defer recoverPanic("ssh session channel")
				defer s.releaseChannel(state)
				s.runSessionChannel(ctx, sshConn, ch, chanReqs, sessionID, userID)
			}()

		case channelTypeDirectTCPIP:
			// Parse + validate the destination BEFORE counting against
			// the channel cap, so a hostile peer can't burn channel
			// slots with malformed payloads.
			var p directTCPIPPayload
			if err := ssh.Unmarshal(newCh.ExtraData(), &p); err != nil {
				slog.Info("ssh direct-tcpip rejected: malformed payload",
					"session_id", sessionID, "user_id", userID, "error", err)
				if s.metrics != nil {
					s.metrics.incChannelOpenOther()
				}
				_ = newCh.Reject(ssh.ConnectionFailed, "malformed payload")
				continue
			}
			if err := validateLoopbackDest(p.DestHost, p.DestPort); err != nil {
				slog.Info("ssh direct-tcpip rejected: non-loopback destination",
					"session_id", sessionID, "user_id", userID,
					"dest_host", p.DestHost, "dest_port", p.DestPort, "error", err)
				if s.metrics != nil {
					s.metrics.incChannelOpenOther()
				}
				_ = newCh.Reject(ssh.Prohibited, "destination not allowed")
				continue
			}
			if !s.admitChannel(state, sessionID, userID, newCh) {
				continue
			}
			ch, chanReqs, err := newCh.Accept()
			if err != nil {
				slog.Warn("ssh direct-tcpip accept failed", "error", err, "session_id", sessionID)
				s.releaseChannel(state)
				continue
			}
			if s.metrics != nil {
				s.metrics.incChannelOpenSession()
			}
			channelWG.Add(1)
			go func() {
				defer channelWG.Done()
				defer recoverPanic("ssh direct-tcpip channel")
				defer s.releaseChannel(state)
				s.runDirectTCPIPChannel(ctx, ch, chanReqs, sessionID, userID, p)
			}()

		default:
			// RFC 4254 §5.1 reserves "Prohibited" for "denied because
			// of policy" — exactly the case here.
			slog.Info("ssh channel rejected: type not allowed",
				"type", newCh.ChannelType(),
				"session_id", sessionID,
				"user_id", userID,
			)
			if s.metrics != nil {
				s.metrics.incChannelOpenOther()
			}
			if err := newCh.Reject(ssh.Prohibited, "channel type not allowed"); err != nil {
				slog.Debug("channel reject failed", "type", newCh.ChannelType(), "error", err)
			}
		}
	}

	channelWG.Wait()
	_ = sshConn.Close()
}

// admitChannel enforces the three channel caps. Returns true if the
// channel may be accepted; otherwise it rejects newCh and returns false.
// Holds state.mu only briefly.
func (s *Server) admitChannel(state *connState, sessionID, userID string, newCh ssh.NewChannel) bool {
	max := s.cfg.MaxChannelsPerConn

	// Global cap: check first so per-connection bookkeeping doesn't
	// mutate when we're going to reject anyway.
	if dockerExecActive.Load() >= int64(globalExecCap) {
		slog.Warn("ssh channel rejected: global docker-exec cap reached",
			"cap", globalExecCap, "session_id", sessionID, "user_id", userID)
		if err := newCh.Reject(ssh.ResourceShortage, "server busy"); err != nil {
			slog.Debug("channel reject failed", "error", err)
		}
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.lifetime >= lifetimeChannelCap {
		slog.Info("ssh channel rejected: lifetime cap reached",
			"cap", lifetimeChannelCap, "session_id", sessionID, "user_id", userID)
		if err := newCh.Reject(ssh.ResourceShortage, "lifetime channel cap reached"); err != nil {
			slog.Debug("channel reject failed", "error", err)
		}
		return false
	}
	if state.active >= max {
		slog.Info("ssh channel rejected: per-connection cap reached",
			"cap", max, "session_id", sessionID, "user_id", userID)
		if err := newCh.Reject(ssh.ResourceShortage, "per-connection channel cap reached"); err != nil {
			slog.Debug("channel reject failed", "error", err)
		}
		return false
	}

	state.active++
	state.lifetime++
	return true
}

// releaseChannel decrements the active-channel counter when a channel
// goroutine exits.
func (s *Server) releaseChannel(state *connState) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.active > 0 {
		state.active--
	}
}

// runSessionChannel consumes channel requests and pipes them into a
// docker exec child. The request stream determines the shape:
//   - "shell" → PTY bash login
//   - "exec"  → non-PTY sh -c (or PTY sh -c if pty-req was buffered)
//   - "subsystem sftp" → non-PTY `sftp-server -e` via docker exec -i
//     (see sftp.go for the full rationale on why no PTY)
//   - "subsystem <other>" → rejected
//
// Returns when the channel closes, the connection EOFs, or the child
// exits. Always sends exit-status before closing.
func (s *Server) runSessionChannel(
	ctx context.Context,
	sshConn *ssh.ServerConn,
	ch ssh.Channel,
	reqs <-chan *ssh.Request,
	sessionID, userID string,
) {
	defer ch.Close()

	chanCtx, chanCancel := context.WithCancel(ctx)
	defer chanCancel()

	var (
		ptyReq    *ptyReqPayload // buffered pty-req before exec/shell
		envBuf    []envEntry     // buffered env requests
		started   bool
		ptmx      *os.File
		cmd       *exec.Cmd
		startErr  error
		startOnce sync.Once
		ioDone    = make(chan struct{}) // closed when stdin/stdout fan-out finishes
	)

	// container is resolved lazily, on the first start trigger. The
	// auth callback already verified reachability, but the container
	// could vanish between auth and start; we re-check here.
	resolveContainer := func() (string, error) {
		if s.resolveContainerHook != nil {
			return s.resolveContainerHook(chanCtx, sessionID)
		}
		sid, err := uuid.Parse(sessionID)
		if err != nil {
			return "", err
		}
		if s.queries == nil || s.workspaces == nil {
			return "", errors.New("workspace manager unavailable")
		}
		session, err := s.queries.GetSession(chanCtx, sid)
		if err != nil {
			return "", err
		}
		return s.workspaces.ContainerName(chanCtx, session.Name)
	}

	// startProc spawns the docker exec child once. After it returns,
	// `started` is true and stdin/stdout/stderr are wired (or the channel
	// is doomed via startErr).
	startProc := func(mode execMode, command string) {
		startOnce.Do(func() {
			container, err := resolveContainer()
			if err != nil {
				startErr = err
				slog.Warn("ssh: container resolve failed at start", "error", err, "session_id", sessionID)
				close(ioDone)
				return
			}

			if mode == execModeSFTP {
				// SFTP doesn't honor client-supplied env vars and forwarding
				// LANG/TERM to the docker CLI would strip its PATH. Use a
				// dedicated builder that leaves cmd.Env nil (inherit parent).
				cmd = buildSFTPCmd(chanCtx, s.dockerBin, container)
			} else {
				env := filterEnv(envBuf)
				cmd = buildExecCmd(chanCtx, s.dockerBin, container, command, mode, env)
			}

			if mode == execModePTYShell || mode == execModePTYExec {
				var ws *pty.Winsize
				if ptyReq != nil {
					ws = &pty.Winsize{
						Rows: uint16(ptyReq.Rows),
						Cols: uint16(ptyReq.Cols),
						X:    uint16(ptyReq.Width),
						Y:    uint16(ptyReq.Height),
					}
				}
				// pty.StartWithAttrs overrides cmd.SysProcAttr, so we
				// re-apply the Setpgid bit explicitly.
				ptmx, err = pty.StartWithAttrs(cmd, ws, &syscall.SysProcAttr{Setpgid: true})
				if err != nil {
					startErr = err
					slog.Warn("ssh: pty.Start failed", "error", err)
					close(ioDone)
					return
				}
				dockerExecActive.Add(1)
				started = true

				// PTY: bidirectional fan-out. stdout pty → ssh channel,
				// stdin ssh channel → pty. Both copies are best-effort:
				// the first to error tears the other down via chanCancel().
				go func() {
					_, _ = io.Copy(ch, ptmx)
				}()
				go func() {
					_, _ = io.Copy(ptmx, ch)
				}()
				go func() {
					_ = cmd.Wait()
					close(ioDone)
				}()
				return
			}

			// Non-PTY: pipe stdin/stdout/stderr ourselves so we can
			// abandon the stdin pump when the child exits. Letting
			// os/exec own the pipes would cause cmd.Wait() to block on
			// the stdin io.Copy until the SSH peer closes its write
			// side, which clients like VS Code's install probe never do
			// before reading the exit-status.
			stdinPipe, err := cmd.StdinPipe()
			if err != nil {
				startErr = err
				close(ioDone)
				return
			}
			stdoutPipe, err := cmd.StdoutPipe()
			if err != nil {
				startErr = err
				_ = stdinPipe.Close()
				close(ioDone)
				return
			}
			stderrPipe, err := cmd.StderrPipe()
			if err != nil {
				startErr = err
				_ = stdinPipe.Close()
				_ = stdoutPipe.Close()
				close(ioDone)
				return
			}
			if err := cmd.Start(); err != nil {
				startErr = err
				slog.Warn("ssh: docker exec start failed", "error", err, "session_id", sessionID)
				close(ioDone)
				return
			}
			dockerExecActive.Add(1)
			started = true

			// stdout/stderr → ssh channel. For SFTP we route stderr to
			// io.Discard instead: sftp-server's `-e` debug output would
			// otherwise mix into the client's stderr stream, and some SFTP
			// clients don't drain stderr promptly which would wedge the
			// pump.
			var pumpsDone sync.WaitGroup
			pumpsDone.Add(2)
			go func() {
				defer pumpsDone.Done()
				_, _ = io.Copy(ch, stdoutPipe)
			}()
			go func() {
				defer pumpsDone.Done()
				if mode == execModeSFTP {
					_, _ = io.Copy(io.Discard, stderrPipe)
				} else {
					_, _ = io.Copy(ch.Stderr(), stderrPipe)
				}
			}()

			// ssh channel → stdin. Independent goroutine; we ignore its
			// completion. When cmd exits, we'll Close() stdin to unblock
			// the io.Copy below.
			go func() {
				_, _ = io.Copy(stdinPipe, ch)
				_ = stdinPipe.Close()
			}()

			go func() {
				_ = cmd.Wait()
				// Force-close stdin so the stdin pump exits even if the
				// peer never closes its write side.
				_ = stdinPipe.Close()
				pumpsDone.Wait()
				close(ioDone)
			}()
		})
	}

	// requestLoop consumes inbound channel requests. Returns when reqs
	// closes (channel closed by peer) or chanCtx is canceled.
	reqsDone := make(chan struct{})
	go func() {
		defer recoverPanic("ssh session request loop")
		defer close(reqsDone)
		for req := range reqs {
			s.handleSessionRequest(req, &ptyReq, &envBuf, &ptmx, cmd, &started, startProc)
		}
	}()

	// Wait for either: (a) the child to exit, (b) the peer closed the
	// channel (request stream EOFs), or (c) the connection-scoped
	// context cancels. In all three cases we go through cleanup.
	select {
	case <-ioDone:
	case <-reqsDone:
	case <-chanCtx.Done():
	}

	// Cleanup. Order matters: close PTY master first so the child sees
	// SIGHUP, then escalate via SIGTERM → SIGKILL on the process group.
	if ptmx != nil {
		_ = ptmx.Close()
	}
	if started && cmd != nil && cmd.Process != nil {
		// Negative pid → process group, matching Setpgid: true. If the
		// process already exited cleanly via ioDone we still call kill;
		// it just returns ESRCH which we ignore.
		pgid, _ := syscall.Getpgid(cmd.Process.Pid)
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}

		// Wait up to processGroupKillGrace for graceful exit, then SIGKILL.
		select {
		case <-ioDone:
		case <-time.After(processGroupKillGrace):
			if pgid > 0 {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-ioDone
		}
		dockerExecActive.Add(-1)
	}

	// Send exit-status. Even if we couldn't start the child, send a
	// non-zero status so the client sees a clean failure rather than a
	// stuck channel.
	status := uint32(0)
	if startErr != nil {
		status = 1
	} else if cmd != nil && cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		if code < 0 {
			status = 1
		} else {
			status = uint32(code)
		}
	}
	exitMsg := make([]byte, 4)
	binary.BigEndian.PutUint32(exitMsg, status)
	if _, err := ch.SendRequest("exit-status", false, exitMsg); err != nil {
		slog.Debug("send exit-status failed", "error", err)
	}
}

// handleSessionRequest dispatches a single SSH session-channel request.
// It runs on the requestLoop goroutine; mutates buffered state until the
// start trigger fires.
func (s *Server) handleSessionRequest(
	req *ssh.Request,
	ptyReqOut **ptyReqPayload,
	envBufOut *[]envEntry,
	ptmxRef **os.File,
	cmd *exec.Cmd,
	started *bool,
	startProc func(mode execMode, command string),
) {
	switch req.Type {
	case reqPTY:
		var p ptyReqPayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("pty-req decode failed", "error", err)
			req.Reply(false, nil)
			return
		}
		if *started {
			// pty-req after start has no useful semantics here.
			req.Reply(false, nil)
			return
		}
		*ptyReqOut = &p
		req.Reply(true, nil)

	case reqEnv:
		var p envPayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("env decode failed", "error", err)
			req.Reply(false, nil)
			return
		}
		if !envAllowed(p.Name) {
			// Drop silently. Replying false would let the client
			// enumerate the allowlist, which is information-leak; OpenSSH
			// itself returns success for unknown env vars.
			req.Reply(true, nil)
			return
		}
		if *started {
			req.Reply(false, nil)
			return
		}
		*envBufOut = append(*envBufOut, envEntry{Name: p.Name, Value: p.Value})
		req.Reply(true, nil)

	case reqShell:
		if *started {
			req.Reply(false, nil)
			return
		}
		// Branch on whether a pty-req was buffered earlier on this
		// channel. An interactive ssh client (terminal panel, `ssh user@host`)
		// sends pty-req first, then shell — we run bash -l with a PTY so
		// the user sees PS1 and bash treats stdin as a TTY.
		//
		// VS Code Remote-SSH connects with `ssh -T` (PTY disabled) and
		// sends shell with NO pty-req. It then pipes its install script
		// into stdin and parses stdout. Forcing a PTY here would cause
		// the kernel pty driver to echo every input byte back, and bash
		// would print its prompt — both corrupt the byte stream the
		// install script needs to talk over. So no-pty-req → no PTY,
		// just a non-interactive login shell.
		mode := execModePTYShell
		if *ptyReqOut == nil {
			mode = execModeNonPTYShell
		}
		req.Reply(true, nil)
		startProc(mode, "")

	case reqExec:
		var p execPayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("exec decode failed", "error", err)
			req.Reply(false, nil)
			return
		}
		if *started {
			req.Reply(false, nil)
			return
		}
		mode := execModeNonPTY
		if *ptyReqOut != nil {
			mode = execModePTYExec
		}
		req.Reply(true, nil)
		startProc(mode, p.Command)

	case reqSubsystem:
		var p subsystemPayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("subsystem decode failed", "error", err)
			req.Reply(false, nil)
			return
		}
		if p.Subsystem != "sftp" {
			// Unknown subsystem (publickey-hostbound@, ascii, etc).
			// Reject; the channel survives so the client can try exec.
			slog.Debug("subsystem rejected: not sftp", "subsystem", p.Subsystem)
			req.Reply(false, nil)
			return
		}
		if *started {
			req.Reply(false, nil)
			return
		}
		// SFTP is binary framing — a PTY would corrupt it. Reject if the
		// client also asked for a pty.
		if *ptyReqOut != nil {
			slog.Debug("sftp rejected: pty-req previously buffered on same channel")
			req.Reply(false, nil)
			return
		}
		// Reply first so the client knows the subsystem is accepted; then
		// kick off the docker exec child. From here on the SSH channel
		// byte stream is the SFTP wire protocol.
		req.Reply(true, nil)
		startProc(execModeSFTP, "")

	case reqWindowChange:
		var p windowChangePayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("window-change decode failed", "error", err)
			return // window-change has wantReply=false per RFC 4254 §6.7
		}
		if *ptmxRef != nil {
			// PTY is alive; resize it. No reply per RFC.
			_ = pty.Setsize(*ptmxRef, &pty.Winsize{
				Rows: uint16(p.Rows),
				Cols: uint16(p.Cols),
				X:    uint16(p.Width),
				Y:    uint16(p.Height),
			})
		}
		// No reply per RFC.

	case reqSignal:
		var p signalPayload
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			slog.Debug("signal decode failed", "error", err)
			return
		}
		// PTY: write ETX (^C) for INT; otherwise log+drop. We don't
		// forward signals to the host docker exec since the host process
		// is a docker CLI wrapper, not the in-container program.
		if *ptmxRef != nil {
			if p.Name == "INT" {
				_, _ = (*ptmxRef).Write([]byte{0x03})
			} else {
				slog.Debug("ssh signal: unsupported in PTY", "signal", p.Name)
			}
		} else {
			slog.Debug("ssh signal: drop on non-PTY exec", "signal", p.Name)
		}
		// No reply per RFC (wantReply always false).

	default:
		// Unknown types, INCLUDING `auth-agent-req@openssh.com`.
		// Reply false; channel survives.
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}
