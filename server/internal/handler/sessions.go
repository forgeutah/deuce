package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// defaultSSHPort is the fallback port used when sshListenAddr is unset or
// unparseable. Mirrors the sshproxy package default; U11 will wire the real
// value through config.
const defaultSSHPort = 2222

const maxSessionDescriptionLength = 200

type sessionResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	ProjectID       uuid.UUID      `json:"projectId"`
	Status          string         `json:"status"`
	WorkspaceStatus string         `json:"workspaceStatus"`
	PlanContent     string         `json:"planContent"`
	CreatedAt       time.Time      `json:"createdAt"`
	LastActivityAt  time.Time      `json:"lastActivityAt"`
	UnreadCount     int            `json:"unreadCount"`
	Members         []memberResult `json:"members"`
}

func (h *Handler) buildSessionResponse(r *http.Request, s db.Session, userID uuid.UUID) (sessionResponse, error) {
	members, err := h.queries.ListSessionMembers(r.Context(), s.ID)
	if err != nil {
		return sessionResponse{}, err
	}

	unread, err := h.queries.GetUnreadCount(r.Context(), db.GetUnreadCountParams{
		SessionID: s.ID,
		UserID:    userID,
	})
	if err != nil {
		unread = 0
	}

	memberResults := make([]memberResult, 0, len(members))
	for _, m := range members {
		memberResults = append(memberResults, memberResult{
			ID:     m.ID,
			Name:   m.Name,
			Email:  m.Email,
			Avatar: m.Avatar,
			Status: m.Status,
		})
	}

	return sessionResponse{
		ID:              s.ID,
		Name:            s.Name,
		Description:     s.Description,
		ProjectID:       s.ProjectID,
		Status:          s.Status,
		WorkspaceStatus: s.WorkspaceStatus,
		PlanContent:     s.PlanContent,
		CreatedAt:       s.CreatedAt,
		LastActivityAt:  s.LastActivityAt,
		UnreadCount:     int(unread),
		Members:         memberResults,
	}, nil
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	// ?archived=true returns only archived sessions (the Archived view); the
	// default returns only non-archived sessions (the normal sidebar).
	archived := r.URL.Query().Get("archived")
	listArchived := archived == "true" || archived == "1"

	var sessions []db.Session
	if listArchived {
		sessions, err = h.queries.ListArchivedSessionsForUser(r.Context(), userID)
	} else {
		sessions, err = h.queries.ListSessionsForUser(r.Context(), userID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list sessions")
		return
	}

	result := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		sr, err := h.buildSessionResponse(r, s, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
			return
		}
		result = append(result, sr)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
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

	// Read gate: a session is visible to anyone on its team, not just its
	// members. Non-team users cannot read a session even by direct ID.
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}

	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return
	}

	writeJSON(w, http.StatusOK, sr)
}

type createSessionRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ProjectID   string   `json:"projectId"`
	RepoURL     string   `json:"repoUrl"`
	MemberIDs   []string `json:"memberIds"`
	// AgentIDs is tolerated-and-ignored: pre-013 clients sent a roster pick,
	// but the single built-in deuce agent is implicitly part of every session.
	AgentIDs []string `json:"agentIds"`
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "session name is required")
		return
	}

	// session.Name flows directly into `devpod up --id <name>`, `devpod stop`,
	// and `devpod delete` argv slots. Validate at the boundary so unsafe IDs
	// can't reach the CLI later via the workspace lifecycle endpoints.
	if err := validateWorkspaceID(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_NAME", err.Error())
		return
	}

	if len(req.Description) > maxSessionDescriptionLength {
		writeError(w, http.StatusBadRequest, "DESCRIPTION_TOO_LONG", "description exceeds maximum length")
		return
	}

	// repoURL flows into `devpod up <repoURL>` and is re-executed on every
	// Start and Rebuild. Reject file://, embedded credentials, and other
	// schemes here so the persisted value is always safe to re-execute.
	if err := validateRepoURL(req.RepoURL); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REPO_URL", err.Error())
		return
	}

	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "invalid project ID")
		return
	}

	session, err := h.queries.CreateSession(r.Context(), db.CreateSessionParams{
		Name:        req.Name,
		Description: req.Description,
		ProjectID:   projectID,
		RepoUrl:     req.RepoURL,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to create session")
		return
	}

	// Add current user as member
	_ = h.queries.AddSessionMember(r.Context(), db.AddSessionMemberParams{
		SessionID: session.ID,
		UserID:    userID,
	})

	// Add additional members
	for _, mid := range req.MemberIDs {
		memberID, err := uuid.Parse(mid)
		if err != nil {
			continue
		}
		_ = h.queries.AddSessionMember(r.Context(), db.AddSessionMemberParams{
			SessionID: session.ID,
			UserID:    memberID,
		})
	}

	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return
	}

	// Broadcast session creation to all members
	msg, _ := ws.NewServerMessage(ws.TypeSessionUpdate, session.ID.String(), sr)
	for _, mid := range req.MemberIDs {
		h.hub.BroadcastToUser(mid, msg)
	}

	// Kick off DevPod workspace in background
	if req.RepoURL != "" && h.workspaces != nil && h.workspaces.Available() {
		go h.startWorkspace(session.ID, req.Name, req.RepoURL)
	}

	writeJSON(w, http.StatusCreated, sr)
}

