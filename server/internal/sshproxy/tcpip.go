package sshproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// channelTypeDirectTCPIP is the RFC 4254 §7.2 channel type the client
// opens for client-initiated port forwarding (`ssh -L`-style and
// `ssh -D` dynamic forwarding alike — VS Code Remote-SSH uses the
// latter to tunnel the remote server's listening port back to the
// editor).
const channelTypeDirectTCPIP = "direct-tcpip"

// directTCPIPPayload is the channel-open extra data for direct-tcpip.
// Per RFC 4254 §7.2: dest host + port and originator host + port. We
// only consult DestHost / DestPort; Orig* are accepted for logging /
// future audit but never trusted (the client supplies them).
type directTCPIPPayload struct {
	DestHost string
	DestPort uint32
	OrigHost string
	OrigPort uint32
}

// validateLoopbackDest returns nil if host is one of the loopback
// literals we accept and port is in the unprivileged-or-not range
// 1..65535. Anything else is rejected — direct-tcpip with a non-loopback
// dest would let an authenticated user pivot to anything the deuce
// server (and the container) can reach on the network.
//
// We deliberately accept the three literal forms ("localhost",
// "127.0.0.1", "::1") rather than DNS-resolving — DNS could resolve
// "localhost" to something else, and bash's /dev/tcp doesn't accept
// IPv6 brackets, so the validated forms are also the forms safe to
// splice into the forward command.
func validateLoopbackDest(host string, port uint32) error {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		// allowed
	default:
		return errors.New("destination not loopback")
	}
	if port == 0 || port > 65535 {
		return errors.New("port out of range")
	}
	return nil
}

// runDirectTCPIPChannel handles an accepted direct-tcpip channel:
// resolves the workspace container, spawns the bash-based TCP forwarder
// via docker exec, and pipes the SSH channel bytes through. Mirrors
// runSessionChannel's lifecycle: child exit, peer close, or context
// cancel all trigger cleanup. Always sends exit-status before closing.
func (s *Server) runDirectTCPIPChannel(
	ctx context.Context,
	ch ssh.Channel,
	reqs <-chan *ssh.Request,
	sessionID, userID string,
	payload directTCPIPPayload,
) {
	defer ch.Close()

	// direct-tcpip has no session-level requests of its own; the spec
	// permits "signal" requests but VS Code doesn't send them. Drain
	// to keep the request channel from filling and blocking the peer.
	go ssh.DiscardRequests(reqs)

	chanCtx, chanCancel := context.WithCancel(ctx)
	defer chanCancel()

	container, err := s.resolveContainerForSession(chanCtx, sessionID)
	if err != nil {
		slog.Warn("ssh: direct-tcpip container resolve failed",
			"error", err, "session_id", sessionID, "user_id", userID)
		s.sendChannelExitStatus(ch, 1)
		return
	}

	user := s.resolveExecUser(chanCtx, container)
	cmd := buildTCPForwardCmd(chanCtx, s.dockerBin, container, payload.DestHost, payload.DestPort, user)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		slog.Warn("ssh: direct-tcpip stdin pipe failed", "error", err)
		s.sendChannelExitStatus(ch, 1)
		return
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		slog.Warn("ssh: direct-tcpip stdout pipe failed", "error", err)
		s.sendChannelExitStatus(ch, 1)
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		slog.Warn("ssh: direct-tcpip stderr pipe failed", "error", err)
		s.sendChannelExitStatus(ch, 1)
		return
	}

	if err := cmd.Start(); err != nil {
		slog.Warn("ssh: direct-tcpip start failed",
			"error", err, "session_id", sessionID, "dest_port", payload.DestPort)
		s.sendChannelExitStatus(ch, 1)
		return
	}
	dockerExecActive.Add(1)
	defer dockerExecActive.Add(-1)

	slog.Debug("ssh: direct-tcpip channel started",
		"session_id", sessionID, "user_id", userID,
		"dest_host", payload.DestHost, "dest_port", payload.DestPort,
		"orig_host", payload.OrigHost, "orig_port", payload.OrigPort,
	)

	var pumps sync.WaitGroup
	pumps.Add(2)
	// container → client
	go func() {
		defer pumps.Done()
		_, _ = io.Copy(ch, stdoutPipe)
	}()
	// stderr is discarded: bash's "/dev/tcp: connection refused" or
	// similar would leak into the SSH channel's stderr stream, which
	// the client typically routes nowhere useful for forward channels.
	go func() {
		_, _ = io.Copy(io.Discard, stderrPipe)
	}()
	// client → container
	go func() {
		defer pumps.Done()
		_, _ = io.Copy(stdinPipe, ch)
		_ = stdinPipe.Close()
	}()

	procDone := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		_ = stdinPipe.Close() // unblock the client→container pump
		close(procDone)
	}()

	select {
	case <-procDone:
	case <-chanCtx.Done():
	}

	pumps.Wait()

	status := uint32(0)
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		if code < 0 {
			status = 1
		} else {
			status = uint32(code)
		}
	}
	s.sendChannelExitStatus(ch, status)
}

// resolveContainerForSession centralizes the session→container lookup
// shared by session and direct-tcpip channels. Mirrors the inline
// closure in runSessionChannel.
func (s *Server) resolveContainerForSession(ctx context.Context, sessionID string) (string, error) {
	if s.resolveContainerHook != nil {
		return s.resolveContainerHook(ctx, sessionID)
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return "", err
	}
	if s.queries == nil || s.workspaces == nil {
		return "", errors.New("workspace manager unavailable")
	}
	session, err := s.queries.GetSession(ctx, sid)
	if err != nil {
		return "", err
	}
	return s.workspaces.ContainerName(ctx, session.Name)
}

// sendChannelExitStatus pushes an SSH exit-status request on ch. Errors
// are swallowed at Debug — by the time exit-status fires the peer may
// already have closed the channel and we don't care.
func (s *Server) sendChannelExitStatus(ch ssh.Channel, status uint32) {
	exitMsg := make([]byte, 4)
	binary.BigEndian.PutUint32(exitMsg, status)
	if _, err := ch.SendRequest("exit-status", false, exitMsg); err != nil {
		slog.Debug("send exit-status failed", "error", err)
	}
}
