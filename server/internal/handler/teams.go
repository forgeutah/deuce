package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
)

type teamResponse struct {
	ID      uuid.UUID      `json:"id"`
	Name    string         `json:"name"`
	Slug    string         `json:"slug"`
	Members []memberResult `json:"members"`
}

// teamBrowseResponse is the shape the team-management UI lists (every team plus
// the caller's membership state and a member count), distinct from the
// members-embedding teamResponse used for the caller's own teams.
type teamBrowseResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	MemberCount int       `json:"memberCount"`
	IsMember    bool      `json:"isMember"`
	IsDefault   bool      `json:"isDefault"`
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

// ListAllTeams backs the team-management browse list: every team with a member
// count and whether the caller belongs to it, so the UI can render Join/Leave.
func (h *Handler) ListAllTeams(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	rows, err := h.queries.ListAllTeamsWithMembership(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list teams")
		return
	}

	result := make([]teamBrowseResponse, 0, len(rows))
	for _, t := range rows {
		result = append(result, teamBrowseResponse{
			ID:          t.ID,
			Name:        t.Name,
			Slug:        t.Slug,
			MemberCount: int(t.MemberCount),
			IsMember:    t.IsMember,
			IsDefault:   t.IsDefault,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

type createTeamRequest struct {
	Name string `json:"name"`
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases, collapses runs of non-alphanumerics to single hyphens,
// and trims leading/trailing hyphens. Returns "" when nothing usable remains.
func slugify(name string) string {
	s := nonSlugChars.ReplaceAllString(strings.ToLower(name), "-")
	return strings.Trim(s, "-")
}

// CreateTeam creates a team and adds the caller as its first member. Any
// authenticated user may create a team (flat trust, mirroring sessions).
func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	callerID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "team name is required")
		return
	}
	slug := slugify(name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "INVALID_NAME", "team name must contain letters or numbers")
		return
	}

	team, err := h.queries.CreateTeam(r.Context(), db.CreateTeamParams{Name: name, Slug: slug})
	if err != nil {
		// Unique slug collision -> 409 so the UI can prompt for a different name.
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "SLUG_TAKEN", "a team with a similar name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to create team")
		return
	}

	if err := h.queries.AddTeamMember(r.Context(), db.AddTeamMemberParams{TeamID: team.ID, UserID: callerID}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to add creator to team")
		return
	}

	writeJSON(w, http.StatusCreated, teamBrowseResponse{
		ID:          team.ID,
		Name:        team.Name,
		Slug:        team.Slug,
		MemberCount: 1,
		IsMember:    true,
		IsDefault:   team.IsDefault,
	})
}

// JoinTeam adds the caller to a team (self-serve). Idempotent.
func (h *Handler) JoinTeam(w http.ResponseWriter, r *http.Request) {
	teamID, err := uuid.Parse(chi.URLParam(r, "teamID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TEAM_ID", "invalid team ID")
		return
	}
	callerID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	if err := h.queries.AddTeamMember(r.Context(), db.AddTeamMemberParams{TeamID: teamID, UserID: callerID}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to join team")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LeaveTeam removes the caller from a team (self-serve). Removing OTHER members
// is deferred — the path only permits self-removal. Leaving the default team is
// refused so a user can't strand themselves with zero session visibility.
func (h *Handler) LeaveTeam(w http.ResponseWriter, r *http.Request) {
	teamID, err := uuid.Parse(chi.URLParam(r, "teamID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TEAM_ID", "invalid team ID")
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
	if targetID != callerID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "can only remove yourself from a team")
		return
	}

	team, err := h.queries.GetTeam(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "TEAM_NOT_FOUND", "team not found")
		return
	}
	if team.IsDefault {
		writeError(w, http.StatusBadRequest, "CANNOT_LEAVE_DEFAULT", "cannot leave the default team")
		return
	}

	if err := h.queries.RemoveTeamMember(r.Context(), db.RemoveTeamMemberParams{TeamID: teamID, UserID: callerID}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to leave team")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
