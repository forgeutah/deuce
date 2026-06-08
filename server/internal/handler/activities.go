package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"encoding/json"

	db "github.com/forgeutah/deuce/server/internal/db"
)

type activityResponse struct {
	ID          uuid.UUID       `json:"id"`
	SessionID   uuid.UUID       `json:"sessionId"`
	Type        string          `json:"type"`
	Description string          `json:"description"`
	AgentID     *uuid.UUID      `json:"agentId"`
	Metadata    json.RawMessage `json:"metadata"`
	Timestamp   time.Time       `json:"timestamp"`
}

func toActivityResponse(a db.ActivityItem) activityResponse {
	var agentID *uuid.UUID
	if a.AgentID.Valid {
		id := uuid.UUID(a.AgentID.Bytes)
		agentID = &id
	}
	metadata := json.RawMessage("null")
	if a.Metadata != nil {
		metadata = a.Metadata
	}
	return activityResponse{
		ID:          a.ID,
		SessionID:   a.SessionID,
		Type:        a.Type,
		Description: a.Description,
		AgentID:     agentID,
		Metadata:    metadata,
		Timestamp:   a.CreatedAt,
	}
}

func (h *Handler) ListActivities(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Read gate: team membership grants read access to a session's activity.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	activities, err := h.queries.ListActivities(r.Context(), db.ListActivitiesParams{
		SessionID: sessionID,
		Limit:     int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list activities")
		return
	}

	result := make([]activityResponse, 0, len(activities))
	for _, a := range activities {
		result = append(result, toActivityResponse(a))
	}

	writeJSON(w, http.StatusOK, result)
}

// Unused for now, but the function signature matches what the agent simulator needs
func (h *Handler) createActivity(sessionID uuid.UUID, actType, description string, agentID *uuid.UUID) {
	var aid pgtype.UUID
	if agentID != nil {
		aid = pgtype.UUID{Bytes: *agentID, Valid: true}
	}
	_, _ = h.queries.CreateActivity(nil, db.CreateActivityParams{
		SessionID:   sessionID,
		Type:        actType,
		Description: description,
		AgentID:     aid,
	})
}
