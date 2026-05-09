package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/auth"
	"github.com/forgeutah/deuce/server/internal/config"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/handler"
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

	h := handler.New(s.queries, s.pool, s.hub)

	go s.hub.Run()

	r.Route("/api", func(r chi.Router) {
		r.Get("/me", h.GetMe)
		r.Get("/teams", h.ListTeams)
		r.Get("/projects", h.ListProjects)
		r.Get("/agents", h.ListAgents)

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
			})
		})
	})

	r.Get("/ws", h.HandleWebSocket)

	return r
}
