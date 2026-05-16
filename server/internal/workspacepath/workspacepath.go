// Package workspacepath resolves the host-filesystem path to a DevPod
// docker-provider workspace's content directory.
//
// For the docker provider (the local-dev default), workspace files live on
// the host at $DEVPOD_AGENT_CONTENT_DIR/<workspace-name>/content. This
// package centralizes that resolution so handlers and helpers don't redrive
// it independently and so the DEVPOD_AGENT_CONTENT_DIR override lands in
// one place.
//
// See docs/solutions/architecture-patterns/devpod-docker-workspace-bind-mount-2026-05-13.md
// for the rationale (host-FS access vs `devpod ssh`).
package workspacepath

import (
	"os"
	"path/filepath"
)

// Resolve returns the host path to the named workspace's content directory.
// If DEVPOD_AGENT_CONTENT_DIR is set, it overrides the default location.
// The path is constructed lexically and is not validated for existence —
// callers that need that should os.Stat the result.
func Resolve(workspaceName string) string {
	base := os.Getenv("DEVPOD_AGENT_CONTENT_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".devpod", "agent", "contexts", "default", "workspaces")
	}
	return filepath.Join(base, workspaceName, "content")
}
