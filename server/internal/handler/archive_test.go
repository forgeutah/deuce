package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// archiveFixture spins up an isolated DB schema and seeds a team, a project,
// a session, a session member (alice) and a team-only member (bob, who can
// read the team's sessions but is NOT a session member, so the write gate
// must reject her archive attempts). The handler is built with workspaces=nil
// so archive runs status-only (no container teardown) — the teardown path
// reuses the already-exercised workspace delete action and is verified
// manually / by the existing workspace tests.
type archiveFixture struct {
	pool      *pgxpool.Pool
	queries   *db.Queries
	router    chi.Router
	memberID  uuid.UUID // session member
	teamOnly  uuid.UUID // team member, not session member
	sessionID uuid.UUID
}

func newArchiveFixture(t *testing.T) *archiveFixture {
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

	var teamID, memberID, teamOnly, projectID, sessionID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO teams (name, slug) VALUES ('Test Team', 'test-team') RETURNING id`).Scan(&teamID); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('alice@example.com', 'Alice') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('bob@example.com', 'Bob') RETURNING id`).Scan(&teamOnly); err != nil {
		t.Fatalf("seed team-only user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects (name, team_id) VALUES ('Test Project', $1) RETURNING id`, teamID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sessions (name, project_id) VALUES ('alice-workspace', $1) RETURNING id`, projectID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_members (session_id, user_id) VALUES ($1, $2)`, sessionID, memberID); err != nil {
		t.Fatalf("seed session_member: %v", err)
	}
	// Both users are on the team (team membership = read gate).
	if _, err := pool.Exec(ctx, `INSERT INTO team_members (team_id, user_id) VALUES ($1, $2), ($1, $3)`, teamID, memberID, teamOnly); err != nil {
		t.Fatalf("seed team_members: %v", err)
	}

	h := New(q, pool, ws.NewHub(), "", nil, nil, nil, "", "")

	r := chi.NewRouter()
	r.Use(auth.Middleware(""))
	r.Get("/api/sessions", h.ListSessions)
	r.Post("/api/sessions/{sessionID}/archive", h.ArchiveSession)
	r.Post("/api/sessions/{sessionID}/unarchive", h.UnarchiveSession)

	return &archiveFixture{
		pool:      pool,
		queries:   q,
		router:    r,
		memberID:  memberID,
		teamOnly:  teamOnly,
		sessionID: sessionID,
	}
}

func (f *archiveFixture) do(t *testing.T, method, path, asUserID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if asUserID != "" {
		req.Header.Set("X-User-ID", asUserID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// statusOf reads the current status column for the fixture's session.
func (f *archiveFixture) statusOf(t *testing.T) string {
	t.Helper()
	s, err := f.queries.GetSession(context.Background(), f.sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	return s.Status
}

// listIDs returns the session IDs the member sees from GET /api/sessions
// (optionally the archived view).
func (f *archiveFixture) listIDs(t *testing.T, archived bool) map[string]bool {
	t.Helper()
	path := "/api/sessions"
	if archived {
		path += "?archived=true"
	}
	rec := f.do(t, http.MethodGet, path, f.memberID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var sessions []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	ids := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		ids[s.ID] = true
	}
	return ids
}

// TestArchiveSession_FlipsStatusAndFiltersList covers R1/R3/R4: archiving a
// session flips status to archived, removes it from the default sidebar list,
// and surfaces it only through the archived view.
func TestArchiveSession_FlipsStatusAndFiltersList(t *testing.T) {
	f := newArchiveFixture(t)

	// Precondition: visible in the default list, status active.
	if !f.listIDs(t, false)[f.sessionID.String()] {
		t.Fatalf("precondition: session should be in default list")
	}
	if got := f.statusOf(t); got != "active" {
		t.Fatalf("precondition: status want active, got %q", got)
	}

	rec := f.do(t, http.MethodPost, "/api/sessions/"+f.sessionID.String()+"/archive", f.memberID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := f.statusOf(t); got != "archived" {
		t.Errorf("status after archive: want archived, got %q", got)
	}
	if f.listIDs(t, false)[f.sessionID.String()] {
		t.Errorf("archived session must not appear in default list")
	}
	if !f.listIDs(t, true)[f.sessionID.String()] {
		t.Errorf("archived session must appear in archived list")
	}
}

// TestArchiveSession_StatusOnlyWhenWorkspaceUnavailable covers the
// DevPod-unavailable path: with no workspace manager, archive still succeeds
// (status-only) and does not touch workspace_status.
func TestArchiveSession_StatusOnlyWhenWorkspaceUnavailable(t *testing.T) {
	f := newArchiveFixture(t)

	before, err := f.queries.GetSession(context.Background(), f.sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	rec := f.do(t, http.MethodPost, "/api/sessions/"+f.sessionID.String()+"/archive", f.memberID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	after, err := f.queries.GetSession(context.Background(), f.sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if after.WorkspaceStatus != before.WorkspaceStatus {
		t.Errorf("workspace_status should be untouched without devpod: was %q, now %q",
			before.WorkspaceStatus, after.WorkspaceStatus)
	}
}

// TestArchiveSession_NonMemberForbidden covers R6: a team member who is NOT a
// session member is rejected by the write gate, and the status is unchanged.
func TestArchiveSession_NonMemberForbidden(t *testing.T) {
	f := newArchiveFixture(t)

	rec := f.do(t, http.MethodPost, "/api/sessions/"+f.sessionID.String()+"/archive", f.teamOnly.String())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("archive by non-member: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := f.statusOf(t); got != "active" {
		t.Errorf("status must be unchanged after rejected archive: got %q", got)
	}
}

// TestUnarchiveSession_RestoresStatus covers R5: unarchiving flips status back
// to active and returns the session to the default list.
func TestUnarchiveSession_RestoresStatus(t *testing.T) {
	f := newArchiveFixture(t)

	if rec := f.do(t, http.MethodPost, "/api/sessions/"+f.sessionID.String()+"/archive", f.memberID.String()); rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec := f.do(t, http.MethodPost, "/api/sessions/"+f.sessionID.String()+"/unarchive", f.memberID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := f.statusOf(t); got != "active" {
		t.Errorf("status after unarchive: want active, got %q", got)
	}
	if !f.listIDs(t, false)[f.sessionID.String()] {
		t.Errorf("restored session must reappear in default list")
	}
	if f.listIDs(t, true)[f.sessionID.String()] {
		t.Errorf("restored session must not appear in archived list")
	}
}

// TestArchiveSession_InvalidUUID covers the path-param parse branch.
func TestArchiveSession_InvalidUUID(t *testing.T) {
	f := newArchiveFixture(t)

	rec := f.do(t, http.MethodPost, "/api/sessions/not-a-uuid/archive", f.memberID.String())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid uuid: want 400, got %d body=%s", rec.Code, rec.Body.String())
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
