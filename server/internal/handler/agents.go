package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// Agent color palette for auto-assignment
var agentColors = []struct {
	Color      string
	ColorMuted string
}{
	{"#58a6ff", "#0c2d6b"},
	{"#BE8FFF", "#3c1e70"},
	{"#3fb950", "#033a16"},
	{"#d29922", "#4b2900"},
	{"#f778ba", "#5e103e"},
	{"#79c0ff", "#0a3069"},
	{"#ffa657", "#5a1e02"},
	{"#ff7b72", "#67060c"},
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.queries.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list agents")
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

type createAgentRequest struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Description  string `json:"description"`
	SystemPrompt string `json:"systemPrompt"`
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "agent name is required")
		return
	}

	// Auto-assign color based on existing agent count
	agents, _ := h.queries.ListAgents(r.Context())
	colorIdx := len(agents) % len(agentColors)
	color := agentColors[colorIdx]

	agent, err := h.queries.CreateAgent(r.Context(), db.CreateAgentParams{
		Name:         req.Name,
		Role:         req.Role,
		Color:        color.Color,
		ColorMuted:   color.ColorMuted,
		Provider:     req.Provider,
		Model:        req.Model,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to create agent")
		return
	}

	writeJSON(w, http.StatusCreated, agent)
}

type updateAgentRequest struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Description  string `json:"description"`
	SystemPrompt string `json:"systemPrompt"`
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_ID", "invalid agent ID")
		return
	}

	var req updateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	agent, err := h.queries.UpdateAgent(r.Context(), db.UpdateAgentParams{
		ID:           agentID,
		Name:         req.Name,
		Role:         req.Role,
		Provider:     req.Provider,
		Model:        req.Model,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_ID", "invalid agent ID")
		return
	}

	if err := h.queries.SoftDeleteAgent(r.Context(), agentID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "agent not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	// Write gate: changing a session's agent roster requires SESSION membership.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionMember(w, r, sessionID, userID) {
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
