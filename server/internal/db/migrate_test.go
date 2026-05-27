package db

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestMigrations_EmbedCapturesAllOnDiskFiles asserts the //go:embed directive
// matches every .sql file actually in migrations/. Guards against the
// surprising case where a new migration file lands but the embed pattern
// silently skips it (e.g., starting with an underscore, missing extension).
func TestMigrations_EmbedCapturesAllOnDiskFiles(t *testing.T) {
	embedded := MigrationsFS()

	entries, err := fs.ReadDir(embedded, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations/: %v", err)
	}
	embeddedNames := map[string]bool{}
	for _, e := range entries {
		embeddedNames[e.Name()] = true
	}

	onDisk, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read on-disk migrations/: %v", err)
	}
	for _, e := range onDisk {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if !embeddedNames[e.Name()] {
			t.Errorf("migration %q exists on disk but is missing from embed", e.Name())
		}
	}
	if len(embeddedNames) == 0 {
		t.Fatal("embedded migrations directory is empty")
	}
}

// dbURL returns the test database URL, or "" when the test should be skipped.
// CI provisions a Postgres service container and sets TEST_DATABASE_URL.
func dbURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test (CI provides Postgres service container)")
	}
	return url
}

func openTestPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test pool: %v", err)
	}
	return pool
}

// resetSchema drops and recreates the public schema so each test starts with
// an empty database. Cheaper than a full DROP DATABASE / CREATE DATABASE,
// and avoids needing CREATEDB on the test role.
func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}

func TestRunMigrations_AppliesAllEmbeddedMigrations(t *testing.T) {
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Verify goose_db_version exists and has at least one row per embedded
	// migration. Don't hard-code the count — new migrations land regularly.
	var versionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&versionCount); err != nil {
		t.Fatalf("query goose_db_version: %v", err)
	}

	embedded, err := fs.ReadDir(MigrationsFS(), "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations/: %v", err)
	}
	migrationCount := 0
	for _, e := range embedded {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			migrationCount++
		}
	}
	if versionCount != migrationCount {
		t.Errorf("applied migrations: want %d, got %d in goose_db_version", migrationCount, versionCount)
	}

	// Spot-check that a known table from migration 001 exists.
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='users')").Scan(&exists); err != nil {
		t.Fatalf("query users table existence: %v", err)
	}
	if !exists {
		t.Error("users table missing after RunMigrations")
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	var firstCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version").Scan(&firstCount); err != nil {
		t.Fatalf("count goose_db_version after first: %v", err)
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	var secondCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version").Scan(&secondCount); err != nil {
		t.Fatalf("count goose_db_version after second: %v", err)
	}

	if firstCount != secondCount {
		t.Errorf("goose_db_version row count changed on re-run: %d -> %d", firstCount, secondCount)
	}
}

// brokenFixtureFS builds a minimal goose-compatible fs.FS with one good
// migration followed by one deliberately invalid migration. Used to assert
// that RunMigrations surfaces the failure rather than leaving the DB in a
// partially-applied state.
func brokenFixtureFS(t *testing.T) fs.FS {
	t.Helper()
	dir := t.TempDir()
	must := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	must("001_create_alpha.sql", `-- +goose Up
CREATE TABLE alpha (id SERIAL PRIMARY KEY);
`)
	must("002_create_broken.sql", `-- +goose Up
CREATE TABLE beta_broken (this is not valid SQL syntax at all);
`)
	return os.DirFS(dir)
}

func TestRunMigrations_BrokenMigrationFailsAndLeavesNoOrphans(t *testing.T) {
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	err := runMigrationsAgainst(ctx, sqlDB, brokenFixtureFS(t), ".")
	if err == nil {
		t.Fatal("expected RunMigrations to fail on the broken fixture migration")
	}

	// The first migration (alpha) should have applied successfully; the
	// second (broken) must not have created beta_broken. Goose runs each
	// migration in its own transaction, so the broken migration's effects
	// roll back even though earlier migrations stay committed.
	var alphaExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='alpha')").Scan(&alphaExists); err != nil {
		t.Fatalf("query alpha table: %v", err)
	}
	if !alphaExists {
		t.Error("first valid migration should have applied; alpha table missing")
	}

	var brokenExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='beta_broken')").Scan(&brokenExists); err != nil {
		t.Fatalf("query beta_broken table: %v", err)
	}
	if brokenExists {
		t.Error("broken migration should have rolled back; beta_broken table exists")
	}
}

// TestRunMigrationsAgainst_AppliesFstestFixture is a pure-unit-style test
// that exercises the testable seam against a fstest.MapFS — kept distinct
// from the integration tests so the wiring (not the SQL execution) is
// covered even when Postgres isn't reachable.
func TestRunMigrationsAgainst_RequiresDialect(t *testing.T) {
	// Run with a real DB so SetDialect works end-to-end. With no DB this
	// test would only exercise SetDialect + UpContext failing on a closed
	// connection; not informative. Skip when no DB.
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	fixture := fstest.MapFS{
		"001_create_gamma.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE gamma (id INT);\n")},
	}
	if err := runMigrationsAgainst(ctx, sqlDB, fixture, "."); err != nil {
		t.Fatalf("runMigrationsAgainst: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='gamma')").Scan(&exists); err != nil {
		t.Fatalf("query gamma: %v", err)
	}
	if !exists {
		t.Error("gamma table should exist after applying fstest fixture")
	}
}

