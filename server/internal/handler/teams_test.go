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
)

type teamsFixture struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	router  chi.Router
}

func newTeamsFixture(t *testing.T) *teamsFixture {
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
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if err := db.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	q := db.New(pool)
	h := New(q, pool, nil, "", nil, nil, nil, nil, nil, "", "")

	r := chi.NewRouter()
	r.Use(auth.Middleware(""))
	r.Route("/api/teams", func(r chi.Router) {
		r.Get("/", h.ListTeams)
		r.Get("/all", h.ListAllTeams)
		r.Post("/", h.CreateTeam)
		r.Route("/{teamID}", func(r chi.Router) {
			r.Post("/join", h.JoinTeam)
			r.Delete("/members/{userID}", h.LeaveTeam)
		})
	})

	return &teamsFixture{pool: pool, queries: q, router: r}
}

func (f *teamsFixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name) VALUES ($1, $1) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func (f *teamsFixture) defaultTeamID(t *testing.T) uuid.UUID {
	t.Helper()
	team, err := f.queries.GetDefaultTeam(context.Background())
	if err != nil {
		t.Fatalf("get default team: %v", err)
	}
	return team.ID
}

func (f *teamsFixture) do(t *testing.T, method, path, asUserID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if asUserID != "" {
		req.Header.Set("X-User-ID", asUserID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestListAllTeams_MembershipAndCounts(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")
	def := f.defaultTeamID(t)
	if err := f.queries.AddTeamMember(context.Background(), db.AddTeamMemberParams{TeamID: def, UserID: alice}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	rec := f.do(t, http.MethodGet, "/api/teams/all", alice.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var got []teamBrowseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawDefault bool
	for _, tm := range got {
		if tm.ID == def {
			sawDefault = true
			if !tm.IsMember {
				t.Fatalf("alice should be a member of the default team")
			}
			if !tm.IsDefault {
				t.Fatalf("default team should report isDefault=true")
			}
			if tm.MemberCount < 1 {
				t.Fatalf("default team member count should be >= 1, got %d", tm.MemberCount)
			}
		}
	}
	if !sawDefault {
		t.Fatalf("browse list should include the default team")
	}
}

func TestCreateTeam_AddsCreatorAndSlugifies(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")

	rec := f.do(t, http.MethodPost, "/api/teams/", alice.String(), `{"name":"My New Team!"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got teamBrowseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "my-new-team" {
		t.Fatalf("slug: want my-new-team, got %q", got.Slug)
	}
	if !got.IsMember || got.MemberCount != 1 {
		t.Fatalf("creator should be the sole member; isMember=%v count=%d", got.IsMember, got.MemberCount)
	}

	// Duplicate slug -> 409.
	rec2 := f.do(t, http.MethodPost, "/api/teams/", alice.String(), `{"name":"My New Team"}`)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("duplicate slug: want 409, got %d", rec2.Code)
	}
}

func TestCreateTeam_RejectsEmptyAndUnslugifiable(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")

	if rec := f.do(t, http.MethodPost, "/api/teams/", alice.String(), `{"name":"   "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: want 400, got %d", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/api/teams/", alice.String(), `{"name":"!!!"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unslugifiable name: want 400, got %d", rec.Code)
	}
}

func TestJoinTeam_ThenSessionsVisible(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")

	// Create a fresh team alice is NOT in, then join it.
	var teamID uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO teams (name, slug) VALUES ('Other', 'other') RETURNING id`).Scan(&teamID); err != nil {
		t.Fatalf("seed team: %v", err)
	}

	rec := f.do(t, http.MethodPost, "/api/teams/"+teamID.String()+"/join", alice.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("join: want 204, got %d", rec.Code)
	}
	member, err := f.queries.IsTeamMember(context.Background(), db.IsTeamMemberParams{TeamID: teamID, UserID: alice})
	if err != nil || !member {
		t.Fatalf("alice should be a member after join (member=%v err=%v)", member, err)
	}
}

func TestJoinTeam_NonexistentReturns404(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")
	ghost := uuid.New() // valid UUID, no such team

	rec := f.do(t, http.MethodPost, "/api/teams/"+ghost.String()+"/join", alice.String(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("join nonexistent team: want 404, got %d", rec.Code)
	}
}

func TestLeaveTeam_GuardsDefaultAndSelfOnly(t *testing.T) {
	f := newTeamsFixture(t)
	alice := f.seedUser(t, "alice@example.com")
	bob := f.seedUser(t, "bob@example.com")
	def := f.defaultTeamID(t)
	_ = f.queries.AddTeamMember(context.Background(), db.AddTeamMemberParams{TeamID: def, UserID: alice})

	// Leaving the default team -> 400.
	if rec := f.do(t, http.MethodDelete, "/api/teams/"+def.String()+"/members/"+alice.String(), alice.String(), ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("leave default: want 400, got %d", rec.Code)
	}

	// A non-default team: alice can leave herself.
	var other uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO teams (name, slug) VALUES ('Other', 'other') RETURNING id`).Scan(&other); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	_ = f.queries.AddTeamMember(context.Background(), db.AddTeamMemberParams{TeamID: other, UserID: alice})

	// Removing someone else -> 403.
	if rec := f.do(t, http.MethodDelete, "/api/teams/"+other.String()+"/members/"+bob.String(), alice.String(), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("remove other: want 403, got %d", rec.Code)
	}
	// Removing self -> 204.
	if rec := f.do(t, http.MethodDelete, "/api/teams/"+other.String()+"/members/"+alice.String(), alice.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("leave self: want 204, got %d", rec.Code)
	}
	member, _ := f.queries.IsTeamMember(context.Background(), db.IsTeamMemberParams{TeamID: other, UserID: alice})
	if member {
		t.Fatalf("alice should no longer be a member after leaving")
	}
}
