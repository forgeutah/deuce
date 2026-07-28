package sshproxy

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// allModes covers every docker-exec shape the proxy builds. A missing
// --user in any one of them puts that channel back on the image's USER,
// which is what produced `fatal: detected dubious ownership` in VS Code
// terminals: the image declares USER root while the workspace tree is
// owned by the devcontainer's remoteUser.
var allModes = []struct {
	name string
	mode execMode
}{
	{"non-pty", execModeNonPTY},
	{"pty-exec", execModePTYExec},
	{"pty-shell", execModePTYShell},
	{"non-pty-shell", execModeNonPTYShell},
	{"sftp", execModeSFTP},
}

func TestDockerArgsRunsAsResolvedUser(t *testing.T) {
	for _, m := range allModes {
		t.Run(m.name, func(t *testing.T) {
			args := dockerArgs("alice", "echo hi", m.mode, "vscode")

			i := slices.Index(args, "--user")
			if i < 0 {
				t.Fatalf("dockerArgs(%s) has no --user: %#v", m.name, args)
			}
			if i+1 >= len(args) || args[i+1] != "vscode" {
				t.Fatalf("dockerArgs(%s) --user value wrong: %#v", m.name, args)
			}
			// The flag belongs to `docker exec`, so it has to precede the
			// container name — after it, docker treats it as part of the
			// in-container command.
			c := slices.Index(args, "alice")
			if c < 0 || i > c {
				t.Errorf("dockerArgs(%s) --user must precede the container: %#v", m.name, args)
			}
		})
	}
}

func TestDockerArgsOmitsUserWhenUnknown(t *testing.T) {
	// No declared user means today's behaviour: let the image's USER stand.
	// Passing an empty --user would make docker reject the exec outright.
	for _, m := range allModes {
		t.Run(m.name, func(t *testing.T) {
			args := dockerArgs("alice", "echo hi", m.mode, "")

			if slices.Contains(args, "--user") {
				t.Errorf("dockerArgs(%s) sent --user with no resolved user: %#v", m.name, args)
			}
			if slices.Contains(args, "") {
				t.Errorf("dockerArgs(%s) has an empty argv slot: %#v", m.name, args)
			}
		})
	}
}

func TestBuildExecCmdPassesUser(t *testing.T) {
	cmd := buildExecCmd(context.Background(), "docker", "alice", "echo hi", execModePTYShell, nil, "vscode")

	if !slices.Contains(cmd.Args, "--user") || !slices.Contains(cmd.Args, "vscode") {
		t.Errorf("buildExecCmd argv missing user: %#v", cmd.Args)
	}
}

func TestBuildSFTPCmdPassesUser(t *testing.T) {
	// SFTP writes files into the workspace, so running it as the wrong user
	// leaves root-owned files in a remoteUser-owned tree.
	cmd := buildSFTPCmd(context.Background(), "docker", "alice", "vscode")

	if !slices.Contains(cmd.Args, "--user") || !slices.Contains(cmd.Args, "vscode") {
		t.Errorf("buildSFTPCmd argv missing user: %#v", cmd.Args)
	}
}

func TestBuildTCPForwardCmdPassesUser(t *testing.T) {
	cmd := buildTCPForwardCmd(context.Background(), "docker", "alice", "127.0.0.1", 8080, "vscode")

	if !slices.Contains(cmd.Args, "--user") || !slices.Contains(cmd.Args, "vscode") {
		t.Errorf("buildTCPForwardCmd argv missing user: %#v", cmd.Args)
	}
	// The forwarding script must survive the added flag intact.
	if !strings.Contains(strings.Join(cmd.Args, " "), "/dev/tcp/127.0.0.1/8080") {
		t.Errorf("buildTCPForwardCmd lost its script: %#v", cmd.Args)
	}
}
