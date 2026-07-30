package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/forgeutah/deuce/server/internal/agent/pirun/extension"
)

// ErrPrebuildTagNotFound is returned when `devpod build` succeeded but its
// output did not carry the image tag line we parse. Callers degrade to the
// original from-scratch path rather than failing the session.
var ErrPrebuildTagNotFound = errors.New("could not parse prebuild image tag from devpod build output")

// ansiRE strips the SGR colour codes devpod writes in its default `plain`
// log format, so tag parsing sees the bare text.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// prebuildImageRE captures the tag from devpod's closing build line:
//
//	done Successfully build image deuce-prebuild:devpod-da04665bfb...
//
// Parsing a log line is fragile by nature, so every failure here is
// non-fatal: EnsurePrebuild returns an error and Create falls back to the
// original behaviour. A devpod upgrade that changes this wording costs the
// cache, not the session.
var prebuildImageRE = regexp.MustCompile(`Successfully build image (\S+)`)

// validImageRef bounds what may reach `docker build -t` and `devpod up
// --devcontainer-image` argv. The value originates in devpod's stdout, which
// is not attacker-controlled in any path we know of — this is the same
// defence-in-depth posture as validContainerName, not the only barrier.
var validImageRef = regexp.MustCompile(`^[a-z0-9]+([._\-/][a-z0-9]+)*(:[0-9]+)?(/[a-zA-Z0-9._\-]+)*:[a-zA-Z0-9._\-]+$`)

// devpodTagPrefix is the tag prefix devpod gives a prebuild image; the
// remainder is the hash of the devcontainer definition. Deuce republishes
// its own baked layer under deuceTagPrefix + the same hash, so the two
// images stay paired and both invalidate when the definition changes.
const (
	devpodTagPrefix = "devpod-"
	deuceTagPrefix  = "deuce-"
)

// PrebuildResult describes the image a session should start from.
type PrebuildResult struct {
	// Image is the tag to pass to `devpod up --devcontainer-image`.
	Image string

	// ToolsBaked reports whether Image already contains Pi, the
	// pi-subagents package and the ask-user extension. False means the
	// caller must still run the over-ssh provisioning path — the
	// devcontainer build was cached but the tooling layer was not.
	ToolsBaked bool
}

// SetPrebuildRepository turns on the prebuild cache for this manager. An
// empty repo (the default) leaves Create on its original from-scratch path.
func (m *Manager) SetPrebuildRepository(repo string) {
	m.prebuildRepo = repo
}

// PrebuildEnabled reports whether a prebuild repository is configured.
func (m *Manager) PrebuildEnabled() bool {
	return m.prebuildRepo != ""
}

// EnsurePrebuild makes sure a Deuce-baked devcontainer image exists for
// repoURL and returns the tag to start sessions from.
//
// Two steps, both cached:
//
//  1. `devpod build --repository <repo> --skip-push` builds the repo's own
//     devcontainer once and tags it <repo>:devpod-<hash>, where <hash> is
//     devpod's hash of the devcontainer definition. Re-running is cheap when
//     the definition is unchanged (BuildKit cache hit), and produces a new
//     hash when it changes — which is what gives the cache its staleness
//     behaviour for free.
//  2. A thin Deuce layer bakes Pi, pi-subagents and the ask-user extension
//     on top, tagged <repo>:deuce-<hash>. Skipped entirely when that tag
//     already exists locally.
//
// Note this runs on every create, so the repo is still cloned once by
// `devpod build` — the win is skipping the image build and the Pi install,
// not the clone. Making step 1 conditional would mean caching the hash,
// which cannot be computed without the clone that produces it.
//
// A failure to bake is not a failure to start: the devpod prebuild image is
// returned with ToolsBaked false so the caller provisions over ssh as before.
func (m *Manager) EnsurePrebuild(ctx context.Context, repoURL string, logFn LogFunc) (PrebuildResult, error) {
	if m.prebuildRepo == "" {
		return PrebuildResult{}, errors.New("prebuild repository not configured")
	}

	base, err := m.buildDevcontainerImage(ctx, repoURL, logFn)
	if err != nil {
		return PrebuildResult{}, err
	}

	baked, err := bakedTag(base)
	if err != nil {
		return PrebuildResult{}, err
	}

	if m.imageExists(ctx, baked) {
		slog.Info("using cached baked prebuild image", "repo", repoURL, "image", baked)
		if logFn != nil {
			logFn(fmt.Sprintf("Using cached workspace image %s", baked))
		}
		return PrebuildResult{Image: baked, ToolsBaked: true}, nil
	}

	if err := m.bakeAgentTools(ctx, base, baked, logFn); err != nil {
		// Degrade, don't fail: the devcontainer build is still cached, and
		// the caller's over-ssh provisioning fills in the tooling.
		slog.Warn("baking agent tools into prebuild image failed; falling back to over-ssh provisioning",
			"repo", repoURL, "base", base, "error", err)
		if logFn != nil {
			logFn("WARNING: could not bake Pi into the workspace image — falling back to installing it after start")
		}
		return PrebuildResult{Image: base, ToolsBaked: false}, nil
	}

	return PrebuildResult{Image: baked, ToolsBaked: true}, nil
}

