package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// visFixture builds an isolated schema and exposes helpers to seed teams,
// users, projects, sessions, and memberships so the team-scoped visibility
// and join-to-participate behavior (U1-U4) can be exercised end-to-end.
type visFixture struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	handler *Handler
	router  chi.Router
}

func newVisFixture(t *testing.T) *visFixture {
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

	// A hub is needed so the send/join handlers' broadcasts don't nil-panic.
	hub := ws.NewHub()
	go hub.Run()

	h := New(q, pool, hub, "", nil, nil, nil, nil, nil, "", "")

	r := chi.NewRouter()
	r.Use(auth.Middleware(""))
	r.Route("/api/sessions", func(r chi.Router) {
		r.Get("/", h.ListSessions)
		r.Route("/{sessionID}", func(r chi.Router) {
			r.Get("/", h.GetSession)
			r.Patch("/", h.UpdateSession)
			r.Post("/join", h.JoinSession)
			r.Post("/members", h.AddSessionMember)
			r.Get("/messages", h.ListMessages)
			r.Post("/messages", h.SendMessage)
			r.Get("/activities", h.ListActivities)
			r.Get("/files", h.ListFiles)
		})
	})

	return &visFixture{pool: pool, queries: q, handler: h, router: r}
}

func (f *visFixture) seedTeam(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO teams (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&id); err != nil {
		t.Fatalf("seed team %s: %v", slug, err)
	}
	return id
}

func (f *visFixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name) VALUES ($1, $1) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return id
}

func (f *visFixture) addTeamMember(t *testing.T, teamID, userID uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)`, teamID, userID); err != nil {
		t.Fatalf("add team member: %v", err)
	}
}

func (f *visFixture) seedProject(t *testing.T, teamID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO projects (name, team_id) VALUES ($1, $2) RETURNING id`, name, teamID).Scan(&id); err != nil {
		t.Fatalf("seed project %s: %v", name, err)
	}
	return id
}

