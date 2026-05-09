package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
)

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.queries.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list agents")
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

type updateAgentsRequest struct {
	AgentIDs []string `json:"agentIds"`
}

func (h *Handler) UpdateSessionAgents(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	var req updateAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	// Remove all existing agents
	if err := h.queries.RemoveAllSessionAgents(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update agents")
		return
	}

	// Add new agents
	for _, aid := range req.AgentIDs {
		agentID, err := uuid.Parse(aid)
		if err != nil {
			continue
		}
		_ = h.queries.AddSessionAgent(r.Context(), db.AddSessionAgentParams{
			SessionID: sessionID,
			AgentID:   agentID,
		})
	}

	// Return updated agent list
	agents, err := h.queries.ListSessionAgents(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list agents")
		return
	}
	writeJSON(w, http.StatusOK, agents)
}
