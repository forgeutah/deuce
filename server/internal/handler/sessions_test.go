package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
)

// vscodeURIFixture spins up an isolated DB schema, applies migrations, and
// seeds a team / project / session / member so the vscode-uri handler can
// be exercised end-to-end. Mirrors the userSSHKeysFixture pattern in the db
// package.
type vscodeURIFixture struct {
	pool      *pgxpool.Pool
	queries   *db.Queries
	handler   *Handler
	router    chi.Router
	userID    uuid.UUID
	sessionID uuid.UUID
	session   db.Session
}

func newVSCodeURIFixture(t *testing.T, publicHostname, sshAddr string) *vscodeURIFixture {
	t.Helper()
	dburl := os.Getenv("TEST_DATABASE_URL")
	if dburl == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dburl)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping pool: %v", err)
	}
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if err := db.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	q := db.New(pool)

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
	if err := pool.QueryRow(ctx, `INSERT INTO sessions (name, project_id) VALUES ('alice-workspace', $1) RETURNING id`, projectID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_members (session_id, user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatalf("seed session_member: %v", err)
	}

	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get seeded session: %v", err)
	}

	// Construct handler with zero/empty deps — only the queries field and
	// the two URI-builder fields are exercised by GetSessionVSCodeURI.
	h := New(q, pool, nil, "", nil, nil, nil, nil, nil, publicHostname, sshAddr)

	// Build a chi router so chi.URLParam works inside the handler, and
	// inject the seeded userID via the existing auth middleware. The
	// middleware reads X-User-ID; passing "" as the default forces the
	// header to drive identity.
	r := chi.NewRouter()
	r.Use(auth.Middleware(""))
	r.Get("/api/sessions/{sessionID}/vscode-uri", h.GetSessionVSCodeURI)

	return &vscodeURIFixture{
		pool:      pool,
		queries:   q,
		handler:   h,
		router:    r,
		userID:    userID,
		sessionID: sessionID,
		session:   session,
	}
}

// validFingerprintForHandler builds a SHA256-formatted fingerprint that
// satisfies the user_ssh_keys CHECK constraint. Distinct seed bytes produce
// distinct fingerprints so the per-user uniqueness index doesn't trip.
func validFingerprintForHandler(seed byte) string {
	body := strings.Repeat(string(seed), 43)
	body = strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' {
			return r
		}
		return 'A'
	}, body)
	return "SHA256:" + body
}

// seedKey inserts a key for the fixture's seeded user.
func (f *vscodeURIFixture) seedKey(t *testing.T, fpSeed byte) {
	t.Helper()
	_, err := f.queries.CreateUserSSHKey(context.Background(), db.CreateUserSSHKeyParams{
		UserID:      f.userID,
		Label:       "test key",
		PublicKey:   "ssh-ed25519 AAAA test",
		Fingerprint: validFingerprintForHandler(fpSeed),
	})
	if err != nil {
		t.Fatalf("seed SSH key: %v", err)
	}
}

