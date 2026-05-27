package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// maxDisplayNameLen caps display names so a hostile or careless client
// cannot store a multi-MB name and force every downstream serialization
// to pay for it. 100 is generous for human names and matches the rough
// ceiling shown by other social products' display-name fields.
const maxDisplayNameLen = 100

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	user, err := h.queries.GetUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// UpdateMe lets the authenticated user update their own display name.
// This is the endpoint the welcome screen posts to when a user provisioned
// without a name header (exe.dev-style proxy) chooses one. The frontend
// gates the rest of the app on a non-empty name.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "INVALID_NAME", "display name cannot be empty")
		return
	}
	if len(name) > maxDisplayNameLen {
		writeError(w, http.StatusBadRequest, "INVALID_NAME", "display name too long")
		return
	}

	user, err := h.queries.UpdateUserName(r.Context(), db.UpdateUserNameParams{
		ID:   userID,
		Name: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, user)
}
