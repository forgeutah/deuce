package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/agent"
	"github.com/forgeutah/deuce/server/internal/agent/pirun"
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
	pool         *pgxpool.Pool
	queries      *db.Queries
	cfg          *config.Config
	hub          *ws.Hub
	version      string
	sshAvailable func() bool

	// handler is captured at Router() time so main.go can drain the
	// handler's tracked workspace-action goroutines during shutdown.
	handler *handler.Handler

	// piRuntime / piSupervisor are set when the Pi harness is active
	// (DEUCE_AGENT_HARNESS=pi); main.go drains them via ShutdownAgents.
	piRuntime    *agent.Runtime
	piSupervisor *pirun.Supervisor
}

// ShutdownAgents stops the Pi runtime and supervisor (no-op in legacy mode).
// Called from main.go's shutdown drain.
func (s *Server) ShutdownAgents(ctx context.Context) {
	if s.piRuntime != nil {
		s.piRuntime.Shutdown()
	}
	if s.piSupervisor != nil {
		_ = s.piSupervisor.Shutdown(ctx)
	}
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

// SetSSHAvailable installs a predicate that the vscode-uri endpoint checks
// before building a URI. main.go sets this after sshproxy startup so the
// HTTP server reflects the live SSH state. Nil predicate = always available
// (e.g., tests that don't care).
func (s *Server) SetSSHAvailable(predicate func() bool) {
	s.sshAvailable = predicate
}

// Hub returns the shared WebSocket hub. main.go uses it to construct the
// reconciler before Router() starts the hub goroutine. Safe to call before
// Router() — Hub itself is created in New() and BroadcastToSession only
// writes to subscriber channels (no dependency on Run()).
func (s *Server) Hub() *ws.Hub {
	return s.hub
}

// WaitWorkspaceActions blocks until in-flight workspace lifecycle goroutines
// complete or ctx expires. main.go calls this in the shutdown drain so a
// `devpod delete` triggered just before SIGTERM doesn't continue running
// after the process reported orderly shutdown. Safe before Router() runs —
// returns nil immediately when no handler is yet attached.
func (s *Server) WaitWorkspaceActions(ctx context.Context) error {
	if s.handler == nil {
		return nil
	}
	return s.handler.WaitWorkspaceActions(ctx)
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

	// Startup recovery: flip any workspace_status sitting in a transitional
	// state (starting/stopping/rebuilding/deleting) to `failed`. Their owning
	// goroutines died with the prior process; without this, the rows would
	// sit transitional indefinitely (the reconciler skips them by design).
	// The user can recover via the Start / Rebuild endpoints.
	if err := s.queries.ResetStaleWorkspaceTransitions(context.Background()); err != nil {
		slog.Warn("failed to reset stale workspace transitions", "error", err)
	}

	// PublicHostname / SSHListenAddr come from config (U11). The vscode-uri
	// endpoint falls back to r.Host when PublicHostname is empty (dev mode);
	// proxy mode requires it via config.Validate.
	h := handler.New(s.queries, s.pool, s.hub, s.cfg.GitHubToken, wm, tm, s.cfg.WSAllowedOriginList(), s.cfg.PublicHostname, s.cfg.SSHListenAddr)
	if s.sshAvailable != nil {
		h.SetSSHAvailable(s.sshAvailable)
	}

	// Pi agent harness: run boot recovery to completion BEFORE the runtime
	// starts accepting work (KTD10 happens-before), then wire the runtime
	// into the handler.
	if err := handler.RecoverStuckTasks(context.Background(), s.queries); err != nil {
		// Abort boot: serving with crash-stuck tasks would report them live
		// in snapshots forever (KTD10 retry-then-abort).
		panic(fmt.Sprintf("pi harness boot recovery failed: %v", err))
	}
	launcher := pirun.NewDevpodLauncher(wm, s.cfg.PiProvider, s.cfg.PiModel)
	sup := pirun.NewSupervisor(launcher, s.cfg.AnthropicAPIKey)
	basePrompt := s.cfg.AgentSystemPrompt
	if basePrompt == "" {
		basePrompt = agent.DefaultBaseSystemPrompt
	}
	rt := agent.NewRuntime(agent.NewDBStore(s.pool, s.queries), sup, s.hub, basePrompt)
	rt.Start()
	h.SetRuntime(rt)
	s.piSupervisor = sup
	s.piRuntime = rt
	s.handler = h

	go s.hub.Run()

	r.Route("/api", func(r chi.Router) {
		r.Get("/version", handler.Version(s.version))
		r.Route("/me", func(r chi.Router) {
			r.Get("/", h.GetMe)
			r.Patch("/", h.UpdateMe)
			r.Route("/ssh-keys", func(r chi.Router) {
				r.Get("/", h.ListMySSHKeys)
				r.Post("/", h.CreateMySSHKey)
				r.Delete("/{keyID}", h.DeleteMySSHKey)
			})
		})
		r.Route("/teams", func(r chi.Router) {
			r.Get("/", h.ListTeams)
			r.Get("/all", h.ListAllTeams)
			r.Post("/", h.CreateTeam)
			r.Route("/{teamID}", func(r chi.Router) {
				r.Post("/join", h.JoinTeam)
				r.Delete("/members/{userID}", h.LeaveTeam)
			})
		})
		r.Get("/users", h.ListUsers)
		r.Get("/projects", h.ListProjects)
		// Single built-in agent settings (deuce). GET: any authenticated user.
		// PUT: any authenticated user — global blast radius is accepted for
		// now (no finer role model exists; audit trail deferred, see plan).
		r.Get("/agent", h.GetAgentSettings)
		r.Put("/agent", h.UpdateAgentSettings)
		r.Get("/github/orgs", h.ListGitHubOrgs)
		r.Get("/github/repos", h.ListGitHubRepos)

		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", h.ListSessions)
			r.Post("/", h.CreateSession)
			r.Route("/{sessionID}", func(r chi.Router) {
				r.Get("/", h.GetSession)
				r.Patch("/", h.UpdateSession)
				r.Post("/join", h.JoinSession)
				r.Post("/members", h.AddSessionMember)
				r.Delete("/members/{userID}", h.RemoveSessionMember)
				r.Get("/messages", h.ListMessages)
				r.Post("/messages", h.SendMessage)
				r.Get("/activities", h.ListActivities)
				r.Get("/agent-runs", h.AgentRunsSnapshot)
				r.Get("/files", h.ListFiles)
				r.Get("/files/content", h.GetFileContent)
				r.Get("/vscode-uri", h.GetSessionVSCodeURI)
				r.Post("/agent/stop", h.StopAgent)
				r.Route("/workspace", func(r chi.Router) {
					r.Post("/start", h.StartWorkspace)
					r.Post("/stop", h.StopWorkspace)
					r.Post("/rebuild", h.RebuildWorkspace)
					r.Post("/delete", h.DeleteWorkspace)
				})
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
