package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnsurePrebuild_EndToEnd exercises the real thing: `devpod build`, the
// baked Deuce layer, the cache hit on a second call, and `devpod up
// --devcontainer-image` producing a container that actually has Pi on PATH
// as the devcontainer's remoteUser.
//
// Opt-in because it shells out to devpod and docker, pulls a base image, and
// downloads Node and Pi over the network — minutes, not milliseconds. Run it
// after touching prebuild.go or the baked Dockerfile:
//
//	DEUCE_PREBUILD_E2E=1 go test ./internal/workspace/ -run EndToEnd -v -timeout 20m
func TestEnsurePrebuild_EndToEnd(t *testing.T) {
	if os.Getenv("DEUCE_PREBUILD_E2E") == "" {
		t.Skip("set DEUCE_PREBUILD_E2E=1 to run (shells out to devpod + docker, needs network)")
	}
	for _, bin := range []string{"devpod", "docker"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}

	// A devcontainer with a build step: devpod skips prebuild entirely for a
	// plain "image" devcontainer, so an image-only fixture would pass
	// without exercising anything.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, ".devcontainer", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", "FROM mcr.microsoft.com/devcontainers/base:ubuntu\nRUN echo e2e > /etc/deuce-e2e-marker\n")
	write("devcontainer.json", `{"name":"deuce-e2e","build":{"dockerfile":"Dockerfile"},"remoteUser":"vscode"}`)

	const repository = "deuce-prebuild-e2e"
	m := NewManager("devpod", "docker", "")
	m.SetPrebuildRepository(repository)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	logFn := func(line string) { t.Log(line) }

	res, err := m.EnsurePrebuild(ctx, dir, logFn)
	if err != nil {
		t.Fatalf("EnsurePrebuild: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "image", "rm", "-f", res.Image).Run()
	})
	if !res.ToolsBaked {
		t.Fatal("expected ToolsBaked — the baked layer failed and silently degraded")
	}
	if !strings.Contains(res.Image, ":"+deuceTagPrefix) {
		t.Fatalf("expected a Deuce-tagged image, got %q", res.Image)
	}

	// Second call must hit the cache: same tag, and no rebuild.
	second, err := m.EnsurePrebuild(ctx, dir, logFn)
	if err != nil {
		t.Fatalf("second EnsurePrebuild: %v", err)
	}
	if second.Image != res.Image || !second.ToolsBaked {
		t.Errorf("cache miss on second call: first=%+v second=%+v", res, second)
	}

	// The payoff: a container started from the baked image must have Pi
	// runnable as the remoteUser, with no over-ssh install.
	const wsID = "deuce-prebuild-e2e-ws"
	t.Cleanup(func() {
		_ = exec.Command("devpod", "delete", wsID, "--force", "--ignore-not-found").Run()
	})
	if _, err := m.Create(ctx, wsID, dir, logFn); err != nil {
		t.Fatalf("Create from baked image: %v", err)
	}

	checks := map[string]string{
		"pi on PATH":         piLoginShell("pi --version"),
		"ask-user extension": `test -f "$HOME/.pi/agent/extensions/ask-user.ts" && echo present`,
		"remote user":        "whoami",
		"base layer intact":  "cat /etc/deuce-e2e-marker",
	}
	for name, cmd := range checks {
		out, err := m.ExecInWorkspace(ctx, wsID, cmd).CombinedOutput()
		got := strings.TrimSpace(string(out))
		if err != nil {
			t.Errorf("%s: %v (output: %s)", name, err, got)
			continue
		}
		t.Logf("%s -> %s", name, got)
		if got == "" {
			t.Errorf("%s produced no output", name)
		}
	}
}