func (f *visFixture) seedSession(t *testing.T, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO sessions (name, project_id) VALUES ($1, $2) RETURNING id`, name, projectID).Scan(&id); err != nil {
		t.Fatalf("seed session %s: %v", name, err)
	}
	return id
}

func (f *visFixture) addSessionMember(t *testing.T, sessionID, userID uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO session_members (session_id, user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatalf("add session member: %v", err)
	}
}

func (f *visFixture) do(t *testing.T, method, path, asUserID, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	if asUserID != "" {
		req.Header.Set("X-User-ID", asUserID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// --- U1: team-scoped session list ---

func TestListSessions_TeamScopedVisibility(t *testing.T) {
	f := newVisFixture(t)

	teamA := f.seedTeam(t, "team-a")
	teamB := f.seedTeam(t, "team-b")
	alice := f.seedUser(t, "alice@example.com")
	f.addTeamMember(t, teamA, alice) // alice is in team A only

	projA := f.seedProject(t, teamA, "proj-a")
	projB := f.seedProject(t, teamB, "proj-b")
	sessA := f.seedSession(t, projA, "session-a")
	_ = f.seedSession(t, projB, "session-b") // team B session, invisible to alice

	// Alice is a team member of A but NOT a session_member of sessA.
	rec := f.do(t, http.MethodGet, "/api/sessions/", alice.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 visible session (team A), got %d", len(got))
	}
	if got[0].ID != sessA {
		t.Fatalf("want session-a (%s), got %s", sessA, got[0].ID)
	}
}

func TestListSessions_MemberStillSeesSession(t *testing.T) {
	f := newVisFixture(t)

	teamA := f.seedTeam(t, "team-a")
	alice := f.seedUser(t, "alice@example.com")
	f.addTeamMember(t, teamA, alice)
	projA := f.seedProject(t, teamA, "proj-a")
	sessA := f.seedSession(t, projA, "session-a")
	f.addSessionMember(t, sessA, alice) // R9 regression: members keep visibility

	rec := f.do(t, http.MethodGet, "/api/sessions/", alice.String(), "")
	var got []sessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 1 || got[0].ID != sessA {
		t.Fatalf("member should still see their session; got %d sessions", len(got))
	}
}

func TestListSessions_NoTeamSeesNothing(t *testing.T) {
	f := newVisFixture(t)

	teamA := f.seedTeam(t, "team-a")
	projA := f.seedProject(t, teamA, "proj-a")
	_ = f.seedSession(t, projA, "session-a")
	loner := f.seedUser(t, "loner@example.com") // in no team

	rec := f.do(t, http.MethodGet, "/api/sessions/", loner.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var got []sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("user in no team should see no sessions, got %d", len(got))
	}
}

// --- U2: team-membership read gate ---

// readScenario seeds team A (alice in it, not a session member), team B
// (bob in it), and one session under team A. Returns the ids needed to
// exercise read endpoints from both an in-team and an out-of-team user.
func (f *visFixture) seedReadScenario(t *testing.T) (sessID, alice, bob uuid.UUID) {
	teamA := f.seedTeam(t, "team-a")
	teamB := f.seedTeam(t, "team-b")
	alice = f.seedUser(t, "alice@example.com")
	bob = f.seedUser(t, "bob@example.com")
	f.addTeamMember(t, teamA, alice)
	f.addTeamMember(t, teamB, bob)
	projA := f.seedProject(t, teamA, "proj-a")
	sessID = f.seedSession(t, projA, "session-a")
	return sessID, alice, bob
}

func TestGetSession_ReadGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, bob := f.seedReadScenario(t)

	// In-team, NOT a session member -> 200 (team membership grants read).
	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String(), alice.String(), ""); rec.Code != http.StatusOK {
		t.Fatalf("team member GetSession: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Out-of-team -> 403.
	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String(), bob.String(), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team GetSession: want 403, got %d", rec.Code)
	}
}

func TestListMessages_ReadGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, bob := f.seedReadScenario(t)

	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String()+"/messages", alice.String(), ""); rec.Code != http.StatusOK {
		t.Fatalf("team member ListMessages: want 200, got %d", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String()+"/messages", bob.String(), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team ListMessages: want 403, got %d", rec.Code)
	}
}

func TestListActivities_ReadGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, bob := f.seedReadScenario(t)

	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String()+"/activities", alice.String(), ""); rec.Code != http.StatusOK {
		t.Fatalf("team member ListActivities: want 200, got %d", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String()+"/activities", bob.String(), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team ListActivities: want 403, got %d", rec.Code)
	}
}

// --- U3: write gate on SendMessage ---

func (f *visFixture) countMessages(t *testing.T, sessID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM messages WHERE session_id = $1`, sessID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return n
}

func TestSendMessage_WriteGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, bob := f.seedReadScenario(t)

	body := `{"content":"hello","mentions":[]}`

	// Team member but NOT a session member -> 403, no row created.
	rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/messages", alice.String(), body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-session-member send: want 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if n := f.countMessages(t, sessID); n != 0 {
		t.Fatalf("non-member send must not persist; got %d messages", n)
	}

	// Out-of-team -> 403 (team gate would also reject; session gate fires first).
	if rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/messages", bob.String(), body); rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team send: want 403, got %d", rec.Code)
	}

	// Now make alice a session member -> 201, row created.
	f.addSessionMember(t, sessID, alice)
	if rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/messages", alice.String(), body); rec.Code != http.StatusCreated {
		t.Fatalf("member send: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if n := f.countMessages(t, sessID); n != 1 {
		t.Fatalf("member send should persist exactly 1 message; got %d", n)
	}
}

func TestSendMessage_GateBeforeValidation(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, _ := f.seedReadScenario(t)

	// Empty content from a NON-member must still 403 (membership checked
	// before EMPTY_CONTENT), so non-members can't probe validation rules.
	rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/messages", alice.String(), `{"content":"","mentions":[]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty content from non-member: want 403 (gate first), got %d", rec.Code)
	}
}

