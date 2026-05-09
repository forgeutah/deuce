package handler

import (
	"net/http"

	"github.com/google/uuid"
)

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	teamID := r.URL.Query().Get("teamId")

	if teamID != "" {
		tid, err := uuid.Parse(teamID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_TEAM_ID", "invalid team ID")
			return
		}
		projects, err := h.queries.ListProjectsByTeam(r.Context(), tid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list projects")
			return
		}
		writeJSON(w, http.StatusOK, projects)
		return
	}

	projects, err := h.queries.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list projects")
		return
	}
	writeJSON(w, http.StatusOK, projects)
}
