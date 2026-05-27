package handler

import (
	"context"
	"encoding/json"
	"log/slog"
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
	Agents          []agentResult  `json:"agents"`
	Members         []memberResult `json:"members"`
}

type agentResult struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Color        string    `json:"color"`
	ColorMuted   string    `json:"colorMuted"`
	Status       string    `json:"status"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Description  string    `json:"description"`
	SystemPrompt string    `json:"systemPrompt"`
}

func (h *Handler) buildSessionResponse(r *http.Request, s db.Session, userID uuid.UUID) (sessionResponse, error) {
	agents, err := h.queries.ListSessionAgents(r.Context(), s.ID)
	if err != nil {
		return sessionResponse{}, err
	}

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

	agentResults := make([]agentResult, 0, len(agents))
	for _, a := range agents {
		agentResults = append(agentResults, agentResult{
			ID:           a.ID,
			Name:         a.Name,
			Role:         a.Role,
			Color:        a.Color,
			ColorMuted:   a.ColorMuted,
			Status:       a.AgentStatus,
			Provider:     a.Provider,
			Model:        a.Model,
			Description:  a.Description,
			SystemPrompt: a.SystemPrompt,
		})
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
		Agents:          agentResults,
		Members:         memberResults,
	}, nil
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	sessions, err := h.queries.ListSessionsForUser(r.Context(), userID)
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
	AgentIDs    []string `json:"agentIds"`
	MemberIDs   []string `json:"memberIds"`
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

	if len(req.Description) > maxSessionDescriptionLength {
		writeError(w, http.StatusBadRequest, "DESCRIPTION_TOO_LONG", "description exceeds maximum length")
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

	// Add agents
	for _, aid := range req.AgentIDs {
		agentID, err := uuid.Parse(aid)
		if err != nil {
			continue
		}
		_ = h.queries.AddSessionAgent(r.Context(), db.AddSessionAgentParams{
			SessionID: session.ID,
			AgentID:   agentID,
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
// Authorization mirrors GetSession: any authenticated user can fetch the
// URI. The real access gate is the SSH proxy's public-key auth — only a
// session-member's registered key will authenticate at SSH time.
func (h *Handler) GetSessionVSCodeURI(w http.ResponseWriter, r *http.Request) {
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
		host = r.Host
	}
	port := sshPortFromAddr(h.sshListenAddr)

	// session.Name is the workspace ID per the existing repo invariant
	// (U2 was deferred). Sessions cannot be renamed today, so this is
	// stable for the lifetime of the URI.
	uri := "vscode://vscode-remote/ssh-remote+dc-" + session.ID.String() +
		"@" + host + ":" + strconv.Itoa(port) +
		"/workspaces/" + session.Name

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
		// Install Claude Code after workspace creation succeeds
		if installErr := h.workspaces.InstallTools(ctx, workspaceID, logFn); installErr != nil {
			slog.Warn("claude code installation failed", "sessionID", sessionID, "error", installErr)
			// Non-fatal — workspace is still usable
		}
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