// --- U4: self-serve session join ---

func (f *visFixture) isSessionMember(t *testing.T, sessID, userID uuid.UUID) bool {
	t.Helper()
	var ok bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM session_members WHERE session_id=$1 AND user_id=$2)`,
		sessID, userID).Scan(&ok); err != nil {
		t.Fatalf("check session member: %v", err)
	}
	return ok
}

func TestJoinSession_TeamMemberCanJoin(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, _ := f.seedReadScenario(t)

	rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/join", alice.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("team member join: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !f.isSessionMember(t, sessID, alice) {
		t.Fatalf("alice should be a session member after join")
	}
	// Response should reflect alice in members.
	var sr sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, m := range sr.Members {
		if m.ID == alice {
			found = true
		}
	}
	if !found {
		t.Fatalf("join response should include caller in members")
	}

	// Now alice can post.
	if rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/messages", alice.String(), `{"content":"hi","mentions":[]}`); rec.Code != http.StatusCreated {
		t.Fatalf("post after join: want 201, got %d", rec.Code)
	}
}

func TestJoinSession_OutOfTeamForbidden(t *testing.T) {
	f := newVisFixture(t)
	sessID, _, bob := f.seedReadScenario(t)

	rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/join", bob.String(), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team join: want 403, got %d", rec.Code)
	}
	if f.isSessionMember(t, sessID, bob) {
		t.Fatalf("out-of-team user must not become a member")
	}
}

// --- Authz gates closed during code review (terminal/workspace/files/update/
// agents were previously un-gated; these lock in the representative gates) ---

func TestListFiles_ReadGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, _, bob := f.seedReadScenario(t)

	// Out-of-team user is rejected by the team read-gate before any workspace
	// logic runs (so workspace-not-ready never masks the 403).
	rec := f.do(t, http.MethodGet, "/api/sessions/"+sessID.String()+"/files", bob.String(), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-team ListFiles: want 403, got %d", rec.Code)
	}
}

func TestUpdateSession_WriteGate(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, _ := f.seedReadScenario(t)

	// alice is a team member but NOT a session member -> PATCH is write-gated.
	rec := f.do(t, http.MethodPatch, "/api/sessions/"+sessID.String(), alice.String(), `{"description":"hijack"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-session-member UpdateSession: want 403, got %d", rec.Code)
	}
}

func TestAddSessionMember_TargetMustBeTeamMember(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, bob := f.seedReadScenario(t)
	f.addSessionMember(t, sessID, alice) // alice can manage members

	// bob is NOT on the session's team -> adding him is rejected.
	rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/members", alice.String(), `{"userId":"`+bob.String()+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add non-team user: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Put bob on the team -> now he can be added.
	teamA, err := f.queries.IsSessionTeamMember(context.Background(), db.IsSessionTeamMemberParams{SessionID: sessID, UserID: alice})
	if err != nil || !teamA {
		t.Fatalf("precondition: alice should be a team member")
	}
	// Resolve team via project and add bob.
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO team_members (team_id, user_id) SELECT p.team_id, $2 FROM sessions s JOIN projects p ON p.id = s.project_id WHERE s.id = $1`,
		sessID, bob); err != nil {
		t.Fatalf("add bob to team: %v", err)
	}
	rec2 := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/members", alice.String(), `{"userId":"`+bob.String()+`"}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("add team user: want 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestJoinSession_Idempotent(t *testing.T) {
	f := newVisFixture(t)
	sessID, alice, _ := f.seedReadScenario(t)

	for i := 0; i < 2; i++ {
		if rec := f.do(t, http.MethodPost, "/api/sessions/"+sessID.String()+"/join", alice.String(), ""); rec.Code != http.StatusOK {
			t.Fatalf("join #%d: want 200, got %d", i+1, rec.Code)
		}
	}
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM session_members WHERE session_id=$1 AND user_id=$2`, sessID, alice).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("idempotent join should leave exactly 1 membership row, got %d", n)
	}
}
