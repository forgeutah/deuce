package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVSCodeCacheEnabled(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	if m.VSCodeCacheEnabled() {
		t.Error("cache should be off by default")
	}
	m.SetVSCodeCacheDir("/var/lib/deuce/vscode")
	if !m.VSCodeCacheEnabled() {
		t.Error("cache should be on once a directory is configured")
	}
}

func TestCachePathFor(t *testing.T) {
	m := NewManager("devpod", "docker", "")

	if _, err := m.cachePathFor("ws1"); !errors.Is(err, ErrVSCodeCacheDisabled) {
		t.Errorf("expected ErrVSCodeCacheDisabled when unconfigured, got %v", err)
	}

	m.SetVSCodeCacheDir("/var/lib/deuce/vscode")
	got, err := m.cachePathFor("ws1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/var/lib/deuce/vscode/ws1"; got != want {
		t.Errorf("cachePathFor = %q, want %q", got, want)
	}
}

// TestCachePathFor_RejectsTraversal is the containment guarantee: a
// workspace id must never be able to steer writes outside the cache root.
func TestCachePathFor_RejectsTraversal(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	m.SetVSCodeCacheDir("/var/lib/deuce/vscode")

	hostile := []string{
		"../../etc",
		"..",
		"/absolute",
		"ws/../../escape",
		"",
		"with space",
		"semi;colon",
	}
	for _, id := range hostile {
		if got, err := m.cachePathFor(id); err == nil {
			t.Errorf("workspace id %q should be rejected, got path %q", id, got)
		}
	}
}

// TestContainerHome_ResolvesFromPasswd covers why $HOME is not read directly:
// `docker exec` does not set it, so the passwd database is the only reliable
// source for where ~/.vscode-server lives.
func TestContainerHome_ResolvesFromPasswd(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	var gotArgs []string
	m.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("/home/vscode\n"), nil
	}

	home, err := m.containerHome(context.Background(), "devpod-abc", "vscode")
	if err != nil {
		t.Fatal(err)
	}
	if home != "/home/vscode" {
		t.Errorf("home = %q, want /home/vscode", home)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--user vscode") {
		t.Errorf("expected the exec to run as the container user: %v", gotArgs)
	}
	if !strings.Contains(joined, "getent passwd") {
		t.Errorf("expected a passwd lookup rather than $HOME: %v", gotArgs)
	}
}

func TestContainerHome_RejectsImplausibleValues(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	for _, out := range []string{"", "  ", "not-absolute", "/home/$(hostile)", "/home/a b"} {
		m.runner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte(out + "\n"), nil
		}
		if home, err := m.containerHome(context.Background(), "devpod-abc", "vscode"); err == nil {
			t.Errorf("output %q should be rejected, got home %q", out, home)
		}
	}
}

func TestContainerHome_OmitsUserFlagWhenUnknown(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	var gotArgs []string
	m.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("/root\n"), nil
	}
	if _, err := m.containerHome(context.Background(), "devpod-abc", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(gotArgs, " "), "--user") {
		t.Errorf("empty user must not produce a --user flag: %v", gotArgs)
	}
}

// TestRestoreVSCodeServer_CacheMissIsNotAnError covers the first-session
// path: nothing cached yet must be silent, not a logged failure.
func TestRestoreVSCodeServer_CacheMissIsNotAnError(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	m.SetVSCodeCacheDir(t.TempDir())
	m.runner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		t.Error("restore must not touch docker when there is no cached payload")
		return nil, nil
	}
	if err := m.RestoreVSCodeServer(context.Background(), "ws1", nil); err != nil {
		t.Errorf("cache miss should be a no-op, got %v", err)
	}
}

// TestPurgeVSCodeServer removes both the promoted cache and any staging
// directory left by an interrupted save.
func TestPurgeVSCodeServer(t *testing.T) {
	root := t.TempDir()
	m := NewManager("devpod", "docker", "")
	m.SetVSCodeCacheDir(root)

	for _, dir := range []string{"ws1", "ws1.partial"} {
		if err := os.MkdirAll(filepath.Join(root, dir, vscodeServerDirName), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.PurgeVSCodeServer("ws1"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"ws1", "ws1.partial"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone after purge, stat err = %v", dir, err)
		}
	}
}

func TestPurgeVSCodeServer_DisabledIsNoOp(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	if err := m.PurgeVSCodeServer("ws1"); err != nil {
		t.Errorf("purge with the cache disabled should be a no-op, got %v", err)
	}
}

// TestSaveVSCodeServer_PromotesAtomically checks the staging swap: a
// successful save must leave the payload at the final path and no .partial
// directory behind, so a later restore cannot pick up a half-copied tree.
func TestSaveVSCodeServer_PromotesAtomically(t *testing.T) {
	root := t.TempDir()
	m := NewManager("devpod", "docker", "")
	m.SetVSCodeCacheDir(root)

	m.runner = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch {
		case args[0] == "exec":
			return []byte("/home/vscode\n"), nil
		case args[0] == "cp":
			// args: cp -a <src> <stagingDir>
			dst := args[len(args)-1]
			return nil, os.MkdirAll(filepath.Join(dst, vscodeServerDirName), 0o700)
		case args[0] == "image":
			return []byte("[]"), nil
		}
		return []byte("[]"), nil
	}
	// ContainerName/ContainerUser would shell out; short-circuit by seeding
	// the user cache and stubbing the container lookup through the runner.
	m.resolveTargetHook = func(context.Context, string) (string, string, string, error) {
		return "devpod-abc", "vscode", "/home/vscode", nil
	}

	if err := m.SaveVSCodeServer(context.Background(), "ws1", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "ws1", vscodeServerDirName)); err != nil {
		t.Errorf("payload should be promoted to the final path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1.partial")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging directory should be gone after promotion, stat err = %v", err)
	}
}
