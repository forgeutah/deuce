package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/agent"
	"github.com/forgeutah/deuce/server/internal/auth"
	"github.com/forgeutah/deuce/server/internal/config"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/handler"
	"github.com/forgeutah/deuce/server/internal/terminal"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type Server struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	cfg     *config.Config
	hub     *ws.Hub
}

func New(pool *pgxpool.Pool, cfg *config.Config) *Server {
	return &Server{
		pool:    pool,
		queries: db.New(pool),
		cfg:     cfg,
		hub:     ws.NewHub(),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "X-User-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(auth.Middleware(s.cfg.UserID))

	wm := workspace.NewManager(s.cfg.DevPodBin, s.cfg.DevPodProvider)
	if !wm.Available() {
		slog.Warn("devpod binary not found, workspace creation will be skipped")
	} else {
		if err := wm.EnsureDockerProvider(context.Background()); err != nil {
			slog.Warn("failed to ensure docker provider", "error", err)
		}
	}

	tm := terminal.NewManager()

	// Create agent executor and queue
	exec := agent.NewExecutor(wm, s.cfg.AnthropicAPIKey)
	aq := agent.NewQueue()

	// Startup recovery: reset stale agent statuses from prior server crash
	if err := s.queries.ResetStaleAgentStatuses(context.Background()); err != nil {
		slog.Warn("failed to reset stale agent statuses", "error", err)
	}

	h := handler.New(s.queries, s.pool, s.hub, s.cfg.GitHubToken, wm, tm, exec, aq)

	go s.hub.Run()

	r.Route("/api", func(r chi.Router) {
		r.Get("/me", h.GetMe)
		r.Get("/teams", h.ListTeams)
		r.Get("/projects", h.ListProjects)
		r.Get("/agents", h.ListAgents)
		r.Post("/agents", h.CreateAgent)
		r.Put("/agents/{agentID}", h.UpdateAgent)
		r.Delete("/agents/{agentID}", h.DeleteAgent)
		r.Get("/github/orgs", h.ListGitHubOrgs)
		r.Get("/github/repos", h.ListGitHubRepos)

		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", h.ListSessions)
			r.Post("/", h.CreateSession)
			r.Route("/{sessionID}", func(r chi.Router) {
				r.Get("/", h.GetSession)
				r.Patch("/", h.UpdateSession)
				r.Get("/messages", h.ListMessages)
				r.Post("/messages", h.SendMessage)
				r.Get("/activities", h.ListActivities)
				r.Put("/agents", h.UpdateSessionAgents)
				r.Post("/agents/stop", h.StopAgent)
			})
		})
	})

	r.Get("/ws", h.HandleWebSocket)
	r.Get("/ws/terminal/{sessionID}", h.HandleTerminalWebSocket)

	return r
}
