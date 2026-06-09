package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/forgeutah/deuce/server/internal/agent/pirun/extension"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// provisionAgentTools installs the agent harnesses into a workspace container:
// Claude Code (legacy fallback), Pi, and the ask-user extension. All installers
// are idempotent and non-fatal, so this is safe to run on every create/start —
// it migrates containers created before agent support without recreating them.
func (h *Handler) provisionAgentTools(ctx context.Context, workspaceID string, logFn workspace.LogFunc) {
	if err := h.workspaces.InstallTools(ctx, workspaceID, logFn); err != nil {
		slog.Warn("claude code installation failed", "workspace", workspaceID, "error", err)
	}
	if err := h.workspaces.InstallPi(ctx, workspaceID, logFn); err != nil {
		slog.Warn("pi installation failed", "workspace", workspaceID, "error", err)
	}
	if err := h.workspaces.InstallPiExtension(ctx, workspaceID, extension.AskUserFilename, extension.AskUser, logFn); err != nil {
		// Loud, not fatal: the workspace still comes up, but the user has been
		// told via logFn that agents can't ask questions here (R10). Error level
		// so it stands out from routine provisioning warnings.
		slog.Error("pi extension installation failed", "workspace", workspaceID, "error", err)
	}
}

// workspaceAction names the four lifecycle operations the user can trigger.
// Each one maps to a transitional workspace_status that the handler writes
// synchronously, then a devpod CLI call in a tracked background goroutine,
// then a terminal workspace_status written when the call returns.
type workspaceAction string

const (
	actionStart   workspaceAction = "start"
	actionStop    workspaceAction = "stop"
	actionRebuild workspaceAction = "rebuild"
	actionDelete  workspaceAction = "delete"
)

// transitionalStatus returns the workspace_status to write while the action
// is in flight. The reconciler skips rows in these states by design (R5).
func (a workspaceAction) transitionalStatus() string {
	switch a {
	case actionStart:
		return "starting"
	case actionStop:
		return "stopping"
	case actionRebuild:
		return "rebuilding"
	case actionDelete:
		return "deleting"
	}
	return ""
}

// isTransitionalStatus reports whether status is one of the four in-flight
// values the handler owns. Used to short-circuit concurrent actions with a
// 409 WORKSPACE_BUSY response.
func isTransitionalStatus(status string) bool {
	switch status {
	case "starting", "stopping", "rebuilding", "deleting":
		return true
	}
	return false
}

// StartWorkspace POST /api/sessions/{sessionID}/workspace/start
func (h *Handler) StartWorkspace(w http.ResponseWriter, r *http.Request) {
	h.handleWorkspaceAction(w, r, actionStart)
}

// StopWorkspace POST /api/sessions/{sessionID}/workspace/stop
func (h *Handler) StopWorkspace(w http.ResponseWriter, r *http.Request) {
	h.handleWorkspaceAction(w, r, actionStop)
}

// RebuildWorkspace POST /api/sessions/{sessionID}/workspace/rebuild
func (h *Handler) RebuildWorkspace(w http.ResponseWriter, r *http.Request) {
	h.handleWorkspaceAction(w, r, actionRebuild)
}

// DeleteWorkspace POST /api/sessions/{sessionID}/workspace/delete
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	h.handleWorkspaceAction(w, r, actionDelete)
}

