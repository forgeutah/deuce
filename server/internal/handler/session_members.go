package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// ListUsers returns every provisioned user, for the "add people" picker. Users
// are auto-provisioned on first login (dev middleware or proxy mode), so this
// is the set of teammates who have signed in at least once. Returned in the
// shared memberResult shape so it lines up with Session.members on the client.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.queries.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list users")
		return
	}

	result := make([]memberResult, 0, len(users))
	for _, u := range users {
		result = append(result, memberResult{
			ID:     u.ID,
			Name:   u.Name,
			Email:  u.Email,
			Avatar: u.Avatar,
			Status: u.Status,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

type addSessionMemberRequest struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

// AddSessionMember adds an existing user to an existing session. Membership is
// the single visibility gate (REST list + WS subscribe), so this is how a
// teammate gets access to a session after it was created.
//
// Authorization: any current member may add others. Sessions are shared
// workspaces with no owner role, mirroring the collaborative channel model —
// the same flat trust the create flow already grants every co-member.
func (h *Handler) AddSessionMember(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	callerID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	if !h.requireSessionMember(w, r, sessionID, callerID) {
		return
	}

	var req addSessionMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	// Resolve the target user by ID (people-picker) or email (type-to-invite).
	var target db.User
	switch {
	case req.UserID != "":
		targetID, err := uuid.Parse(req.UserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid target user ID")
			return
		}
		target, err = h.queries.GetUser(r.Context(), targetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
	case req.Email != "":
		target, err = h.queries.LookupUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
		if err != nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "no user with that email has signed in yet")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "MISSING_USER", "userId or email is required")
		return
	}

	if err := h.queries.AddSessionMember(r.Context(), db.AddSessionMemberParams{
		SessionID: sessionID,
		UserID:    target.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to add member")
		return
	}

	sr := h.broadcastMembershipChange(w, r, sessionID, callerID, target.ID)
	if sr == nil {
		return
	}
	writeJSON(w, http.StatusOK, sr)
}

// JoinSession adds the caller to a session as a member (the "Join Session"
// self-serve path). Unlike AddSessionMember — which requires the caller to
// already be a member in order to add OTHERS — JoinSession adds the caller
// themselves, gated on TEAM membership so a user can only join a session they
// can actually see. Idempotent (ON CONFLICT DO NOTHING). Leaving reuses
// RemoveSessionMember with the caller's own ID.
func (h *Handler) JoinSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	callerID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	// Team membership is the read boundary; you may only join a session whose
	// team you belong to.
	if !h.requireSessionTeamMember(w, r, sessionID, callerID) {
		return
	}

	if err := h.queries.AddSessionMember(r.Context(), db.AddSessionMemberParams{
		SessionID: sessionID,
		UserID:    callerID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to join session")
		return
	}

	sr := h.broadcastMembershipChange(w, r, sessionID, callerID, callerID)
	if sr == nil {
		return
	}
	writeJSON(w, http.StatusOK, sr)
}

// RemoveSessionMember removes a user from a session. A member may remove
// anyone, including themselves (the "leave session" path). Idempotent: removing
// a non-member is a no-op that still returns the current state.
func (h *Handler) RemoveSessionMember(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid target user ID")
		return
	}

	callerID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	if !h.requireSessionMember(w, r, sessionID, callerID) {
		return
	}

	if err := h.queries.RemoveSessionMember(r.Context(), db.RemoveSessionMemberParams{
		SessionID: sessionID,
		UserID:    targetID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to remove member")
		return
	}

	sr := h.broadcastMembershipChange(w, r, sessionID, callerID, targetID)
	if sr == nil {
		return
	}
	writeJSON(w, http.StatusOK, sr)
}

// requireSessionMember writes a 403/500 and returns false unless userID is a
// current member of sessionID. Mirrors the gate in AgentRunsSnapshot (KTD14).
func (h *Handler) requireSessionMember(w http.ResponseWriter, r *http.Request, sessionID, userID uuid.UUID) bool {
	member, err := h.queries.IsSessionMember(r.Context(), db.IsSessionMemberParams{
		SessionID: sessionID,
		UserID:    userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to check membership")
		return false
	}
	if !member {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "not a session member")
		return false
	}
	return true
}

// requireSessionTeamMember writes a 403/500 and returns false unless userID
// belongs to the team that owns sessionID's project. This is the READ gate:
// team membership grants visibility/read access to a session, whereas
// requireSessionMember (session membership) gates writing and the live stream.
func (h *Handler) requireSessionTeamMember(w http.ResponseWriter, r *http.Request, sessionID, userID uuid.UUID) bool {
	member, err := h.queries.IsSessionTeamMember(r.Context(), db.IsSessionTeamMemberParams{
		SessionID: sessionID,
		UserID:    userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to check team membership")
		return false
	}
	if !member {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "not a team member")
		return false
	}
	return true
}

// broadcastMembershipChange rebuilds the session response and pushes a
// session_update to the existing room (so members' lists refresh) and directly
// to the affected user (added members aren't subscribed to the room yet;
// removed members need their list to drop the session). Returns the response,
// or nil after writing an error — callers should bail when nil.
func (h *Handler) broadcastMembershipChange(w http.ResponseWriter, r *http.Request, sessionID, callerID, affectedID uuid.UUID) *sessionResponse {
	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return nil
	}

	sr, err := h.buildSessionResponse(r, session, callerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to build session")
		return nil
	}

	msg, _ := ws.NewServerMessage(ws.TypeSessionUpdate, sessionID.String(), sr)
	h.hub.BroadcastToSession(sessionID.String(), msg, nil)
	h.hub.BroadcastToUser(affectedID.String(), msg)

	return &sr
}