// buildDevcontainerImage runs `devpod build` and returns the tag it reports.
func (m *Manager) buildDevcontainerImage(ctx context.Context, repoURL string, logFn LogFunc) (string, error) {
	args := devpodBuildArgs(repoURL, m.prebuildRepo, m.provider)

	slog.Info("building devcontainer prebuild image", "repo", repoURL, "repository", m.prebuildRepo)
	if logFn != nil {
		logFn("Preparing workspace image (cached after the first build)...")
	}

	cmd := exec.CommandContext(ctx, m.bin, args...)
	// Same clone-time credential story as Create — `devpod build` clones the
	// repo too, so private repos need the isolated credential store here.
	if gitEnv := m.gitCredentialEnv(); gitEnv != nil {
		cmd.Env = append(os.Environ(), gitEnv...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("devpod build start: %w", err)
	}

	var tag string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := ansiRE.ReplaceAllString(scanner.Text(), "")
		slog.Debug("devpod build output", "repo", repoURL, "line", line)
		if logFn != nil {
			logFn(line)
		}
		// Last match wins: devpod names several intermediate images during
		// the build and reports the final one on its closing line.
		if match := prebuildImageRE.FindStringSubmatch(line); match != nil {
			tag = match[1]
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("devpod build failed: %w", err)
	}
	if tag == "" || !validImageRef.MatchString(tag) {
		return "", fmt.Errorf("%w (got %q)", ErrPrebuildTagNotFound, tag)
	}

	slog.Info("devcontainer prebuild image ready", "repo", repoURL, "image", tag)
	return tag, nil
}

// devpodBuildArgs assembles the `devpod build` argv. --skip-push keeps the
// resulting tag local to the Docker daemon: the image is consumed by tag via
// --devcontainer-image, so no registry is involved.
func devpodBuildArgs(repoURL, repository, provider string) []string {
	args := []string{"build", repoURL, "--repository", repository, "--skip-push"}
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	return args
}

// bakedTag maps devpod's prebuild tag to the tag Deuce bakes its own layer
// under, preserving the definition hash so both invalidate together.
func bakedTag(prebuild string) (string, error) {
	idx := strings.LastIndex(prebuild, ":")
	if idx < 0 {
		return "", fmt.Errorf("prebuild image %q has no tag", prebuild)
	}
	repo, tag := prebuild[:idx], prebuild[idx+1:]
	hash, ok := strings.CutPrefix(tag, devpodTagPrefix)
	if !ok {
		return "", fmt.Errorf("prebuild image tag %q does not start with %q", tag, devpodTagPrefix)
	}
	return repo + ":" + deuceTagPrefix + hash, nil
}

// imageExists reports whether the Docker daemon already holds the tag.
func (m *Manager) imageExists(ctx context.Context, image string) bool {
	_, err := m.runner(ctx, "docker", "image", "inspect", image)
	return err == nil
}

// imageUser resolves the user the baked layer's RUN steps should execute as,
// from the same merged `devcontainer.metadata` label ContainerUser reads.
// Installing as root would put Pi in /root, where the session's remoteUser
// cannot reach it. Empty means "leave the image's USER alone".
func (m *Manager) imageUser(ctx context.Context, image string) string {
	out, err := m.runner(ctx, "docker", "image", "inspect",
		"--format", `{{index .Config.Labels "devcontainer.metadata"}}`, image)
	if err != nil {
		slog.Warn("could not read devcontainer metadata from prebuild image", "image", image, "error", err)
		return ""
	}
	return execUserFromMetadata(strings.TrimSpace(string(out)))
}

// imageDefaultUser returns the image's own USER directive, so the baked
// layer can restore it rather than silently changing the image's default.
func (m *Manager) imageDefaultUser(ctx context.Context, image string) string {
	out, err := m.runner(ctx, "docker", "image", "inspect", "--format", `{{.Config.User}}`, image)
	if err != nil {
		return ""
	}
	user := strings.TrimSpace(string(out))
	if !validExecUser.MatchString(user) {
		return ""
	}
	return user
}

// bakeAgentTools builds the thin Deuce layer on top of base and tags it
// baked. The build context is generated on the fly from the same Go
// constants the over-ssh installers use, so the two paths cannot drift.
func (m *Manager) bakeAgentTools(ctx context.Context, base, baked string, logFn LogFunc) error {
	runAs := m.imageUser(ctx, base)
	if runAs == "" {
		// No declared remoteUser: install as whoever the image runs as.
		runAs = m.imageDefaultUser(ctx, base)
	}
	if runAs == "" {
		runAs = "root"
	}
	finalUser := m.imageDefaultUser(ctx, base)
	if finalUser == "" {
		finalUser = "root"
	}

	dir, err := os.MkdirTemp("", "deuce-workspace-image-")
	if err != nil {
		return fmt.Errorf("create build context: %w", err)
	}
	defer os.RemoveAll(dir)

	files := map[string]string{
		"Dockerfile":              workspaceImageDockerfile,
		"pi-install.sh":           piInstallScript,
		extension.AskUserFilename: extension.AskUser,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	slog.Info("baking agent tools into prebuild image", "base", base, "baked", baked, "user", runAs)
	if logFn != nil {
		logFn("Baking Pi and agent tooling into the workspace image...")
	}

	args := []string{
		"build",
		"--build-arg", "BASE=" + base,
		"--build-arg", "REMOTE_USER=" + runAs,
		"--build-arg", "FINAL_USER=" + finalUser,
		"--build-arg", "PI_SUBAGENTS=" + PiSubagentsPackage,
		"--build-arg", "ASK_USER_FILE=" + extension.AskUserFilename,
		"-t", baked,
		dir,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker build start: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("workspace image build output", "line", line)
		if logFn != nil {
			logFn(line)
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	if logFn != nil {
		logFn(fmt.Sprintf("Workspace image ready: %s", baked))
	}
	return nil
}

// workspaceImageDockerfile bakes the agent harness onto a devpod-built
// devcontainer image. It is a Go constant rather than a file under deploy/
// so a deployed `deuce` binary carries it — the plan named a deploy/
// directory, but that would make the running server depend on the repo
// layout being present next to it.
//
// HOME is resolved per RUN from the passwd database: Docker's USER directive
// changes the uid but not $HOME, so a bare "$HOME" here would resolve to
// root's home and install Pi where the session user cannot reach it.
const workspaceImageDockerfile = `# syntax=docker/dockerfile:1
ARG BASE
FROM ${BASE}

ARG REMOTE_USER=root
ARG FINAL_USER=root
ARG PI_SUBAGENTS
ARG ASK_USER_FILE

USER root
COPY pi-install.sh /tmp/deuce-build/pi-install.sh
COPY ${ASK_USER_FILE} /tmp/deuce-build/${ASK_USER_FILE}
RUN chmod 0755 /tmp/deuce-build/pi-install.sh && chmod -R a+rX /tmp/deuce-build

USER ${REMOTE_USER}

# Pi itself, via the same script the over-ssh installer uses.
RUN set -eu; \
    H="$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f6 || true)"; \
    [ -n "$H" ] || H="$HOME"; \
    [ -n "$H" ] || H=/root; \
    export HOME="$H"; \
    sh /tmp/deuce-build/pi-install.sh

# The subagents package, registered in Pi's own settings.
RUN set -eu; \
    H="$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f6 || true)"; \
    [ -n "$H" ] || H="$HOME"; \
    [ -n "$H" ] || H=/root; \
    export HOME="$H"; \
    export PATH="$H/.local/bin:$PATH"; \
    pi install "${PI_SUBAGENTS}"

# The ask-user extension, in Pi's auto-discovery path.
RUN set -eu; \
    H="$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f6 || true)"; \
    [ -n "$H" ] || H="$HOME"; \
    [ -n "$H" ] || H=/root; \
    mkdir -p "$H/.pi/agent/extensions"; \
    cp "/tmp/deuce-build/${ASK_USER_FILE}" "$H/.pi/agent/extensions/${ASK_USER_FILE}"

USER root
RUN rm -rf /tmp/deuce-build
USER ${FINAL_USER}
`
