package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/config"
	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/reconcile"
	"github.com/forgeutah/deuce/server/internal/server"
	"github.com/forgeutah/deuce/server/internal/sshproxy"
	"github.com/forgeutah/deuce/server/internal/workspace"
)

// Version is set at build time via -ldflags="-X main.Version=<tag>". The
// release Dockerfile and the release-build Makefile target both inject the
// git tag here. Default "dev" for unflagged builds (go run, go build with
// no flags, IDE builds).
var Version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := waitForDatabase(ctx, pool); err != nil {
		slog.Error("database never became reachable", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	// Run goose migrations in-process before the HTTP listener binds. Failing
	// here exits the process — Compose's restart policy will surface the loop
	// without partial-serving. Forward-only by convention: rolling back to a
	// prior image tag does not down-migrate.
	migrateCtx, migrateCancel := context.WithTimeout(ctx, 60*time.Second)
	if err := db.RunMigrations(migrateCtx, pool); err != nil {
		migrateCancel()
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	migrateCancel()
	slog.Info("migrations applied")

	slog.Info("deuce starting", "version", Version)
	srv := server.New(pool, cfg, Version)

	// SSH proxy startup. Empty SSHListenAddr disables the listener entirely
	// (HTTP keeps serving; /api/sessions/:id/vscode-uri returns 503). Any
	// startup failure (bad host key, port in use, permissive mode) is
	// degraded — not fatal — because HTTP is the load-bearing surface.
	var sshSrv *sshproxy.Server
	var sshUp atomic.Bool
	if cfg.SSHListenAddr == "" {
		slog.Info("ssh listener disabled by config (DEUCE_SSH_LISTEN_ADDR is empty)")
	} else {
		hostKeyPath := cfg.SSHHostKeyPath
		if hostKeyPath == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				slog.Error("ssh: cannot resolve user home dir", "error", herr)
			} else {
				hostKeyPath = filepath.Join(home, ".deuce", "ssh_host_ed25519_key")
			}
		}
		sshCfg := sshproxy.Config{
			ListenAddr:         cfg.SSHListenAddr,
			HostKeyPath:        hostKeyPath,
			ServerVersion:      "SSH-2.0-Deuce_" + Version,
			HandshakeTimeout:   time.Duration(cfg.SSHHandshakeTimeoutSec) * time.Second,
			MaxHandshakesPerIP: cfg.SSHMaxHandshakesPerIP,
			MaxChannelsPerConn: cfg.SSHMaxChannelsPerConn,
			GoroutineCap:       cfg.SSHGoroutineCap,
		}
		wm := workspace.NewManager(cfg.DevPodBin, cfg.DevPodProvider)
		s, err := sshproxy.New(sshCfg, db.New(pool), wm)
		if err != nil {
			slog.Error("ssh proxy startup failed — HTTP continues without VS Code remote access", "error", err)
		} else {
			sshSrv = s
			sshUp.Store(true)
			go func() {
				if listenErr := sshSrv.ListenAndServe(cfg.SSHListenAddr); listenErr != nil && !errors.Is(listenErr, net.ErrClosed) {
					slog.Error("ssh listener exited unexpectedly — disabling vscode-uri endpoint", "error", listenErr)
					sshUp.Store(false)
				}
			}()
		}
	}
	srv.SetSSHAvailable(sshUp.Load)

	// Workspace-state reconciler: a single goroutine that polls `docker ps`
	// every 10s and writes truth into sessions.workspace_status when reality
	// drifts. Shares the same shutdown context as HTTP and SSH so SIGTERM
	// drains all three together. Docker-provider-only by design — the
	// BulkContainerStatus method assumes the docker DevPod provider.
	reconWM := workspace.NewManager(cfg.DevPodBin, cfg.DevPodProvider)
	reconciler := reconcile.New(db.New(pool), srv.Hub(), reconWM, reconWM, 10*time.Second)
	go reconciler.Run(ctx)

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Plan U2 / Key Technical Decisions: dev mode trusts client-supplied
	// X-User-ID and is safe only on a loopback bind. Bare ":<port>" binds
	// every interface (effectively 0.0.0.0), which is unsafe on a shared
	// host. This is a WARN not a fatal because container network
	// namespacing legitimately uses 0.0.0.0 — operators may know what
	// they're doing — but the warning makes the foot-gun visible.
	if cfg.AuthMode == config.AuthModeDev {
		slog.Warn("dev mode active — server trusts client-supplied X-User-ID; safe only on a loopback bind",
			"auth_mode", cfg.AuthMode,
			"addr", addr,
			"hint", "set DEUCE_AUTH_MODE=proxy and configure DEUCE_PROXY_HEADER_* env vars for any non-loopback deployment",
		)
	}

	// Proxy mode: surface every disabled optional check at startup so an
	// operator who unintentionally removed an env var sees the regression
	// before it bites. Each disabled check means "no application-layer
	// gate; trust the network boundary fully." When all three are
	// disabled, the boot log says so explicitly.
	if cfg.AuthMode == config.AuthModeProxy {
		var disabled []string
		if !cfg.ProxySecretCheckEnabled() {
			disabled = append(disabled, "shared-secret")
		}
		if !cfg.ProxyContractCheckEnabled() {
			disabled = append(disabled, "contract-version")
		}
		if !cfg.ProxyRoleCheckEnabled() {
			disabled = append(disabled, "required-role")
		}
		if len(disabled) > 0 {
			slog.Warn("proxy mode active — application-layer checks disabled, admission depends entirely on network ingress",
				"auth_mode", cfg.AuthMode,
				"disabled_checks", disabled,
				"hint", "ensure Deuce binds only on loopback or a private overlay network where the configured proxy is the sole ingress",
			)
		}
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port, "auth_mode", cfg.AuthMode)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	// Shut down HTTP and SSH concurrently under the same 10s deadline. Both
	// listeners share fate but neither blocks the other's drain.
	httpDone := make(chan error, 1)
	go func() { httpDone <- httpServer.Shutdown(shutdownCtx) }()

	if sshSrv != nil {
		if err := sshSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("ssh shutdown error", "error", err)
		}
	}

	if err := reconciler.Shutdown(shutdownCtx); err != nil {
		slog.Error("reconciler shutdown error", "error", err)
	}

	if err := <-httpDone; err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}

// waitForDatabase pings pool with exponential backoff + jitter, up to 5
// attempts capped at a 60s total budget. Tolerates a Postgres sidecar that
// becomes ready slightly after this process — Compose `service_healthy`
// covers the common case, but a same-host docker-compose restart can race
// it. Returns the last ping error if every attempt fails.
func waitForDatabase(parent context.Context, pool *pgxpool.Pool) error {
	deadlineCtx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()

	const maxAttempts = 5
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pingCtx, pingCancel := context.WithTimeout(deadlineCtx, 5*time.Second)
		err := pool.Ping(pingCtx)
		pingCancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		// Exponential backoff with full jitter, capped at 8s.
		base := min(time.Duration(1<<attempt)*time.Second, 8*time.Second)
		sleep := time.Duration(rand.Int63n(int64(base)))
		slog.Info("database not ready, retrying", "attempt", attempt, "sleep", sleep, "error", err)
		select {
		case <-time.After(sleep):
		case <-deadlineCtx.Done():
			return errors.Join(lastErr, deadlineCtx.Err())
		}
	}
	return lastErr
}
