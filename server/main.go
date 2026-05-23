package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/config"
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

	if err := pool.Ping(ctx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

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
