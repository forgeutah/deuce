package workspace

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// devpodBuildOutput is a verbatim capture of devpod v0.6.15's closing lines,
// ANSI colour codes and all. Parsing real output rather than a cleaned-up
// approximation is the point: the colour codes are exactly what would break
// a naive prefix match.
const devpodBuildOutput = "\x1b[0;1;37m21:14:26 \x1b[0m\x1b[0;1;36minfo \x1b[0m#6 naming to docker.io/library/deuce-prebuild-probe:devpod-da04665bfb6d8267ff56dcd9e6483d75 done\n" +
	"\x1b[0;1;37m21:14:26 \x1b[0m\x1b[0;1;32mdone \x1b[0mSuccessfully build image deuce-prebuild-probe:devpod-da04665bfb6d8267ff56dcd9e6483d75\n" +
	"\x1b[0;1;37m21:14:26 \x1b[0m\x1b[0;1;36minfo \x1b[0mDeleting container...\n"

// TestPrebuildImageRE_ParsesRealDevpodOutput locks in tag extraction against
// the actual log format, including the ANSI stripping it depends on.
func TestPrebuildImageRE_ParsesRealDevpodOutput(t *testing.T) {
	var got string
	for _, raw := range strings.Split(devpodBuildOutput, "\n") {
		line := ansiRE.ReplaceAllString(raw, "")
		if m := prebuildImageRE.FindStringSubmatch(line); m != nil {
			got = m[1]
		}
	}
	want := "deuce-prebuild-probe:devpod-da04665bfb6d8267ff56dcd9e6483d75"
	if got != want {
		t.Errorf("parsed tag = %q, want %q", got, want)
	}
	if !validImageRef.MatchString(got) {
		t.Errorf("parsed tag %q failed validImageRef", got)
	}
}

// TestPrebuildImageRE_NoMatchLeavesEmpty confirms unrelated output does not
// yield a bogus tag — the caller treats empty as ErrPrebuildTagNotFound and
// falls back rather than passing garbage to docker.
func TestPrebuildImageRE_NoMatchLeavesEmpty(t *testing.T) {
	for _, line := range []string{
		"info Building devcontainer...",
		"error failed to solve: process did not complete successfully",
		"",
	} {
		if m := prebuildImageRE.FindStringSubmatch(line); m != nil {
			t.Errorf("line %q unexpectedly matched, got %q", line, m[1])
		}
	}
}

func TestBakedTag(t *testing.T) {
	tests := []struct {
		name     string
		prebuild string
		want     string
		wantErr  bool
	}{
		{
			name:     "bare repository",
			prebuild: "deuce-prebuild:devpod-da04665bfb6d8267ff56dcd9e6483d75",
			want:     "deuce-prebuild:deuce-da04665bfb6d8267ff56dcd9e6483d75",
		},
		{
			name:     "registry path",
			prebuild: "ghcr.io/forgeutah/deuce-prebuild:devpod-abc123",
			want:     "ghcr.io/forgeutah/deuce-prebuild:deuce-abc123",
		},
		{
			// The colon in a host:port must not be mistaken for the tag
			// separator — LastIndex is what makes this work.
			name:     "registry with port",
			prebuild: "localhost:5000/deuce-prebuild:devpod-abc123",
			want:     "localhost:5000/deuce-prebuild:deuce-abc123",
		},
		{
			name:     "no tag at all",
			prebuild: "deuce-prebuild",
			wantErr:  true,
		},
		{
			// devpod changed its tagging scheme: better to fail and fall
			// back than to bake onto an image we cannot key correctly.
			name:     "unexpected tag prefix",
			prebuild: "deuce-prebuild:latest",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bakedTag(tt.prebuild)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("bakedTag(%q) = %q, want %q", tt.prebuild, got, tt.want)
			}
		})
	}
}