type updateSessionRequest struct {
	Status          *string `json:"status"`
	PlanContent     *string `json:"planContent"`
	WorkspaceStatus *string `json:"workspaceStatus"`
	Description     *string `json:"description"`
}

func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
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

	// Write gate: mutating session state (status, plan, description, workspace
	// status) requires SESSION membership.
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.Description != nil && len(*req.Description) > maxSessionDescriptionLength {
		writeError(w, http.StatusBadRequest, "DESCRIPTION_TOO_LONG", "description exceeds maximum length")
		return
	}

	var session db.Session
	if req.Status != nil {
		session, err = h.queries.UpdateSessionStatus(r.Context(), db.UpdateSessionStatusParams{
			ID:     sessionID,
			Status: *req.Status,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update session")
			return
		}
	}

	if req.PlanContent != nil {
		session, err = h.queries.UpdateSessionPlan(r.Context(), db.UpdateSessionPlanParams{
			ID:          sessionID,
			PlanContent: *req.PlanContent,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update plan")
			return
		}
	}

	if req.WorkspaceStatus != nil {
		session, err = h.queries.UpdateSessionWorkspaceStatus(r.Context(), db.UpdateSessionWorkspaceStatusParams{
			ID:              sessionID,
			WorkspaceStatus: *req.WorkspaceStatus,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update workspace status")
			return
		}
	}

	if req.Description != nil {
		session, err = h.queries.UpdateSessionDescription(r.Context(), db.UpdateSessionDescriptionParams{
			ID:          sessionID,
			Description: *req.Description,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update description")
			return
		}
	}

	if session.ID == uuid.Nil {
		session, err = h.queries.GetSession(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
			return
		}
	}

	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return
	}

	// Broadcast update
	msg, _ := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), sr)
	h.hub.BroadcastToSession(sessionID.String(), msg, nil)

	writeJSON(w, http.StatusOK, sr)
}

// ArchiveSession POST /api/sessions/{sessionID}/archive retires a session:
// it flips status to "archived" (preserving all history) and tears down the
// session's DevPod container in the background to reclaim resources. The
// session disappears from the normal sidebar (ListSessionsForUser excludes
// archived) and surfaces only through the Archived view.
func (h *Handler) ArchiveSession(w http.ResponseWriter, r *http.Request) {
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

	// Write gate: archiving retires the session and destroys its container, so
	// it requires SESSION membership. Runs before any existence lookup.
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	// Flip status to archived FIRST, before teardown. The reconciler keys off
	// ListNonArchivedSessions, so flipping first removes the session from its
	// view and prevents a race to restart the container mid-teardown.
	session, err := h.queries.UpdateSessionStatus(r.Context(), db.UpdateSessionStatusParams{
		ID:     sessionID,
		Status: "archived",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to archive session")
		return
	}

	// Tear down the container in the background when devpod is available and a
	// container may still exist. Reuses the shared delete action path, which
	// writes the terminal workspace_status ("missing"/"failed") and broadcasts
	// it. Archive still succeeds (status-only) when devpod is unavailable.
	if h.workspaces != nil && h.workspaces.Available() &&
		session.WorkspaceStatus != "missing" && !isTransitionalStatus(session.WorkspaceStatus) {
		if _, derr := h.queries.UpdateSessionWorkspaceStatus(r.Context(), db.UpdateSessionWorkspaceStatusParams{
			ID:              sessionID,
			WorkspaceStatus: "deleting",
		}); derr == nil {
			session.WorkspaceStatus = "deleting"
		}
		h.workspaceActions.Add(1)
		go func() {
			defer h.workspaceActions.Done()
			h.runWorkspaceAction(sessionID, session.Name, session.RepoUrl, actionDelete)
		}()
	}

	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return
	}

	// Broadcast the archived state; clients refetch the (now archived-filtered)
	// list and drop the session from the sidebar.
	if msg, mErr := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), sr); mErr == nil {
		h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	}

	writeJSON(w, http.StatusOK, sr)
}

// UnarchiveSession POST /api/sessions/{sessionID}/unarchive restores an
// archived session by flipping status back to "active". The container was torn
// down at archive time, so workspace_status remains "missing"; the session
// reappears in the sidebar with the normal start-workspace affordance.
func (h *Handler) UnarchiveSession(w http.ResponseWriter, r *http.Request) {
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

	// Write gate: same membership requirement as archive.
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	session, err := h.queries.UpdateSessionStatus(r.Context(), db.UpdateSessionStatusParams{
		ID:     sessionID,
		Status: "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to restore session")
		return
	}

	sr, err := h.buildSessionResponse(r, session, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return
	}

	if msg, mErr := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), sr); mErr == nil {
		h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	}

	writeJSON(w, http.StatusOK, sr)
}

