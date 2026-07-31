package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitInit builds a throwaway repository with one staged file and one untracked
// file, so loadGitStatus has something to report either way.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
}

// TestLoadGitStatus_IgnoresRepoPlantedFsmonitor is a security regression test.
//
// Git executes core.fsmonitor as a command and honours it from the
// repository's own .git/config. Workspace repositories are not trusted: anyone
// with terminal access to a session, or the agent itself, can write into
// .git/config. Because the server suppresses git's dubious-ownership check in
// order to read workspaces at all, that planted value would otherwise run here
// — in the server process, which on a socket-mounted deployment can reach the
// host Docker daemon.
//
// The test plants the config the way an attacker would and asserts the command
// never ran. It fails if the -c override is dropped from the git invocation.
func TestLoadGitStatus_IgnoresRepoPlantedFsmonitor(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	gitInit(t, repo)

	// Sentinel lives outside the repo so a stray `git clean` can't mask it.
	sentinel := filepath.Join(t.TempDir(), "fsmonitor-ran")
	cmd := exec.Command("git", "config", "core.fsmonitor",
		"sh -c 'printf executed > "+sentinel+"; echo'")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("planting fsmonitor config: %v: %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	statusByPath := map[string]string{}
	if err := loadGitStatus(ctx, repo, "", statusByPath); err != nil {
		t.Fatalf("loadGitStatus: %v", err)
	}

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("core.fsmonitor from repository config was executed by the server process; " +
			"the -c core.fsmonitor=false override has been lost from gitStatusArgs")
	} else if !os.IsNotExist(err) {
		t.Fatalf("checking sentinel: %v", err)
	}

	// The override must not cost us the actual feature.
	if got := statusByPath["staged.txt"]; got == "" {
		t.Errorf("expected a status for staged.txt, got none (statuses: %v)", statusByPath)
	}
	if got := statusByPath["untracked.txt"]; got == "" {
		t.Errorf("expected a status for untracked.txt, got none (statuses: %v)", statusByPath)
	}
}

// TestLoadGitStatus_ReportsStatusesUnderRepoRoot covers the sub-repo path,
// where keys are prefixed with the repo root relative to the workspace.
func TestLoadGitStatus_ReportsStatusesUnderRepoRoot(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	sub := filepath.Join(workspace, "packages", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, sub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	statusByPath := map[string]string{}
	if err := loadGitStatus(ctx, workspace, "packages/api", statusByPath); err != nil {
		t.Fatalf("loadGitStatus: %v", err)
	}

	if _, ok := statusByPath["packages/api/staged.txt"]; !ok {
		t.Errorf("expected key prefixed with the repo root, got: %v", statusByPath)
	}
}
