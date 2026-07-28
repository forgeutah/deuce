package sshproxy

import (
	"context"
	"os/exec"
	"syscall"
)

// SFTP subsystem proxy.
//
// SFTP rides SSH session-channel framing (RFC 4254 §6.5, draft-ietf-secsh-
// filexfer): once the peer asks for `subsystem sftp` and we reply success,
// the channel byte stream IS the SFTP wire protocol. So we just shovel
// bytes between the SSH channel and an in-container `sftp-server` child's
// stdio. No protocol parsing on our side.
//
// The integration with the per-channel handler in session.go is
// deliberately minimal: the `reqSubsystem` case routes to
// `startProc(execModeSFTP, "")`, which reuses the same non-PTY
// stdin/stdout/stderr pipe pumps as `exec` mode. The only SFTP-specific
// tweaks are:
//
//   - The docker argv is `exec -i <container> /usr/lib/openssh/sftp-server
//     -e` (built by `dockerArgs(execModeSFTP)` in docker.go). NEVER `-t`
//     — SFTP is binary framing and a PTY would CRLF-translate the first
//     few packets, producing a notorious "Couldn't read packet: Connection
//     reset by peer" symptom that's mis-diagnosed as a network issue half
//     the time.
//   - stderr from the in-container sftp-server (forced on by `-e`) is
//     discarded rather than mirrored to `ch.Stderr()`. Some SFTP clients
//     don't drain stderr promptly, which would otherwise wedge the pump.
//     Future: route to a structured-log writer prefixed with session id.
//
// Path probing: the DevPod default base image is Debian, so we hardcode
// `/usr/lib/openssh/sftp-server`. RHEL/Alpine ship the binary at
// `/usr/libexec/openssh/sftp-server`; on those images the exec child
// fails immediately with a non-zero status that propagates back to the
// client. Documented in CLAUDE.md and the SSH proxy plan as a v1
// limitation. The devcontainer install instructions point at the Debian
// package name (`openssh-sftp-server`).

// buildSFTPCmd assembles the `docker exec -i <container>
// /usr/lib/openssh/sftp-server -e` invocation under ctx. Exposed at
// package scope (unexported) so unit tests can assert the exact argv
// without booting the full subsystem handler.
//
// Notes:
//   - dockerBin "" falls back to defaultDockerBin ("docker") via $PATH.
//   - SysProcAttr.Setpgid: true so `kill(-pid, ...)` reaches the docker
//     CLI child and its grandchildren — matches the U8 exec lifecycle.
//   - No env is forwarded. SFTP doesn't honor client-supplied env vars,
//     and the U8 allowlist would just be noise here.
//   - argv MUST be `-i`, never `-it`. See file-level comment above.
func buildSFTPCmd(ctx context.Context, dockerBin, container, user string) *exec.Cmd {
	if dockerBin == "" {
		dockerBin = defaultDockerBin
	}
	cmd := exec.CommandContext(ctx, dockerBin, dockerArgs(container, "", execModeSFTP, user)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
