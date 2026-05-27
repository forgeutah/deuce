package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/config"
	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/server"
)

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

	srv := server.New(pool, cfg)

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
			"hint", "set DEUCE_AUTH_MODE=forge-proxy for any non-loopback deployment",
		)
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

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
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
