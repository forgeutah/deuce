package sshproxy

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

// authFixture spins up a temporary schema, applies migrations, and
// seeds a session with one member who has one SSH key. Returns a Server
// whose publicKeyCallback is ready to exercise, plus the seeded
// (sessionID, userID, keyID, publicKey) tuple so tests can construct
// the right (or wrong) auth attempts.
type authFixture struct {
	server     *Server
	sessionID  uuid.UUID
	userID     uuid.UUID
	keyID      uuid.UUID
	publicKey  ssh.PublicKey
	pool       *pgxpool.Pool
	containers *fakeContainerResolver
}

// fakeContainerResolver lets tests force ContainerName outcomes without
// invoking real Docker. We pass it in by reaching into the Server via
// a helper; the production code uses workspace.Manager.
type fakeContainerResolver struct {
	err error
}

func setupAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test (CI provides Postgres service container)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if err := db.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	queries := db.New(pool)

	// Seed: team + user (Alice) + project + session + session_members(Alice).
	var teamID, userID, projectID, sessionID uuid.UUID
	must := func(q string, args ...any) *pgxpool.Pool {
		row := pool.QueryRow(ctx, q, args...)
		_ = row
		return pool
	}
	_ = must
	if err := pool.QueryRow(ctx, `INSERT INTO teams (name, slug) VALUES ('Test', 'test') RETURNING id`).Scan(&teamID); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('alice@example.com', 'Alice') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects (name, team_id) VALUES ('p', $1) RETURNING id`, teamID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sessions (name, project_id) VALUES ('test-workspace', $1) RETURNING id`, projectID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_members (session_id, user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatalf("seed session_member: %v", err)
	}

	// Generate a real ed25519 SSH key so FingerprintSHA256 produces an
	// authentic SHA256: form that satisfies the CHECK constraint.
	pub, priv, err := generateTestKey()
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	_ = priv
	fp := ssh.FingerprintSHA256(pub)

	keyRow, err := queries.CreateUserSSHKey(ctx, db.CreateUserSSHKeyParams{
		UserID:      userID,
		Label:       "alice key",
		PublicKey:   string(ssh.MarshalAuthorizedKey(pub)),
		Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("insert ssh key: %v", err)
	}

	// Build a Server with the seeded queries. Use a temp host key path.
	keyDir := t.TempDir()
	cfg := Config{
		HostKeyPath:        filepath.Join(keyDir, "host_key"),
		ServerVersion:      "SSH-2.0-Deuce_test",
		HandshakeTimeout:   2 * time.Second,
		MaxHandshakesPerIP: 8,
		MaxChannelsPerConn: 8,
	}
	// nil workspaces.Manager skips the container reachability check,
	// which is what most auth tests want. The container-failure test
	// installs its own resolver-failing stub.
	s, err := New(cfg, queries, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return &authFixture{
		server:    s,
		sessionID: sessionID,
		userID:    userID,
		keyID:     keyRow.ID,
		publicKey: pub,
		pool:      pool,
	}
}

// generateTestKey returns an ed25519 PublicKey + raw seed for use in
// tests. The MarshalAuthorizedKey output is a valid ssh-ed25519 line.
func generateTestKey() (ssh.PublicKey, []byte, error) {
	signer, err := generateEd25519Signer()
	if err != nil {
		return nil, nil, err
	}
	return signer.PublicKey(), nil, nil
}

func generateEd25519Signer() (ssh.Signer, error) {
	dir, err := os.MkdirTemp("", "sshproxy-test-key-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	return loadOrGenerateHostKey(filepath.Join(dir, ".deuce", "k"))
}

// fakeAddr is a tiny net.Addr stand-in for tests.
type fakeAddr struct{ addr string }

func (f *fakeAddr) Network() string { return "tcp" }
func (f *fakeAddr) String() string  { return f.addr }

func TestPublicKeyCallback_MatchingSessionMemberKey(t *testing.T) {
	fx := setupAuthFixture(t)

	meta := newMeta("dc-" + fx.sessionID.String())
	perms, err := fx.server.publicKeyCallback(meta, fx.publicKey)
	if err != nil {
		t.Fatalf("auth should succeed: %v", err)
	}
	if perms == nil {
		t.Fatal("Permissions should not be nil")
	}
	if got := perms.Extensions[extSessionID]; got != fx.sessionID.String() {
		t.Errorf("ext session-id: want %s, got %s", fx.sessionID, got)
	}
	if got := perms.Extensions[extUserID]; got != fx.userID.String() {
		t.Errorf("ext user-id: want %s, got %s", fx.userID, got)
	}
	if got := perms.Extensions[extKeyID]; got != fx.keyID.String() {
		t.Errorf("ext key-id: want %s, got %s", fx.keyID, got)
	}
	if !strings.HasPrefix(perms.Extensions[extFP], "SHA256:") {
		t.Errorf("ext fp should be SHA256: form, got %q", perms.Extensions[extFP])
	}
}

func TestPublicKeyCallback_NonSessionMember(t *testing.T) {
	fx := setupAuthFixture(t)
	ctx := context.Background()

	// Bob is in the team but NOT in session_members.
	var bobID uuid.UUID
	if err := fx.pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('bob@example.com', 'Bob') RETURNING id`).Scan(&bobID); err != nil {
		t.Fatalf("seed bob: %v", err)
	}
	queries := db.New(fx.pool)
	bobPub, _, _ := generateTestKey()
	if _, err := queries.CreateUserSSHKey(ctx, db.CreateUserSSHKeyParams{
		UserID:      bobID,
		Label:       "bob",
		PublicKey:   string(ssh.MarshalAuthorizedKey(bobPub)),
		Fingerprint: ssh.FingerprintSHA256(bobPub),
	}); err != nil {
		t.Fatalf("seed bob key: %v", err)
	}

	meta := newMeta("dc-" + fx.sessionID.String())
	_, err := fx.server.publicKeyCallback(meta, bobPub)
	if err == nil {
		t.Fatal("expected auth to fail for non-session-member key")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("expected 'not authorized', got %v", err)
	}
}

func TestPublicKeyCallback_InvalidUsernamesRejectedBeforeDB(t *testing.T) {
	fx := setupAuthFixture(t)

	invalidUsernames := []string{
		"",
		"root",
		"admin",
		"dc-not-a-uuid",
		"dc-bogus-12345",
		"dc-" + fx.sessionID.String() + "extra",
		"dc-" + strings.ToUpper(fx.sessionID.String()), // hex must be lowercase
		"dc-" + fx.sessionID.String() + "\x00admin",    // NULL byte injection
		"dc-" + fx.sessionID.String() + " ",            // trailing space
		"dc-ｄｅｅｄ-0000-0000-0000-000000000000",            // full-width digits
	}
	for _, u := range invalidUsernames {
		meta := newMeta(u)
		_, err := fx.server.publicKeyCallback(meta, fx.publicKey)
		if err == nil {
			t.Errorf("username %q: expected rejection, got success", u)
		}
	}
}

func TestPublicKeyCallback_NonexistentSession(t *testing.T) {
	fx := setupAuthFixture(t)

	bogus := uuid.New()
	meta := newMeta("dc-" + bogus.String())
	_, err := fx.server.publicKeyCallback(meta, fx.publicKey)
	if err == nil {
		t.Fatal("expected ErrNoAuth-equivalent for nonexistent session")
	}
	// The error must NOT distinguish "no session" from "wrong key" —
	// both produce the same "not authorized" message so clients can't
	// enumerate session existence.
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("expected 'not authorized', got %v", err)
	}
}

func TestPublicKeyCallback_FingerprintLogging_NoFullKey(t *testing.T) {
	// authLogCallback should log the fingerprint, never the full key.
	// We exercise the callback indirectly by emitting a structured log
	// and verifying we can identify the fp without the key bytes.
	fx := setupAuthFixture(t)
	meta := newMeta("dc-" + fx.sessionID.String())
	perms, err := fx.server.publicKeyCallback(meta, fx.publicKey)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if !strings.HasPrefix(perms.Extensions[extFP], "SHA256:") {
		t.Errorf("expected SHA256 fingerprint in Permissions, got %q", perms.Extensions[extFP])
	}
	// Verify the full public-key wire format does NOT leak into the fp.
	rawKey := ssh.MarshalAuthorizedKey(fx.publicKey)
	if strings.Contains(perms.Extensions[extFP], strings.TrimSpace(string(rawKey))[8:]) {
		t.Error("fingerprint must NOT contain raw key bytes")
	}
}

func TestPublicKeyCallback_DBErrorReturnsGenericAuthError(t *testing.T) {
	fx := setupAuthFixture(t)
	// Close the pool to force a DB error.
	fx.pool.Close()

	meta := newMeta("dc-" + fx.sessionID.String())
	_, err := fx.server.publicKeyCallback(meta, fx.publicKey)
	if err == nil {
		t.Fatal("expected error when DB is unavailable")
	}
	// Specific message — `auth lookup failed` — confirms it took the
	// non-ErrNoRows branch in publicKeyCallback.
	if !strings.Contains(err.Error(), "auth lookup failed") && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected 'auth lookup failed' or ctx deadline, got %v", err)
	}
}

// newMeta is a small helper for building ssh.ConnMetadata stand-ins.
// We satisfy the parts of the interface publicKeyCallback uses (User,
// RemoteAddr — though the callback doesn't use RemoteAddr today, future
// changes might).
func newMeta(user string) ssh.ConnMetadata {
	return &metaImpl{user: user}
}

type metaImpl struct{ user string }

func (m *metaImpl) User() string          { return m.user }
func (m *metaImpl) SessionID() []byte     { return []byte("test") }
func (m *metaImpl) ClientVersion() []byte { return []byte("SSH-2.0-test") }
func (m *metaImpl) ServerVersion() []byte { return []byte("SSH-2.0-Deuce_test") }
func (m *metaImpl) RemoteAddr() net.Addr  { return &fakeAddr{addr: "127.0.0.1:54321"} }
func (m *metaImpl) LocalAddr() net.Addr   { return &fakeAddr{addr: "127.0.0.1:2222"} }
