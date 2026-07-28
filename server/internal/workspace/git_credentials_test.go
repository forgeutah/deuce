package workspace

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCredentialFiles_EmptyTokenIsNoop(t *testing.T) {
	dir := t.TempDir()
	env, err := gitCredentialFiles(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != nil {
		t.Fatalf("expected nil env for empty token, got %v", env)
	}
	// No files should be written when there is no token.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files written, got %d", len(entries))
	}
}

func TestGitCredentialFiles_WritesConfigAndCredentials(t *testing.T) {
	dir := t.TempDir()
	const token = "ghp_TESTTOKEN123"

	env, err := gitCredentialFiles(dir, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	credPath := filepath.Join(dir, "git-credentials")
	cfgPath := filepath.Join(dir, "gitconfig")

	// Credentials file: 0600, x-access-token username, github.com host.
	credInfo, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if perm := credInfo.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("credentials perm = %o, want 600", perm)
	}
	credBytes, _ := os.ReadFile(credPath)
	wantLine := "https://x-access-token:" + token + "@github.com"
	if !strings.Contains(string(credBytes), wantLine) {
		t.Errorf("credentials = %q, want to contain %q", credBytes, wantLine)
	}

	// gitconfig points its store helper at the credentials file.
	cfgBytes, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgBytes), "helper = store --file="+credPath) {
		t.Errorf("gitconfig = %q, want store helper referencing %q", cfgBytes, credPath)
	}

	// Env isolates git from the operator's real config.
	wantEnv := map[string]bool{
		"GIT_CONFIG_GLOBAL=" + cfgPath: false,
		"GIT_CONFIG_SYSTEM=/dev/null":  false,
	}
	for _, e := range env {
		if _, ok := wantEnv[e]; ok {
			wantEnv[e] = true
		}
	}
	for e, seen := range wantEnv {
		if !seen {
			t.Errorf("missing env var %q in %v", e, env)
		}
	}
}

// TestGitCredentialFiles_GitResolvesToken exercises the exact call DevPod's
// `devpod agent git-credentials` makes on the host — `git credential fill` —
// and asserts our isolated config feeds it the token. This is the mechanism
// the private-repo clone depends on.
func TestGitCredentialFiles_GitResolvesToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	const token = "ghp_RESOLVETOKEN456"

	env, err := gitCredentialFiles(dir, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := exec.Command("git", "credential", "fill")
	// Only our isolated config should be consulted: GIT_CONFIG_GLOBAL points at
	// the store helper, GIT_CONFIG_SYSTEM=/dev/null suppresses the host's.
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git credential fill failed: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "username=x-access-token") {
		t.Errorf("git did not return x-access-token username; got:\n%s", got)
	}
	if !strings.Contains(got, "password="+token) {
		t.Errorf("git did not return the token as password; got:\n%s", got)
	}
}
