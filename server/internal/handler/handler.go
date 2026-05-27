package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/agent"
	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/terminal"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type Handler struct {
	queries        *db.Queries
	pool           *pgxpool.Pool
	hub            *ws.Hub
	githubToken    string
	workspaces     *workspace.Manager
	terminals      *terminal.Manager
	executor       *agent.Executor
	agentQueue     *agent.Queue
	wsOrigins      []string
	publicHostname string
	sshListenAddr  string
}

// New constructs a Handler. publicHostname and sshListenAddr feed the
// vscode:// URI builder in GetSessionVSCodeURI. Both are placeholders until
// U11 wires real config values; for now callers pass empty strings and the
// handler falls back to the request Host header / default port 2222.
func New(queries *db.Queries, pool *pgxpool.Pool, hub *ws.Hub, githubToken string, wm *workspace.Manager, tm *terminal.Manager, exec *agent.Executor, aq *agent.Queue, wsOrigins []string, publicHostname, sshListenAddr string) *Handler {
	return &Handler{
		queries:        queries,
		pool:           pool,
		hub:            hub,
		githubToken:    githubToken,
		workspaces:     wm,
		terminals:      tm,
		executor:       exec,
		agentQueue:     aq,
		wsOrigins:      wsOrigins,
		publicHostname: publicHostname,
		sshListenAddr:  sshListenAddr,
	}
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