func (f *vscodeURIFixture) do(t *testing.T, method, path, asUserID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if asUserID != "" {
		req.Header.Set("X-User-ID", asUserID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// TestGetSessionVSCodeURI_HappyPath asserts that a member with a key on
// file receives a 200 + a syntactically valid vscode:// URI. The URI must
// embed the session UUID, the request Host (since publicHostname is empty
// here), default port 2222, and the workspace path /workspaces/<name>.
func TestGetSessionVSCodeURI_HappyPath(t *testing.T) {
	f := newVSCodeURIFixture(t, "" /* publicHostname falls back to r.Host */, "")
	f.seedKey(t, 'A')

	rec := f.do(t, http.MethodGet, "/api/sessions/"+f.sessionID.String()+"/vscode-uri", f.userID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}

	// VS Code's URI scheme is vscode://vscode-remote/ssh-remote+<user>@<host>:<port>/<path>.
	// From Go's url.Parse perspective the user/host/port live INSIDE the path
	// component because "vscode-remote" is the parsed Host. Assert against
	// the literal URI rather than re-parsing its inner authority.
	if !strings.HasPrefix(body.URI, "vscode://vscode-remote/ssh-remote+") {
		t.Errorf("URI prefix: want vscode://vscode-remote/ssh-remote+, got %q", body.URI)
	}
	wantUser := "dc-" + f.sessionID.String()
	if !strings.Contains(body.URI, wantUser+"@") {
		t.Errorf("user: want %q in URI, got %q", wantUser, body.URI)
	}
	if !strings.Contains(body.URI, ":"+strconv.Itoa(defaultSSHPort)+"/") {
		t.Errorf("port: want :%d in URI, got %q", defaultSSHPort, body.URI)
	}
	wantPath := "/workspaces/" + f.session.Name
	if !strings.HasSuffix(body.URI, wantPath) {
		t.Errorf("path: want suffix %q, got %q", wantPath, body.URI)
	}

	// Smoke-check that the URI string is at least parseable (no embedded
	// nulls, no spaces, etc.).
	if _, err := url.Parse(body.URI); err != nil {
		t.Errorf("url.Parse(%q): %v", body.URI, err)
	}
}

// TestGetSessionVSCodeURI_NoKeyReturns412 verifies the gating behavior the
// frontend depends on to open the SSH key setup modal.
func TestGetSessionVSCodeURI_NoKeyReturns412(t *testing.T) {
	f := newVSCodeURIFixture(t, "", "")

	rec := f.do(t, http.MethodGet, "/api/sessions/"+f.sessionID.String()+"/vscode-uri", f.userID.String())
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status: want 412, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "NO_SSH_KEY" {
		t.Errorf("error code: want NO_SSH_KEY, got %q", body.Error.Code)
	}
}

// TestGetSessionVSCodeURI_InvalidSessionUUID covers the path-param parse
// branch — must return 400 INVALID_SESSION_ID before any DB lookup.
func TestGetSessionVSCodeURI_InvalidSessionUUID(t *testing.T) {
	f := newVSCodeURIFixture(t, "", "")

	rec := f.do(t, http.MethodGet, "/api/sessions/not-a-uuid/vscode-uri", f.userID.String())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "INVALID_SESSION_ID" {
		t.Errorf("error code: want INVALID_SESSION_ID, got %q", body.Error.Code)
	}
}

// TestGetSessionVSCodeURI_NonexistentSession ensures we mirror GetSession's
// 404 SESSION_NOT_FOUND mapping for a valid UUID with no row.
func TestGetSessionVSCodeURI_NonexistentSession(t *testing.T) {
	f := newVSCodeURIFixture(t, "", "")
	f.seedKey(t, 'B')

	missing := uuid.New().String()
	rec := f.do(t, http.MethodGet, "/api/sessions/"+missing+"/vscode-uri", f.userID.String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "SESSION_NOT_FOUND" {
		t.Errorf("error code: want SESSION_NOT_FOUND, got %q", body.Error.Code)
	}
}

// TestGetSessionVSCodeURI_PublicHostnameOverride asserts that when
// publicHostname is configured, the URI uses it verbatim instead of the
// request Host header. Mirrors the planned cfg.PublicHostname behavior.
func TestGetSessionVSCodeURI_PublicHostnameOverride(t *testing.T) {
	f := newVSCodeURIFixture(t, "deuce.example.com", ":2222")
	f.seedKey(t, 'C')

	rec := f.do(t, http.MethodGet, "/api/sessions/"+f.sessionID.String()+"/vscode-uri", f.userID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// The host:port lives in the URI's path component (see HappyPath test
	// for the parsing-quirk explanation).
	if !strings.Contains(body.URI, "@deuce.example.com:2222/") {
		t.Errorf("URI should embed @deuce.example.com:2222/, got %q", body.URI)
	}
	if _, err := url.Parse(body.URI); err != nil {
		t.Errorf("url.Parse(%q): %v", body.URI, err)
	}
}

// TestGetSessionVSCodeURI_PublicHostnameFallback verifies that when
// publicHostname is empty, the URI uses the request Host header.
func TestGetSessionVSCodeURI_PublicHostnameFallback(t *testing.T) {
	f := newVSCodeURIFixture(t, "", "")
	f.seedKey(t, 'D')

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+f.sessionID.String()+"/vscode-uri", nil)
	req.Host = "from-host-header.test"
	req.Header.Set("X-User-ID", f.userID.String())
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(body.URI, "@from-host-header.test:") {
		t.Errorf("URI should embed @from-host-header.test:, got %q", body.URI)
	}
	if _, err := url.Parse(body.URI); err != nil {
		t.Errorf("url.Parse: %v", err)
	}
}

// TestGetSessionVSCodeURI_StripsPortFromHostHeader covers the devcontainer
// case: r.Host comes in as "localhost:8080" because the browser hit the
// forwarded backend port. Without stripping, the URI would splice in
// "localhost:8080:2222" (double-colon) and VS Code rejects it.
func TestGetSessionVSCodeURI_StripsPortFromHostHeader(t *testing.T) {
	f := newVSCodeURIFixture(t, "", "")
	f.seedKey(t, 'E')

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+f.sessionID.String()+"/vscode-uri", nil)
	req.Host = "localhost:8080"
	req.Header.Set("X-User-ID", f.userID.String())
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Must NOT contain the double-colon authority.
	if strings.Contains(body.URI, "localhost:8080:") {
		t.Errorf("URI must not contain double-port; got %q", body.URI)
	}
	if !strings.Contains(body.URI, "@localhost:2222/") {
		t.Errorf("URI should embed @localhost:2222/, got %q", body.URI)
	}
}

// TestSSHPortFromAddr covers the small parser that strips the host portion
// from a Go listen-address string. The fallback path matters because U11
// has not yet shipped real config — every code path that lands here today
// passes an empty string.
func TestSSHPortFromAddr(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", defaultSSHPort},
		{":2222", 2222},
		{":4444", 4444},
		{"0.0.0.0:2222", 2222},
		{"127.0.0.1:5050", 5050},
		{"[::1]:6060", 6060},
		{"garbage", defaultSSHPort},
		{":not-a-port", defaultSSHPort},
		{":0", defaultSSHPort},
		{":99999", defaultSSHPort},
	}
	for _, tc := range cases {
		if got := sshPortFromAddr(tc.in); got != tc.want {
			t.Errorf("sshPortFromAddr(%q): want %d, got %d", tc.in, tc.want, got)
		}
	}
}
