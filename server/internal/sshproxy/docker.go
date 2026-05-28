package sshproxy

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// defaultDockerBin is the docker CLI binary name resolved via $PATH in
// production. Tests can override Server.dockerBin to point at a fake
// helper script. Kept as a constant so the production path remains
// allocation-free.
const defaultDockerBin = "docker"

// Env-var allowlist. Forwarded onto cmd.Env as-is when the request name
// matches. Everything else is silently dropped — see filterEnv. This is
// the only path by which a hostile session-member key can influence the
// docker exec child's environment, so the allowlist is small on purpose.
var envAllowExact = map[string]struct{}{
	"LANG":  {},
	"TERM":  {},
	"HOME":  {},
	"USER":  {},
	"SHELL": {},
}

var envAllowPrefix = []string{
	"VSCODE_",
	"LC_",
}

// envAllowed reports whether the given env-var name passes the allowlist.
// Returns false for everything not exact-matched or prefix-matched, which
// notably includes PATH, LD_PRELOAD, LD_LIBRARY_PATH, PYTHONPATH, etc.
func envAllowed(name string) bool {
	if _, ok := envAllowExact[name]; ok {
		return true
	}
	for _, p := range envAllowPrefix {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// filterEnv returns a deduped, allowlist-filtered slice of "K=V" entries
// suitable for cmd.Env. Later values for the same key win, matching the
// semantics of multiple `env` SSH requests with the same name. Empty or
// disallowed keys are dropped.
func filterEnv(envs []envEntry) []string {
	seen := make(map[string]string, len(envs))
	order := make([]string, 0, len(envs))
	for _, e := range envs {
		if e.Name == "" || !envAllowed(e.Name) {
			continue
		}
		if _, ok := seen[e.Name]; !ok {
			order = append(order, e.Name)
		}
		seen[e.Name] = e.Value
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+seen[k])
	}
	return out
}

// envEntry is a single accepted env request. Stored before the exec/shell
// trigger arrives because crypto/ssh has no typed struct for env payloads.
type envEntry struct {
	Name  string
	Value string
}

// execMode discriminates the docker-exec invocation shapes we use.
type execMode int

const (
	execModeNonPTY      execMode = iota // docker exec -i ... /bin/sh -c <command>
	execModePTYExec                     // docker exec -it ... /bin/sh -c <command>
	execModePTYShell                    // docker exec -it ... /bin/bash -l
	execModeNonPTYShell                 // docker exec -i ... /bin/bash -l (VS Code Remote-SSH sends shell+stdin without pty-req)
	execModeSFTP                        // docker exec -i ... /usr/lib/openssh/sftp-server (U9; not used by U8)
)

// buildExecCmd assembles the *exec.Cmd for a docker exec invocation. The
// command is built with explicit argv — NEVER a shell — so untrusted
// `command` text is delivered as a single argv slot to `/bin/sh -c`
// inside the container. ctx scopes the process lifetime.
//
// `bin` is the docker CLI binary path (typically "docker"). Sets
// SysProcAttr.Setpgid so kill(-pid) reaches the process group, which
// matters because docker exec spawns child processes inside the container
// namespace but the host-side `docker` binary itself may leave grandchild
// monitors around.
func buildExecCmd(ctx context.Context, bin, container, command string, mode execMode, env []string) *exec.Cmd {
	if bin == "" {
		bin = defaultDockerBin
	}
	args := dockerArgs(container, command, mode)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// buildTCPForwardCmd assembles a docker-exec invocation that bridges
// SSH-channel bytes ↔ a loopback TCP socket inside the container, used
// for VS Code Remote-SSH's `direct-tcpip` port forwarding. The bash
// `/dev/tcp/<host>/<port>` builtin opens the socket inside the
// container's network namespace, so no `nc`/`socat` dependency is
// required — bash is already required for the shell-channel mode.
//
// The shell script is:
//
//	exec 3<>/dev/tcp/<host>/<port> || exit 1
//	( cat <&3; kill -TERM $$ 2>/dev/null ) &
//	cat >&3
//
// The backgrounded `cat <&3` pipes container→client; the foreground
// `cat >&3` pipes client→container. Either side closing fires the
// TERM, killing the script, which lets docker exec clean up and the
// SSH channel see EOF.
//
// Caller MUST validate host (loopback-only) and port (1..65535) before
// calling. The script splices them into a bash command verbatim, but
// the inputs go through ssh.Unmarshal of a typed payload struct (host
// is a length-prefixed string, port is a uint32), so the only attack
// surface is the validation rules — not shell-parsing.
func buildTCPForwardCmd(ctx context.Context, bin, container, host string, port uint32) *exec.Cmd {
	if bin == "" {
		bin = defaultDockerBin
	}
	script := "exec 3<>/dev/tcp/" + host + "/" + strconv.FormatUint(uint64(port), 10) + " || exit 1\n" +
		"( cat <&3; kill -TERM $$ 2>/dev/null ) &\n" +
		"cat >&3\n"
	cmd := exec.CommandContext(ctx, bin, "exec", "-i", container, "bash", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// dockerArgs builds the argv slice for a docker exec invocation in the
// given mode. Exposed for unit tests so they can assert the exact argv
// without running docker.
func dockerArgs(container, command string, mode execMode) []string {
	switch mode {
	case execModePTYShell:
		return []string{"exec", "-it", container, "/bin/bash", "-l"}
	case execModeNonPTYShell:
		return []string{"exec", "-i", container, "/bin/bash", "-l"}
	case execModePTYExec:
		return []string{"exec", "-it", container, "/bin/sh", "-c", command}
	case execModeSFTP:
		return []string{"exec", "-i", container, "/usr/lib/openssh/sftp-server", "-e"}
	default: // execModeNonPTY
		return []string{"exec", "-i", container, "/bin/sh", "-c", command}
	}
}
