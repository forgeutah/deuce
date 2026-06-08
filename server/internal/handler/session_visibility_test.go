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
			// /join is registered in U4 once JoinSession exists.
			r.Get("/messages", h.ListMessages)
			r.Post("/messages", h.SendMessage)
			r.Get("/activities", h.ListActivities)
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
