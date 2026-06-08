package db

import (
	"context"
	"testing"
	"time"
)

// TestDefaultTeam_SingletonInvariant verifies migration 011 leaves exactly one
// default team and that the partial unique index rejects a second one.
func TestDefaultTeam_SingletonInvariant(t *testing.T) {
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	q := New(pool)

	// Exactly one default team exists after migration (seed data provides
	// teams, so the backfill marks the earliest one).
	def, err := q.GetDefaultTeam(ctx)
	if err != nil {
		t.Fatalf("GetDefaultTeam: %v", err)
	}
	if !def.IsDefault {
		t.Fatalf("GetDefaultTeam returned a non-default team")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM teams WHERE is_default").Scan(&count); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 default team, got %d", count)
	}

	// The partial unique index must reject promoting a second team to default.
	_, err = pool.Exec(ctx,
		`UPDATE teams SET is_default = true WHERE id <> $1`, def.ID)
	if err == nil {
		t.Fatalf("expected unique-index violation when marking a second default team")
	}
}

// TestAddTeamMember_Idempotent verifies the ON CONFLICT DO NOTHING semantics
// the provisioning path relies on.
func TestAddTeamMember_Idempotent(t *testing.T) {
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	q := New(pool)

	def, err := q.GetDefaultTeam(ctx)
	if err != nil {
		t.Fatalf("GetDefaultTeam: %v", err)
	}

	user, err := q.CreateUserByEmail(ctx, CreateUserByEmailParams{
		Email: "newbie@example.com",
		Name:  "Newbie",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := q.AddTeamMember(ctx, AddTeamMemberParams{TeamID: def.ID, UserID: user.ID}); err != nil {
			t.Fatalf("AddTeamMember #%d: %v", i+1, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM team_members WHERE team_id=$1 AND user_id=$2", def.ID, user.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("idempotent add should leave 1 row, got %d", n)
	}
}
