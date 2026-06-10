package handler

import (
	"encoding/json"
	"net/http"
)

// agentSettingsResponse is the GET/PUT /api/agent shape: deuce's configurable
// system prompt. Identity (id/name/color) renders from constants
// (agent.DeuceAgentID / DeuceAgentName, the frontend DEUCE constant) and is
// not part of the contract.
type agentSettingsResponse struct {
	SystemPrompt string `json:"systemPrompt"`
}

// GetAgentSettings handles GET /api/agent. Authenticated-user gated (any team
// member may read the prompt; it contains instructions, not secrets).
func (h *Handler) GetAgentSettings(w http.ResponseWriter, r *http.Request) {
	ag, err := h.queries.GetDeuceAgent(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to load agent settings")
		return
	}
	writeJSON(w, http.StatusOK, agentSettingsResponse{SystemPrompt: ag.SystemPrompt})
}

type updateAgentSettingsRequest struct {
	SystemPrompt string `json:"systemPrompt"`
}

// UpdateAgentSettings handles PUT /api/agent. The prompt is GLOBAL — it shapes
// deuce in every session. Pi applies it only at process launch, so idle
// sessions' processes are recycled on save; sessions mid-task pick the change
// up on their next process launch. Authenticated-user gated: no finer role
// model exists yet (an audit trail / admin gate is deferred — see the plan's
// Scope Boundaries).
func (h *Handler) UpdateAgentSettings(w http.ResponseWriter, r *http.Request) {
	var req updateAgentSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	ag, err := h.queries.UpdateDeuceSystemPrompt(r.Context(), req.SystemPrompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to update agent settings")
		return
	}

	if h.runtime != nil {
		h.runtime.RecycleIdleProcesses()
	}

	writeJSON(w, http.StatusOK, agentSettingsResponse{SystemPrompt: ag.SystemPrompt})
}
