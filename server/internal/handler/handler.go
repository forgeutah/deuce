package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type Handler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	hub     *ws.Hub
}

func New(queries *db.Queries, pool *pgxpool.Pool, hub *ws.Hub) *Handler {
	return &Handler{queries: queries, pool: pool, hub: hub}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func getUserID(r *http.Request) string {
	return auth.GetUserID(r.Context())
}
