package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

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
	"github.com/forgeutah/deuce/server/internal/web"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type Server struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	cfg     *config.Config
	hub     *ws.Hub
	version string
}

// New constructs a Server. version is the build-injected version string
// surfaced at GET /api/version and on startup logs. Pass "dev" for non-release
// builds.
func New(pool *pgxpool.Pool, cfg *config.Config, version string) *Server {
	return &Server{
		pool:    pool,
		queries: db.New(pool),
		cfg:     cfg,
		hub:     ws.NewHub(),
		version: version,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsAllowedOrigins(s.cfg),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   corsAllowedHeaders(s.cfg),
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// The configured proxy-secret header (cfg.ProxyHeaderSecret) must never
	// appear in logs. chi's middleware.Logger records method/path/status/
	// duration only — if anyone introduces a logging middleware that dumps
	// headers, add explicit redaction for cfg.ProxyHeaderSecret and the rest
	// of the configured DEUCE_PROXY_HEADER_* set.
	if s.cfg.AuthMode == config.AuthModeProxy {
		r.Use(auth.ProxyMiddleware(s.queries, auth.ProxyConfigFromConfig(s.cfg)))
	} else {
		r.Use(auth.Middleware(s.cfg.UserID))
	}

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

	// publicHostname and sshListenAddr are placeholders until U11 wires real
	// config values (DEUCE_PUBLIC_HOSTNAME, DEUCE_SSH_LISTEN_ADDR). For now
	// the URI builder falls back to r.Host and port 2222.
	h := handler.New(s.queries, s.pool, s.hub, s.cfg.GitHubToken, wm, tm, exec, aq, s.cfg.WSAllowedOriginList(), "", "")

	go s.hub.Run()

	r.Route("/api", func(r chi.Router) {
		r.Get("/version", handler.Version(s.version))
		r.Get("/me", h.GetMe)
		r.Patch("/me", h.UpdateMe)
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
				r.Get("/files", h.ListFiles)
				r.Get("/files/content", h.GetFileContent)
				r.Get("/vscode-uri", h.GetSessionVSCodeURI)
				r.Put("/agents", h.UpdateSessionAgents)
				r.Post("/agents/stop", h.StopAgent)
			})
		})
	})

	r.Get("/ws", h.HandleWebSocket)
	r.Get("/ws/terminal/{sessionID}", h.HandleTerminalWebSocket)

	// Catch-all static handler for the embedded Vite SPA. Mounted last so the
	// /api and /ws routes above take precedence — chi resolves most-specific
	// first. Returns 404 for missing /assets/* (hashed-asset misses) and
	// falls back to index.html for unknown application routes.
	r.Handle("/*", web.Handler())

	return r
}

// corsAllowedOrigins returns the CORS AllowedOrigins list. In dev mode it
// admits the localhost Vite (4000) and Go (8080) origins so the SPA can hit
// the API from either host. In proxy mode it reuses DEUCE_WS_ALLOWED_ORIGINS
// — the same trust boundary that gates WebSocket upgrades — prefixed with
// https:// since the hosted deployment runs behind TLS at the proxy.
// Operators who need both http:// and https:// can list each explicitly in
// the env var (each origin shipped verbatim, no scheme synthesis).
func corsAllowedOrigins(cfg *config.Config) []string {
	if cfg.AuthMode == config.AuthModeProxy {
		raw := cfg.WSAllowedOriginList()
		out := make([]string, 0, len(raw)*2)
		for _, o := range raw {
			if strings.Contains(o, "://") {
				out = append(out, o)
				continue
			}
			// Bare hostnames: synthesize https:// (production) and http://
			// (dev/staging behind plain HTTP). The proxy decides which is
			// actually reachable.
			out = append(out, "https://"+o, "http://"+o)
		}
		return out
	}
	return []string{"http://localhost:4000", "http://localhost:8080"}
}

// corsAllowedHeaders computes the CORS AllowedHeaders list from the
// configured proxy header names. The base set covers the standard request
// headers plus X-User-ID (dev-mode pass-through). In proxy mode, each
// non-empty DEUCE_PROXY_HEADER_* value joins the list so browser preflight
// for those headers succeeds. The middleware ignores X-User-ID in proxy
// mode regardless — CORS only governs what the browser is allowed to send.
func corsAllowedHeaders(cfg *config.Config) []string {
	out := []string{"Accept", "Content-Type", "X-User-ID"}
	if cfg.AuthMode != config.AuthModeProxy {
		return out
	}
	for _, h := range []string{
		cfg.ProxyHeaderEmail,
		cfg.ProxyHeaderName,
		cfg.ProxyHeaderAvatar,
		cfg.ProxyHeaderSecret,
		cfg.ProxyHeaderContractVersion,
		cfg.ProxyHeaderRoles,
	} {
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}
