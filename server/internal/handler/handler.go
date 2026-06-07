package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/agent"
	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/terminal"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type Handler struct {
	queries     *db.Queries
	pool        *pgxpool.Pool
	hub         *ws.Hub
	githubToken string
	workspaces  *workspace.Manager
	terminals   *terminal.Manager
	executor    *agent.Executor
	agentQueue  *agent.Queue
	// runtime is the Pi-harness engine (KTD11). Non-nil when
	// DEUCE_AGENT_HARNESS=pi (default); nil in legacy "claude" mode, where the
	// executor/agentQueue path runs instead. Installed via SetRuntime.
	runtime        *agent.Runtime
	wsOrigins      []string
	publicHostname string
	sshListenAddr  string

	// sshAvailable reports whether the SSH proxy is accepting connections.
	// Set by main.go after sshproxy.New + ListenAndServe succeed; flipped
	// to false if the listener crashes or fails to start. The vscode-uri
	// endpoint returns 503 SSH_UNAVAILABLE when this reports false.
	// Nil means "always available" (legacy callers / tests that don't
	// wire the predicate).
	sshAvailable func() bool

	// workspaceActions tracks in-flight workspace lifecycle goroutines
	// (Start / Stop / Rebuild / Delete). main.go waits on this during
	// shutdown so a `devpod delete` doesn't continue running after the
	// process has reported orderly shutdown. The 10s drain window is
	// the same one HTTP and SSH share.
	workspaceActions sync.WaitGroup
}

// WaitWorkspaceActions blocks until every in-flight workspace lifecycle
// goroutine completes or ctx expires. Called from main.go's shutdown drain
// alongside httpServer.Shutdown and sshSrv.Shutdown.
func (h *Handler) WaitWorkspaceActions(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		h.workspaceActions.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// New constructs a Handler. publicHostname is the externally-reachable
// hostname for the vscode:// URI (empty falls back to the request Host
// header in dev; required-in-proxy is enforced by config.Validate).
// sshListenAddr is used to derive the URI port. sshAvailable lets main.go
// flip the SSH state; nil means always-available (test default).
func New(queries *db.Queries, pool *pgxpool.Pool, hub *ws.Hub, githubToken string, wm *workspace.Manager, tm *terminal.Manager, exec *agent.Executor, aq *agent.Queue, wsOrigins []string, publicHostname, sshListenAddr string) *Handler {
	return &Handler{
		queries:        queries,
		pool:           pool,
		hub:            hub,
		githubToken:    githubToken,
		workspaces:     wm,
		terminals:      tm,
		executor:       exec,
		agentQueue:     aq,
		wsOrigins:      wsOrigins,
		publicHostname: publicHostname,
		sshListenAddr:  sshListenAddr,
	}
}

// SetRuntime installs the Pi-harness runtime (KTD11). When set, SendMessage
// routes agent mentions through it instead of the legacy executor queue, and the
// runtime posts agent replies into the chat via postAgentReply.
func (h *Handler) SetRuntime(rt *agent.Runtime) {
	h.runtime = rt
	rt.SetReplyPoster(h.postAgentReply)
}

// SetSSHAvailable installs a predicate that the vscode-uri endpoint checks
// before building a URI. main.go installs a closure that reflects the live
// sshproxy state; tests may leave it unset (treated as always-available).
func (h *Handler) SetSSHAvailable(predicate func() bool) {
	h.sshAvailable = predicate
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func getUserID(r *http.Request) string {
	return auth.GetUserID(r.Context())
}
