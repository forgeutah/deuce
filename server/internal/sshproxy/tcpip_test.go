package sshproxy

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestValidateLoopbackDest_AcceptsLiterals pins the three forms VS Code
// Remote-SSH actually sends. Keeping these in a table makes it obvious
// what's covered if a future contributor wants to add IPv6 brackets,
// 127.0.0.* range, etc.
func TestValidateLoopbackDest_AcceptsLiterals(t *testing.T) {
	cases := []struct {
		host string
		port uint32
	}{
		{"localhost", 1},
		{"localhost", 40301},
		{"127.0.0.1", 22},
		{"127.0.0.1", 65535},
		{"::1", 8080},
	}
	for _, c := range cases {
		if err := validateLoopbackDest(c.host, c.port); err != nil {
			t.Errorf("validateLoopbackDest(%q, %d): want nil, got %v", c.host, c.port, err)
		}
	}
}

// TestValidateLoopbackDest_RejectsEverythingElse locks in the policy:
// only the three exact loopback literals pass. No DNS, no ranges, no
// IPv4-mapped IPv6, no other "looks loopback-ish" inputs. The threat
// is an authenticated user pivoting onto the host network or sidecars,
// so the rule MUST be tight.
func TestValidateLoopbackDest_RejectsEverythingElse(t *testing.T) {
	hostileHosts := []string{
		"",                        // empty
		"0.0.0.0",                 // INADDR_ANY — listening != loopback
		"127.0.0.2",               // technically loopback range but not the literal
		"10.0.0.1",                // RFC1918
		"169.254.169.254",         // AWS / GCE metadata service
		"example.com",             // any DNS name
		"LOCALHOST",               // uppercase rejected — exact match only
		"localhost.localdomain",   // FQDN variant
		"::ffff:127.0.0.1",        // IPv4-mapped IPv6 form
		"[::1]",                   // bracketed (URL-style)
		"127.0.0.1\nattacker.com", // injection attempt
		"127.0.0.1 attacker.com",  // injection attempt with space
		"localhost; rm -rf /",     // shell metachar (defense-in-depth)
	}
	for _, host := range hostileHosts {
		if err := validateLoopbackDest(host, 8080); err == nil {
			t.Errorf("validateLoopbackDest(%q): want rejection, got nil", host)
		}
	}
}

// TestValidateLoopbackDest_RejectsBadPorts covers the port-range half
// of the validator. Port 0 is unusable as a destination; >65535 is
// unrepresentable anyway but the typed payload allows 32 bits.
func TestValidateLoopbackDest_RejectsBadPorts(t *testing.T) {
	for _, p := range []uint32{0, 65536, 1 << 31} {
		if err := validateLoopbackDest("127.0.0.1", p); err == nil {
			t.Errorf("port %d: want rejection, got nil", p)
		}
	}
}

// TestBuildTCPForwardCmd_Argv pins the docker-exec invocation shape.
// The script body is asserted against substrings rather than the full
// string so an unrelated whitespace tweak doesn't churn the test, but
// the structurally important bits — /dev/tcp path, kill -TERM, both
// directions — are all locked.
func TestBuildTCPForwardCmd_Argv(t *testing.T) {
	ctx := context.Background()
	cmd := buildTCPForwardCmd(ctx, "/usr/bin/docker", "alice", "127.0.0.1", 40301)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Errorf("expected Setpgid: true, got %#v", cmd.SysProcAttr)
	}

	// argv shape: docker exec -i <container> bash -c <script>
	if len(cmd.Args) != 7 {
		t.Fatalf("argv length: want 7, got %d (%v)", len(cmd.Args), cmd.Args)
	}
	want := []string{"/usr/bin/docker", "exec", "-i", "alice", "bash", "-c"}
	if !reflect.DeepEqual(cmd.Args[:6], want) {
		t.Errorf("argv prefix:\n got: %#v\nwant: %#v", cmd.Args[:6], want)
	}

	script := cmd.Args[6]
	mustContain := []string{
		"exec 3<>/dev/tcp/127.0.0.1/40301",
		"|| exit 1",
		"cat <&3",
		"cat >&3",
		"kill -TERM $$",
	}
	for _, sub := range mustContain {
		if !strings.Contains(script, sub) {
			t.Errorf("script missing %q\nfull script:\n%s", sub, script)
		}
	}
}

// TestBuildTCPForwardCmd_EmptyBinUsesDefault mirrors
// TestBuildExecCmd_EmptyBinUsesDefault — empty `bin` should resolve to
// the package default ("docker" from $PATH).
func TestBuildTCPForwardCmd_EmptyBinUsesDefault(t *testing.T) {
	cmd := buildTCPForwardCmd(context.Background(), "", "alice", "127.0.0.1", 22)
	if len(cmd.Args) < 1 {
		t.Fatalf("empty argv: %v", cmd.Args)
	}
	if !strings.HasSuffix(cmd.Args[0], "docker") {
		t.Errorf("argv[0] should end in 'docker', got %q", cmd.Args[0])
	}
}
