package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies all pending forward (`Up`) migrations against pool.
//
// Goose acquires a Postgres advisory lock before applying, so it is safe to
// call from multiple processes that boot simultaneously; one wins, the
// others observe an already-up-to-date schema.
//
// Migrations are forward-only by convention. Rolling back to a prior image
// tag does not down-migrate; operators must not roll back across destructive
// migration boundaries.
//
// On any error, the function returns the goose error wrapped with context.
// Callers should treat this as fatal — the binary must exit non-zero rather
// than serve traffic against a partially-applied schema.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Convert the pgxpool to a *sql.DB just for goose. The adapter shares
	// the underlying pgxpool connections — we close the *sql.DB wrapper at
	// the end but the pool itself stays alive for application use.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	return runMigrationsAgainst(ctx, sqlDB, migrationsFS, "migrations")
}

// runMigrationsAgainst is the testable seam: it takes any *sql.DB and any
// fs.FS so tests can inject a broken-migration fixture (fstest.MapFS,
// os.DirFS pointed at testdata, etc.) without touching the production embed.
func runMigrationsAgainst(ctx context.Context, sqlDB *sql.DB, migrations fs.FS, dir string) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, dir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrationsFS exposes the embedded migrations filesystem so tests can
// enumerate the bundled files. Returned as fs.FS to keep callers loose.
func MigrationsFS() fs.FS {
	return migrationsFS
}