// handleWorkspaceAction is the shared front-half for all four endpoints.
// It validates the request, atomically flips workspace_status to the
// transitional value (CAS so concurrent calls don't both pass the check),
// broadcasts, responds 200, then spawns a tracked goroutine to run the
// devpod call.
func (h *Handler) handleWorkspaceAction(w http.ResponseWriter, r *http.Request, action workspaceAction) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	if h.workspaces == nil || !h.workspaces.Available() {
		writeError(w, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE", "devpod is not available on this server")
		return
	}

	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}

	// Authorize: workspace lifecycle is destructive (delete/rebuild wipe the
	// container), so it requires SESSION membership.
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	// Reject when the row is already transitional — another action is in
	// flight. Returns 409 with the in-flight action so the UI can render a
	// useful message.
	if isTransitionalStatus(session.WorkspaceStatus) {
		writeError(w, http.StatusConflict, "WORKSPACE_BUSY",
			fmt.Sprintf("workspace is currently %s", session.WorkspaceStatus))
		return
	}

	// Start and Rebuild both call `devpod up <repoURL> --id <name>`. Without
	// a repoURL the argv is malformed; bail early with a clear error rather
	// than fire devpod and let it fail mid-flight.
	if (action == actionStart || action == actionRebuild) && session.RepoUrl == "" {
		writeError(w, http.StatusBadRequest, "MISSING_REPO_URL",
			"session has no repo URL; cannot start or rebuild")
		return
	}

	transitional := action.transitionalStatus()

	// Atomic CAS: only flip the status if it still matches what we read.
	// Two concurrent action requests against the same session both observe
	// the same starting status; the second one's CAS comes back rows=0 and
	// gets a 409. Closes the TOCTOU window between the busy check above and
	// the write below.
	rows, err := h.queries.UpdateSessionWorkspaceStatusIfMatches(r.Context(), db.UpdateSessionWorkspaceStatusIfMatchesParams{
		ID:             sessionID,
		ExpectedStatus: session.WorkspaceStatus,
		NewStatus:      transitional,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update workspace status")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusConflict, "WORKSPACE_BUSY", "workspace state changed concurrently; try again")
		return
	}

	// Build the response body. The session was just CAS-updated; the row in
	// the DB now carries the transitional status. Mirror that on the local
	// copy rather than re-querying.
	session.WorkspaceStatus = transitional
	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session response")
		return
	}

	// Broadcast the transitional state so subscribed clients fade their
	// tabs immediately. The terminal-state broadcast fires from the
	// goroutine when the action completes.
	if msg, mErr := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), map[string]string{
		"workspaceStatus": transitional,
	}); mErr == nil {
		h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	}

	// Track the goroutine so main.go's shutdown drain waits on it. Without
	// this, a `devpod delete` triggered just before SIGTERM would continue
	// running past httpServer.Shutdown's return and potentially execute
	// against a docker daemon the operator has stopped.
	h.workspaceActions.Add(1)
	go func() {
		defer h.workspaceActions.Done()
		h.runWorkspaceAction(sessionID, session.Name, session.RepoUrl, action)
	}()

	writeJSON(w, http.StatusOK, sr)
}

// runWorkspaceAction executes the devpod CLI call for one action and writes
// the terminal workspace_status when it returns. Always runs in a goroutine
// registered with h.workspaceActions.
func (h *Handler) runWorkspaceAction(sessionID uuid.UUID, workspaceID, repoURL string, action workspaceAction) {
	ctx := context.Background()

	logFn := func(line string) {
		msg, err := ws.NewServerMessage(ws.TypeWorkspaceLog, sessionID.String(), map[string]string{
			"line": line,
		})
		if err == nil {
			h.hub.BroadcastToSession(sessionID.String(), msg, nil)
		}
	}

	var newStatus string
	var actErr error

	switch action {
	case actionStart:
		// devpod up is idempotent against existing workspaces: it resumes
		// a stopped container without rebuilding (verified — `--recreate`
		// is the explicit opt-in for a fresh container).
		actErr = h.workspaces.Create(ctx, workspaceID, repoURL, logFn)
		if actErr == nil {
			// (Re)provision agent tooling on every start so containers created
			// before agent support — or where a prior install failed — pick up
			// Pi + the ask-user extension. The installers are idempotent.
			h.provisionAgentTools(ctx, workspaceID, logFn)
			newStatus = "ready"
		} else {
			newStatus = "failed"
		}

	case actionStop:
		actErr = h.workspaces.Stop(ctx, workspaceID)
		if actErr == nil {
			newStatus = "stopped"
		} else {
			newStatus = "failed"
		}

	case actionRebuild:
		// Delete + Create. Note: Manager.Delete uses CombinedOutput so its
		// output is not streamed via logFn — the user will see logs once
		// Create begins. Documented in the plan's Rebuild risk note.
		if delErr := h.workspaces.Delete(ctx, workspaceID); delErr != nil {
			actErr = fmt.Errorf("rebuild delete: %w", delErr)
		} else {
			actErr = h.workspaces.Create(ctx, workspaceID, repoURL, logFn)
		}
		if actErr == nil {
			h.provisionAgentTools(ctx, workspaceID, logFn)
			newStatus = "ready"
		} else {
			newStatus = "failed"
		}

	case actionDelete:
		actErr = h.workspaces.Delete(ctx, workspaceID)
		if actErr == nil {
			newStatus = "missing"
		} else {
			newStatus = "failed"
		}
	}

	if actErr != nil {
		slog.Error("workspace action failed",
			"action", action,
			"session", sessionID,
			"error", actErr,
		)
		logFn(fmt.Sprintf("ERROR: %s failed: %v", action, actErr))
	}

	if _, err := h.queries.UpdateSessionWorkspaceStatus(ctx, db.UpdateSessionWorkspaceStatusParams{
		ID:              sessionID,
		WorkspaceStatus: newStatus,
	}); err != nil {
		slog.Error("failed to write terminal workspace status",
			"session", sessionID,
			"status", newStatus,
			"error", err,
		)
	}

	if msg, mErr := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), map[string]string{
		"workspaceStatus": newStatus,
	}); mErr == nil {
		h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	}
}
