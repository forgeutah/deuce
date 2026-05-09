package handler

import (
	"net/http"

	"github.com/google/uuid"
)

type teamResponse struct {
	ID      uuid.UUID      `json:"id"`
	Name    string         `json:"name"`
	Slug    string         `json:"slug"`
	Members []memberResult `json:"members"`
}

type memberResult struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	Avatar string    `json:"avatar"`
	Status string    `json:"status"`
}

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	teams, err := h.queries.ListTeamsForUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list teams")
		return
	}

	result := make([]teamResponse, 0, len(teams))
	for _, t := range teams {
		members, err := h.queries.ListTeamMembers(r.Context(), t.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list team members")
			return
		}

		ms := make([]memberResult, 0, len(members))
		for _, m := range members {
			ms = append(ms, memberResult{
				ID:     m.ID,
				Name:   m.Name,
				Email:  m.Email,
				Avatar: m.Avatar,
				Status: m.Status,
			})
		}

		result = append(result, teamResponse{
			ID:      t.ID,
			Name:    t.Name,
			Slug:    t.Slug,
			Members: ms,
		})
	}

	writeJSON(w, http.StatusOK, result)
}
