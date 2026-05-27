package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// userSSHKeysFixture spins up an isolated schema, applies all embedded
// migrations, and returns a Queries handle plus a seeded user + session.
// Each test gets its own database state — no shared fixtures.
func userSSHKeysFixture(t *testing.T) (*Queries, uuid.UUID, uuid.UUID, *fixtureCleanup) {
	t.Helper()
	url := dbURL(t)
	pool := openTestPool(t, url)
	resetSchema(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	q := New(pool)

	// Seed a team, user, project, session, and session_members row so the
	// session-member key lookup query has something to join against.
	var teamID, userID, projectID, sessionID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO teams (name, slug) VALUES ('Test Team', 'test-team') RETURNING id`).Scan(&teamID); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('alice@example.com', 'Alice') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects (name, team_id) VALUES ('Test Project', $1) RETURNING id`, teamID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sessions (name, project_id) VALUES ('Test Session', $1) RETURNING id`, projectID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_members (session_id, user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatalf("seed session_member: %v", err)
	}

	return q, userID, sessionID, &fixtureCleanup{pool: pool, ctx: ctx}
}

type fixtureCleanup struct {
	pool interface{ Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) }
	ctx  context.Context
}

// validFingerprint produces a unique-looking but format-valid SHA256
// fingerprint. The CHECK constraint requires the OpenSSH SHA256: format,
// 43 base64 chars + optional '=' padding.
func validFingerprint(seed byte) string {
	body := strings.Repeat(string(seed), 43)
	// Replace any non-base64 chars with 'A' to satisfy the regex.
	body = strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' {
			return r
		}
		return 'A'
	}, body)
	return "SHA256:" + body
}

func TestUserSSHKeys_DuplicateUserFingerprintFails(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	fp := validFingerprint('B')
	_, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID:      userID,
		Label:       "first",
		PublicKey:   "ssh-ed25519 AAAA first",
		Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err = q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID:      userID,
		Label:       "duplicate",
		PublicKey:   "ssh-ed25519 AAAA second",
		Fingerprint: fp,
	})
	if err == nil {
		t.Fatal("expected unique-constraint violation on duplicate (user_id, fingerprint)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("expected SQLSTATE 23505, got %v", err)
	}
}

func TestUserSSHKeys_DifferentUsersSameFingerprintSucceed(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	url := dbURL(t)
	pool := openTestPool(t, url)
	var bobID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('bob@example.com', 'Bob') RETURNING id`).Scan(&bobID); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	fp := validFingerprint('C')

	if _, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "alice", PublicKey: "ssh-ed25519 AAAA", Fingerprint: fp,
	}); err != nil {
		t.Fatalf("alice insert: %v", err)
	}
	if _, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: bobID, Label: "bob", PublicKey: "ssh-ed25519 AAAA", Fingerprint: fp,
	}); err != nil {
		t.Fatalf("bob insert (same fp, different user): %v", err)
	}
}

func TestUserSSHKeys_PublicKeyTooLongRejected(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	overlong := strings.Repeat("a", 8193)
	_, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "too long", PublicKey: overlong, Fingerprint: validFingerprint('D'),
	})
	if err == nil {
		t.Fatal("expected CHECK violation for public_key > 8192 bytes")
	}
	if !strings.Contains(err.Error(), "user_ssh_keys_public_key_check") {
		t.Errorf("expected public_key CHECK violation, got %v", err)
	}
}

func TestUserSSHKeys_InvalidFingerprintFormatRejected(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	_, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "bad fp", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "not-a-valid-fingerprint",
	})
	if err == nil {
		t.Fatal("expected CHECK violation for malformed fingerprint")
	}
	if !strings.Contains(err.Error(), "user_ssh_keys_fingerprint_check") {
		t.Errorf("expected fingerprint CHECK violation, got %v", err)
	}
}

