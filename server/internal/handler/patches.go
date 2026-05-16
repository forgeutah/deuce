package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// patchSummary is the slim shape used for WebSocket broadcasts and the list endpoint.
// It deliberately omits hunks; consumers fetch the full payload via GetPatch when
// they need the diff content. See the patch-stream-primitive plan, KTD #3.
type patchSummary struct {
	ID                 uuid.UUID  `json:"id"`
	SessionID          uuid.UUID  `json:"sessionId"`
	ProducingMessageID *uuid.UUID `json:"producingMessageId"`
	ParentPatchID      *uuid.UUID `json:"parentPatchId"`
	OriginType         string     `json:"originType"`
	WorkspaceSha       string     `json:"workspaceSha"`
	CommittedSha       *string    `json:"committedSha"`
	FileCount          int32      `json:"fileCount"`
	HunkCount          int32      `json:"hunkCount"`
	FailedMidTurn      bool       `json:"failedMidTurn"`
	CreatedAt          time.Time  `json:"createdAt"`
}

// patchResponse extends patchSummary with the full hunks JSONB.
type patchResponse struct {
	patchSummary
	Hunks json.RawMessage `json:"hunks"`
}

// toPatchSummary maps a sqlc-generated db.Patch into the slim wire shape. This is
// the canonical conversion used by both the REST list endpoint and the WebSocket
// patch_created broadcast.
func toPatchSummary(p db.Patch) patchSummary {
	var producingMessageID *uuid.UUID
	if p.ProducingMessageID.Valid {
		id := uuid.UUID(p.ProducingMessageID.Bytes)
		producingMessageID = &id
	}
	var parentPatchID *uuid.UUID
	if p.ParentPatchID.Valid {
		id := uuid.UUID(p.ParentPatchID.Bytes)
		parentPatchID = &id
	}
	var committedSha *string
	if p.CommittedSha.Valid {
		s := p.CommittedSha.String
		committedSha = &s
	}
	return patchSummary{
		ID:                 p.ID,
		SessionID:          p.SessionID,
		ProducingMessageID: producingMessageID,
		ParentPatchID:      parentPatchID,
		OriginType:         p.OriginType,
		WorkspaceSha:       p.WorkspaceSha,
		CommittedSha:       committedSha,
		FileCount:          p.FileCount,
		HunkCount:          p.HunkCount,
		FailedMidTurn:      p.FailedMidTurn,
		CreatedAt:          p.CreatedAt,
	}
}

func toPatchResponse(p db.Patch) patchResponse {
	hunks := json.RawMessage("null")
	if p.Hunks != nil {
		hunks = p.Hunks
	}
	return patchResponse{
		patchSummary: toPatchSummary(p),
		Hunks:        hunks,
	}
}

func (h *Handler) ListPatches(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	patches, err := h.queries.ListPatchesBySession(r.Context(), db.ListPatchesBySessionParams{
		SessionID: sessionID,
		Limit:     int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list patches")
		return
	}

	result := make([]patchSummary, 0, len(patches))
	for _, p := range patches {
		result = append(result, toPatchSummary(p))
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetPatch(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}
	patchID, err := uuid.Parse(chi.URLParam(r, "patchID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PATCH_ID", "invalid patch ID")
		return
	}

	// Two-column lookup: requires both session_id AND id to match. Prevents the
	// cross-session ID-oracle where a known patch UUID under a different session's
	// URL would otherwise return the full hunk payload.
	patch, err := h.queries.GetPatchBySessionAndID(r.Context(), db.GetPatchBySessionAndIDParams{
		SessionID: sessionID,
		ID:        patchID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PATCH_NOT_FOUND", "patch not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to get patch")
		return
	}

	writeJSON(w, http.StatusOK, toPatchResponse(patch))
}