// TestBakedTag_PreservesHash is the staleness guarantee: the baked tag must
// carry the devcontainer-definition hash, so a definition change yields a new
// devpod tag AND a new baked tag, while an ordinary code push changes neither.
func TestBakedTag_PreservesHash(t *testing.T) {
	before, err := bakedTag("repo:devpod-hash-one")
	if err != nil {
		t.Fatal(err)
	}
	after, err := bakedTag("repo:devpod-hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("different definition hashes produced the same baked tag")
	}
	same, err := bakedTag("repo:devpod-hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if same != before {
		t.Errorf("same definition hash produced different baked tags: %q vs %q", before, same)
	}
}

func TestValidImageRef(t *testing.T) {
	valid := []string{
		"deuce-prebuild:devpod-abc123",
		"ghcr.io/forgeutah/deuce-prebuild:deuce-abc123",
		"localhost:5000/deuce-prebuild:devpod-abc",
	}
	for _, ref := range valid {
		if !validImageRef.MatchString(ref) {
			t.Errorf("expected %q to be valid", ref)
		}
	}

	invalid := []string{
		"",
		"no-tag",
		"--build-arg=evil:tag", // flag-shaped
		"repo:tag with space",  // shell-meta
		"repo:tag;rm -rf /",    // shell-meta
		"repo:tag\nsecond",     // embedded newline
		"repo:$(hostile)",      // command substitution
	}
	for _, ref := range invalid {
		if validImageRef.MatchString(ref) {
			t.Errorf("expected %q to be rejected", ref)
		}
	}
}

// TestDevpodUpArgs covers the plan's "argv assembly with and without
// prebuild" scenario: an empty image must leave the command byte-identical
// to the pre-prebuild behaviour.
func TestDevpodUpArgs(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		image    string
		want     []string
	}{
		{
			name:     "prebuild disabled keeps the original argv",
			provider: "docker",
			image:    "",
			want:     []string{"up", "https://example.com/r.git", "--id", "ws1", "--ide", "none", "--provider", "docker"},
		},
		{
			name:     "prebuild enabled appends the image override",
			provider: "docker",
			image:    "repo:deuce-abc",
			want:     []string{"up", "https://example.com/r.git", "--id", "ws1", "--ide", "none", "--provider", "docker", "--devcontainer-image", "repo:deuce-abc"},
		},
		{
			name:     "empty provider is omitted",
			provider: "",
			image:    "repo:deuce-abc",
			want:     []string{"up", "https://example.com/r.git", "--id", "ws1", "--ide", "none", "--devcontainer-image", "repo:deuce-abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := devpodUpArgs("ws1", "https://example.com/r.git", tt.provider, tt.image)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestDevpodBuildArgs(t *testing.T) {
	got := devpodBuildArgs("https://example.com/r.git", "deuce-prebuild", "docker")
	want := []string{"build", "https://example.com/r.git", "--repository", "deuce-prebuild", "--skip-push", "--provider", "docker"}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}

	// --skip-push is what keeps the cache local; losing it would make every
	// create attempt a registry push.
	if !slices.Contains(got, "--skip-push") {
		t.Error("expected --skip-push in devpod build argv")
	}
}

// TestEnsurePrebuild_DisabledIsAnError confirms the guard: callers must check
// prebuildRepo before calling, and a misconfigured manager cannot silently
// shell out to `devpod build` with an empty --repository.
func TestEnsurePrebuild_DisabledIsAnError(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	if m.PrebuildEnabled() {
		t.Fatal("a fresh manager should have the prebuild cache off")
	}
	if _, err := m.EnsurePrebuild(context.Background(), "https://example.com/r.git", nil); err == nil {
		t.Fatal("expected an error when no prebuild repository is configured")
	}
}

func TestSetPrebuildRepository(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	m.SetPrebuildRepository("deuce-prebuild")
	if !m.PrebuildEnabled() {
		t.Error("expected prebuild to be enabled after SetPrebuildRepository")
	}
	m.SetPrebuildRepository("")
	if m.PrebuildEnabled() {
		t.Error("expected an empty repository to disable prebuild")
	}
}

// TestImageExists_UsesRunnerSeam checks the cache-hit decision without
// shelling out to docker, and confirms a docker failure reads as "absent"
// (which costs a rebuild) rather than as "present" (which would pass a
// nonexistent image to devpod up).
func TestImageExists_UsesRunnerSeam(t *testing.T) {
	var gotArgs []string
	m := NewManager("devpod", "docker", "")

	m.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("[]"), nil
	}
	if !m.imageExists(context.Background(), "repo:deuce-abc") {
		t.Error("expected imageExists to report true when docker inspect succeeds")
	}
	want := []string{"docker", "image", "inspect", "repo:deuce-abc"}
	if !slices.Equal(gotArgs, want) {
		t.Errorf("got  %v\nwant %v", gotArgs, want)
	}

	m.runner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("No such image")
	}
	if m.imageExists(context.Background(), "repo:deuce-abc") {
		t.Error("expected imageExists to report false when docker inspect fails")
	}
}