func TestUserSSHKeys_LookupSessionMemberKey_Found(t *testing.T) {
	q, userID, sessionID, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	fp := validFingerprint('E')
	created, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "alice key", PublicKey: "ssh-ed25519 AAAA", Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	got, err := q.LookupSessionMemberKeyByFingerprint(ctx, LookupSessionMemberKeyByFingerprintParams{
		SessionID: sessionID, Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("returned key ID mismatch: want %s, got %s", created.ID, got.ID)
	}
	if got.UserID != userID {
		t.Errorf("returned user_id mismatch: want %s, got %s", userID, got.UserID)
	}
}

func TestUserSSHKeys_LookupSessionMemberKey_NonMemberRejected(t *testing.T) {
	q, _, sessionID, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	url := dbURL(t)
	pool := openTestPool(t, url)

	// Carol is in the team but NOT in session_members for this session.
	var carolID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('carol@example.com', 'Carol') RETURNING id`).Scan(&carolID); err != nil {
		t.Fatalf("seed carol: %v", err)
	}
	fp := validFingerprint('F')
	if _, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: carolID, Label: "carol", PublicKey: "ssh-ed25519 AAAA", Fingerprint: fp,
	}); err != nil {
		t.Fatalf("create carol key: %v", err)
	}

	_, err := q.LookupSessionMemberKeyByFingerprint(ctx, LookupSessionMemberKeyByFingerprintParams{
		SessionID: sessionID, Fingerprint: fp,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows for non-session-member, got %v", err)
	}
}

func TestUserSSHKeys_TouchLastUsed_SamplesByOneMinute(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	created, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "k", PublicKey: "ssh-ed25519 AAAA", Fingerprint: validFingerprint('G'),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	url := dbURL(t)
	pool := openTestPool(t, url)

	// First touch sets last_used_at from NULL.
	if err := q.TouchUserSSHKeyLastUsed(ctx, created.ID); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	var firstTouch time.Time
	if err := pool.QueryRow(ctx, `SELECT last_used_at FROM user_ssh_keys WHERE id = $1`, created.ID).Scan(&firstTouch); err != nil {
		t.Fatalf("read first touch: %v", err)
	}
	if firstTouch.IsZero() {
		t.Fatal("first touch did not set last_used_at")
	}

	// Second touch within 60s should be a no-op (sampling).
	if err := q.TouchUserSSHKeyLastUsed(ctx, created.ID); err != nil {
		t.Fatalf("second touch: %v", err)
	}
	var secondTouch time.Time
	if err := pool.QueryRow(ctx, `SELECT last_used_at FROM user_ssh_keys WHERE id = $1`, created.ID).Scan(&secondTouch); err != nil {
		t.Fatalf("read second touch: %v", err)
	}
	if !secondTouch.Equal(firstTouch) {
		t.Errorf("second touch within sampling window should be a no-op; got %v vs %v", secondTouch, firstTouch)
	}
}

func TestUserSSHKeys_DeleteScopedToOwningUser(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	url := dbURL(t)
	pool := openTestPool(t, url)

	var bobID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('bob@example.com', 'Bob') RETURNING id`).Scan(&bobID); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	aliceKey, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "alice", PublicKey: "ssh-ed25519 AAAA alice", Fingerprint: validFingerprint('H'),
	})
	if err != nil {
		t.Fatalf("create alice key: %v", err)
	}

	// Bob tries to delete Alice's key — should be a no-op (no rows affected).
	if err := q.DeleteUserSSHKey(ctx, DeleteUserSSHKeyParams{ID: aliceKey.ID, UserID: bobID}); err != nil {
		t.Fatalf("scoped delete attempt: %v", err)
	}

	// Alice's key still exists.
	got, err := q.GetUserSSHKey(ctx, GetUserSSHKeyParams{ID: aliceKey.ID, UserID: userID})
	if err != nil {
		t.Fatalf("alice key was incorrectly deleted by bob's call: %v", err)
	}
	if got.ID != aliceKey.ID {
		t.Errorf("key ID mismatch after scoped-delete attempt")
	}
}

func TestUserSSHKeys_CascadeDeleteRemovesKeys(t *testing.T) {
	q, userID, _, _ := userSSHKeysFixture(t)
	ctx := context.Background()

	_, err := q.CreateUserSSHKey(ctx, CreateUserSSHKeyParams{
		UserID: userID, Label: "k", PublicKey: "ssh-ed25519 AAAA", Fingerprint: validFingerprint('I'),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	url := dbURL(t)
	pool := openTestPool(t, url)
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	keys, err := q.ListUserSSHKeys(ctx, userID)
	if err != nil {
		t.Fatalf("list after cascade: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("cascade delete should remove keys; got %d remaining", len(keys))
	}
}