// vscodeURIResponse is the JSON body returned by GetSessionVSCodeURI.
type vscodeURIResponse struct {
	URI string `json:"uri"`
}

// GetSessionVSCodeURI returns a vscode://vscode-remote/... URI that opens
// the session's devcontainer in VS Code Remote-SSH. The endpoint is gated
// on the calling user having at least one SSH key on file — without one,
// the SSH proxy (U7) would reject the connection. The frontend uses the
// 412 NO_SSH_KEY response to open the SSH key setup modal.
//
// Authorization mirrors GetSession: a team member may fetch the URI. The
// real access gate is the SSH proxy's public-key auth — only a session
// member's registered key will authenticate at SSH time — but we still
// team-gate the endpoint so it doesn't leak the workspace name / reachable
// SSH destination for sessions outside the caller's teams.
func (h *Handler) GetSessionVSCodeURI(w http.ResponseWriter, r *http.Request) {
	// SSH listener availability check — short-circuit before the DB
	// roundtrip so a downed SSH listener doesn't quietly slow the UI down.
	if h.sshAvailable != nil && !h.sshAvailable() {
		writeError(w, http.StatusServiceUnavailable, "SSH_UNAVAILABLE", "VS Code remote access is not available on this server")
		return
	}

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

	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}

	keys, err := h.queries.ListUserSSHKeys(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list SSH keys")
		return
	}
	if len(keys) == 0 {
		writeError(w, http.StatusPreconditionFailed, "NO_SSH_KEY", "user has no SSH keys on file")
		return
	}

	host := h.publicHostname
	if host == "" {
		// Strip any port from r.Host before splicing in the SSH port.
		// Devcontainer / proxy setups commonly land here with "host:port"
		// and concatenating ":2222" would otherwise produce a malformed
		// "host:8080:2222" authority that VS Code rejects.
		host = r.Host
		if hostOnly, _, splitErr := net.SplitHostPort(r.Host); splitErr == nil {
			host = hostOnly
		}
	}
	port := sshPortFromAddr(h.sshListenAddr)

	// session.Name is the workspace ID per the existing repo invariant
	// (U2 was deferred). Sessions cannot be renamed today, so this is
	// stable for the lifetime of the URI.
	//
	// ?windowId=_blank forces VS Code to spawn a NEW window instead of
	// hijacking the user's last-focused one — otherwise opening Deuce
	// while already working in another VS Code window closes that
	// workspace.
	uri := "vscode://vscode-remote/ssh-remote+dc-" + session.ID.String() +
		"@" + host + ":" + strconv.Itoa(port) +
		"/workspaces/" + session.Name +
		"?windowId=_blank"

	writeJSON(w, http.StatusOK, vscodeURIResponse{URI: uri})
}

// sshPortFromAddr extracts the port from a Go listen-address string such as
// ":2222" or "0.0.0.0:2222". Any parse failure falls back to the default
// 2222 — defensive because U11 hasn't shipped real config yet.
func sshPortFromAddr(addr string) int {
	if addr == "" {
		return defaultSSHPort
	}
	// Strip an optional host portion before the colon.
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		addr = addr[idx+1:]
	}
	port, err := strconv.Atoi(addr)
	if err != nil || port <= 0 || port > 65535 {
		return defaultSSHPort
	}
	return port
}

func (h *Handler) startWorkspace(sessionID uuid.UUID, workspaceID, repoURL string) {
	ctx := context.Background()
	slog.Info("starting workspace", "sessionID", sessionID, "workspaceID", workspaceID, "repoURL", repoURL)

	// Stream DevPod output line-by-line to WebSocket clients
	logFn := func(line string) {
		msg, _ := ws.NewServerMessage(ws.TypeWorkspaceLog, sessionID.String(), map[string]string{
			"line": line,
		})
		h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	}

	err := h.workspaces.Create(ctx, workspaceID, repoURL, logFn)

	var newStatus string
	if err != nil {
		slog.Error("workspace creation failed", "sessionID", sessionID, "error", err)
		newStatus = "failed"
	} else {
		// Install the agent harnesses (Claude fallback + Pi + ask-user
		// extension) after workspace creation. Idempotent and non-fatal; the
		// same helper runs on every start/rebuild so older containers migrate.
		h.provisionAgentTools(ctx, workspaceID, logFn)
		newStatus = "ready"
	}

	_, dbErr := h.queries.UpdateSessionWorkspaceStatus(ctx, db.UpdateSessionWorkspaceStatusParams{
		ID:              sessionID,
		WorkspaceStatus: newStatus,
	})
	if dbErr != nil {
		slog.Error("failed to update workspace status in DB", "error", dbErr)
	}

	wsMsg, _ := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), map[string]string{
		"workspaceStatus": newStatus,
	})
	h.hub.BroadcastToSession(sessionID.String(), wsMsg, nil)
}