// TestImageUser_ResolvesRemoteUser covers the bug this guards against:
// installing Pi as root when the session runs as the devcontainer's
// remoteUser would put the binary somewhere the agent cannot reach.
func TestImageUser_ResolvesRemoteUser(t *testing.T) {
	m := NewManager("devpod", "docker", "")
	m.runner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`[{"id":"ghcr.io/devcontainers/features/git:1"},{"remoteUser":"vscode"}]` + "\n"), nil
	}
	if got := m.imageUser(context.Background(), "repo:devpod-abc"); got != "vscode" {
		t.Errorf("imageUser = %q, want %q", got, "vscode")
	}

	// A docker failure must degrade to "" so bakeAgentTools falls back to
	// the image's own USER rather than guessing.
	m.runner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("no such image")
	}
	if got := m.imageUser(context.Background(), "repo:devpod-abc"); got != "" {
		t.Errorf("imageUser on error = %q, want empty", got)
	}
}

// TestWorkspaceImageDockerfile_ResolvesHomePerRun guards the subtlest part of
// the baked layer: Docker's USER directive changes the uid but not $HOME, so
// every RUN must resolve HOME from the passwd database. A regression here
// installs Pi into /root and the agent silently fails to launch.
func TestWorkspaceImageDockerfile_ResolvesHomePerRun(t *testing.T) {
	runCount := strings.Count(workspaceImageDockerfile, "\nRUN ")
	homeResolutions := strings.Count(workspaceImageDockerfile, `getent passwd "$(id -un)"`)
	if homeResolutions < 3 {
		t.Errorf("expected each install RUN to resolve HOME from passwd, found %d resolutions across %d RUN steps",
			homeResolutions, runCount)
	}
	if strings.Contains(workspaceImageDockerfile, "ARG BASE") == false {
		t.Error("Dockerfile must accept a BASE build arg")
	}
	// The layer must drop out of root before installing, otherwise the
	// files land with the wrong ownership.
	lines := strings.Split(workspaceImageDockerfile, "\n")
	userLine, installLine := -1, -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if userLine < 0 && trimmed == "USER ${REMOTE_USER}" {
			userLine = i
		}
		// Match the invocation, not the COPY that puts the script in place.
		if installLine < 0 && strings.HasPrefix(trimmed, "sh /tmp/deuce-build/pi-install.sh") {
			installLine = i
		}
	}
	if userLine < 0 {
		t.Fatal("Dockerfile never switches to ${REMOTE_USER}")
	}
	if installLine < 0 {
		t.Fatal("Dockerfile never invokes pi-install.sh")
	}
	if userLine > installLine {
		t.Errorf("USER ${REMOTE_USER} (line %d) must precede the Pi install (line %d), else Pi installs as root",
			userLine, installLine)
	}
}
