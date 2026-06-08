// Package auth proxy_test.go covers ProxyMiddleware end-to-end at the chi
// handler boundary. It uses httptest plus a hand-rolled fake ProxyUserStore
// — no mocking library, no pgx, no live DB. The fake records calls and
// returns canned results indexed by an atomic call counter so concurrent
// race-loser scenarios can be modeled with simple slices.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/forgeutah/deuce/server/internal/config"
	db "github.com/forgeutah/deuce/server/internal/db"
)

// ---------- helpers ----------

type lookupCall struct {
	email string
}

type createCall struct {
	params db.CreateUserByEmailParams
}

type fakeStore struct {
	mu sync.Mutex

	lookupResults []db.User
	lookupErrs    []error
	lookupCalls   atomic.Int32
	lookupHistory []lookupCall

	createResults []db.User
	createErrs    []error
	createCalls   atomic.Int32
	createHistory []createCall

	// Default-team auto-join (U6). defaultTeam/defaultTeamErr drive
	// GetDefaultTeam; addTeamCalls records every AddTeamMember invocation.
	defaultTeam    db.Team
	defaultTeamErr error
	addTeamErr     error
	addTeamCalls   []db.AddTeamMemberParams
}

func (f *fakeStore) LookupUserByEmail(_ context.Context, email string) (db.User, error) {
	idx := int(f.lookupCalls.Add(1) - 1)
	f.mu.Lock()
	f.lookupHistory = append(f.lookupHistory, lookupCall{email: email})
	f.mu.Unlock()
	if idx >= len(f.lookupResults) && idx >= len(f.lookupErrs) {
		return db.User{}, pgx.ErrNoRows
	}
	var u db.User
	if idx < len(f.lookupResults) {
		u = f.lookupResults[idx]
	}
	var err error
	if idx < len(f.lookupErrs) {
		err = f.lookupErrs[idx]
	}
	return u, err
}

func (f *fakeStore) CreateUserByEmail(_ context.Context, arg db.CreateUserByEmailParams) (db.User, error) {
	idx := int(f.createCalls.Add(1) - 1)
	f.mu.Lock()
	f.createHistory = append(f.createHistory, createCall{params: arg})
	f.mu.Unlock()
	if idx >= len(f.createResults) && idx >= len(f.createErrs) {
		return db.User{}, pgx.ErrNoRows
	}
	var u db.User
	if idx < len(f.createResults) {
		u = f.createResults[idx]
	}
	var err error
	if idx < len(f.createErrs) {
		err = f.createErrs[idx]
	}
	return u, err
}

func (f *fakeStore) GetDefaultTeam(_ context.Context) (db.Team, error) {
	if f.defaultTeamErr != nil {
		return db.Team{}, f.defaultTeamErr
	}
	return f.defaultTeam, nil
}

func (f *fakeStore) AddTeamMember(_ context.Context, arg db.AddTeamMemberParams) error {
	f.mu.Lock()
	f.addTeamCalls = append(f.addTeamCalls, arg)
	f.mu.Unlock()
	return f.addTeamErr
}

func makeUser(t *testing.T, email, name string) db.User {
	t.Helper()
	return db.User{
		ID:    uuid.New(),
		Email: email,
		Name:  name,
	}
}

type recordingHandler struct {
	called      bool
	observedUID string
}

func (h *recordingHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	h.called = true
	h.observedUID = GetUserID(r.Context())
}

func captureLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	return buf, func() { slog.SetDefault(orig) }
}

// forgeBaseline returns the baseline ProxyConfig + headers a forge-proxy
// deployment would send. Tests mutate the returned values for negative cases.
func forgeBaseline() (ProxyConfig, http.Header) {
	pc := ProxyConfig{
		EmailHeader:           "X-Forge-Email",
		NameHeader:            "X-Forge-Name",
		AvatarHeader:          "X-Forge-Avatar",
		SecretHeader:          "X-Forge-Proxy-Secret",
		Secret:                "topsecret",
		ContractVersionHeader: "X-Forge-Contract-Version",
		ContractVersion:       1,
		RolesHeader:           "X-Forge-Roles",
		RolesFormat:           config.RolesFormatCSV,
		RequiredRole:          "member",
	}
	h := http.Header{}
	h.Set(pc.SecretHeader, pc.Secret)
	h.Set(pc.ContractVersionHeader, "1")
	h.Set(pc.EmailHeader, "alice@example.com")
	h.Set(pc.NameHeader, "Alice Example")
	h.Set(pc.AvatarHeader, "https://example.com/a.png")
	h.Set(pc.RolesHeader, "member,admin")
	return pc, h
}

// tailscaleBaseline mirrors a Tailscale Serve deployment: identity headers
// plus app capabilities as JSON object; no secret, no contract version.
func tailscaleBaseline() (ProxyConfig, http.Header) {
	pc := ProxyConfig{
		EmailHeader:  "Tailscale-User-Login",
		NameHeader:   "Tailscale-User-Name",
		AvatarHeader: "Tailscale-User-Profile-Pic",
		RolesHeader:  "Tailscale-App-Capabilities",
		RolesFormat:  config.RolesFormatJSONObject,
		RequiredRole: "example.com/cap/deuce/access",
	}
	h := http.Header{}
	h.Set(pc.EmailHeader, "alice@example.com")
	h.Set(pc.NameHeader, "Alice Example")
	h.Set(pc.AvatarHeader, "https://example.com/a.png")
	h.Set(pc.RolesHeader, `{"example.com/cap/deuce/access":[{}]}`)
	return pc, h
}

// invoke runs the middleware once and returns the response recorder plus the
// downstream handler so callers can assert on both.
func invoke(_ *testing.T, store ProxyUserStore, pc ProxyConfig, hdrs http.Header) (*httptest.ResponseRecorder, *recordingHandler) {
	mw := ProxyMiddleware(store, pc)
	rec := &recordingHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for k, vs := range hdrs {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	mw(rec).ServeHTTP(w, req)
	return w, rec
}

func assertNotAuthed(t *testing.T, w *httptest.ResponseRecorder, rec *recordingHandler, wantStatus int, wantCode string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status: got %d, want %d (body=%q)", w.Code, wantStatus, w.Body.String())
	}
	if rec.called {
		t.Fatal("downstream handler should not have been invoked")
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body should be JSON: %v (raw=%q)", err, w.Body.String())
	}
	if body.Error.Code != wantCode {
		t.Fatalf("error.code: got %q, want %q", body.Error.Code, wantCode)
	}
}

// ---------- happy paths (both providers) ----------

func TestForgeBaseline_AdmitsAndProvisions(t *testing.T) {
	pc, hdrs := forgeBaseline()
	want := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
	if !rec.called {
		t.Fatal("downstream handler should have been invoked")
	}
	if rec.observedUID != want.ID.String() {
		t.Fatalf("ctx user id: got %q, want %q", rec.observedUID, want.ID.String())
	}
	if got := store.createCalls.Load(); got != 1 {
		t.Fatalf("create call count: got %d, want 1", got)
	}
	if c := store.createHistory[0].params; c.Email != "alice@example.com" || c.Name != "Alice Example" || c.Avatar != "https://example.com/a.png" {
		t.Fatalf("create params: got %+v", c)
	}
}

func TestTailscaleBaseline_AdmitsAndProvisions(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	want := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
	if !rec.called || rec.observedUID != want.ID.String() {
		t.Fatalf("downstream not invoked correctly: called=%v uid=%q", rec.called, rec.observedUID)
	}
}

func TestSecondRequest_HitsLookupNotCreate(t *testing.T) {
	pc, hdrs := forgeBaseline()
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupResults: []db.User{existing},
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("unexpected: status=%d called=%v body=%q", w.Code, rec.called, w.Body.String())
	}
	if got := store.createCalls.Load(); got != 0 {
		t.Fatalf("create should not have fired: got %d calls", got)
	}
}

// ---------- identity header presence + shape ----------

func TestEmailHeader_Missing(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Del(pc.EmailHeader)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

func TestEmailHeader_Duplicate(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Add(pc.EmailHeader, "intruder@example.com")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

func TestEmailHeader_EmptyAfterTrim(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.EmailHeader, "   ")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

func TestNameHeader_Missing(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Del(pc.NameHeader)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

func TestNameHeader_EmptyAfterTrim(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.NameHeader, "   ")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

func TestNameHeader_UnconfiguredOptional_AdmitsWithEmptyName(t *testing.T) {
	// exe.dev-style: no name header configured. Middleware skips the name
	// read entirely and provisions the user with an empty name. The
	// welcome screen on the frontend collects the display name from there.
	pc := ProxyConfig{
		EmailHeader: "X-ExeDev-Email",
	}
	h := http.Header{}
	h.Set("X-ExeDev-Email", "alice@example.com")
	// Caller sends some unrelated header — it must be ignored, not consumed.
	h.Set("X-Other-Name", "Should Be Ignored")

	want := makeUser(t, "alice@example.com", "")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	w, rec := invoke(t, store, pc, h)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("unconfigured name header should admit: status=%d body=%q", w.Code, w.Body.String())
	}
	if store.createHistory[0].params.Name != "" {
		t.Fatalf("create should receive empty name when name header unconfigured, got %q", store.createHistory[0].params.Name)
	}
}

// ---------- default-team auto-join (U6) ----------

func TestProvision_AutoJoinsDefaultTeam(t *testing.T) {
	pc, hdrs := forgeBaseline()
	want := makeUser(t, "newbie@forge.dev", "Newbie")
	team := db.Team{ID: uuid.New(), Name: "Default", IsDefault: true}
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
		defaultTeam:   team,
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("provision should admit: status=%d body=%q", w.Code, w.Body.String())
	}
	if len(store.addTeamCalls) != 1 {
		t.Fatalf("want exactly 1 AddTeamMember call, got %d", len(store.addTeamCalls))
	}
	if store.addTeamCalls[0].TeamID != team.ID || store.addTeamCalls[0].UserID != want.ID {
		t.Fatalf("AddTeamMember called with wrong args: %+v", store.addTeamCalls[0])
	}
}

func TestProvision_ReturningUserDoesNotRejoinTeam(t *testing.T) {
	pc, hdrs := forgeBaseline()
	existing := makeUser(t, "alice@forge.dev", "Alice")
	store := &fakeStore{
		lookupResults: []db.User{existing}, // found on first lookup -> not created
		defaultTeam:   db.Team{ID: uuid.New(), IsDefault: true},
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("returning user should admit: status=%d", w.Code)
	}
	if len(store.addTeamCalls) != 0 {
		t.Fatalf("returning user must not trigger team auto-join, got %d calls", len(store.addTeamCalls))
	}
}

func TestProvision_MissingDefaultTeamStillAdmits(t *testing.T) {
	pc, hdrs := forgeBaseline()
	want := makeUser(t, "newbie@forge.dev", "Newbie")
	store := &fakeStore{
		lookupErrs:     []error{pgx.ErrNoRows},
		createResults:  []db.User{want},
		defaultTeamErr: pgx.ErrNoRows, // no default team configured
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("missing default team must not block admission: status=%d body=%q", w.Code, w.Body.String())
	}
	if len(store.addTeamCalls) != 0 {
		t.Fatalf("no team add should occur when default team lookup fails")
	}
}

// ---------- email normalization ----------

func TestEmail_NormalizesCaseAndWhitespace(t *testing.T) {
	pc, hdrs := forgeBaseline()
	// Mixed case + leading/trailing whitespace must collapse to the
	// canonical "alice@example.com" before lookup so a misconfigured
	// upstream can't provision two accounts for the same human.
	hdrs.Set(pc.EmailHeader, "  Alice@Example.COM  ")

	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("normalized lookup should admit: status=%d body=%q", w.Code, w.Body.String())
	}
	if store.lookupHistory[0].email != "alice@example.com" {
		t.Fatalf("lookup email: got %q, want lowercase/trimmed canonical", store.lookupHistory[0].email)
	}
}

// ---------- optional secret check ----------

func TestSecret_MissingHeader(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Del(pc.SecretHeader)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusUnauthorized, codeNotAuthenticated)
}

func TestSecret_WrongSameLength(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.SecretHeader, strings.Repeat("x", len(pc.Secret)))
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusUnauthorized, codeNotAuthenticated)
}

func TestSecret_WrongDifferentLength(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.SecretHeader, "short")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	// Asserts subtle.ConstantTimeCompare length-mismatch is handled before
	// the call (it would panic otherwise).
	assertNotAuthed(t, w, rec, http.StatusUnauthorized, codeNotAuthenticated)
}

func TestSecret_DuplicateHeader(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Add(pc.SecretHeader, "another-secret")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusUnauthorized, codeNotAuthenticated)
}

func TestSecret_Unconfigured_IgnoresHeader(t *testing.T) {
	// Tailscale-style: no secret check. A caller can send any value for
	// X-Forge-Proxy-Secret and it must not change admission.
	pc, hdrs := tailscaleBaseline()
	hdrs.Set("X-Forge-Proxy-Secret", "anything-here")
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("secret unconfigured should ignore the header: status=%d body=%q", w.Code, w.Body.String())
	}
}

// ---------- optional contract version check ----------

func TestContract_MissingHeader(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Del(pc.ContractVersionHeader)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidContractVersion)
}

func TestContract_NonInt(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.ContractVersionHeader, "not-a-number")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidContractVersion)
}

func TestContract_WrongVersion(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.ContractVersionHeader, "2")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidContractVersion)
}

func TestContract_Unconfigured_IgnoresHeader(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	hdrs.Set("X-Forge-Contract-Version", "9999")
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("contract unconfigured should ignore the header: status=%d", w.Code)
	}
}

// ---------- optional roles: CSV ----------

func TestRolesCSV_MissingHeader(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Del(pc.RolesHeader)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusForbidden, codeNotAuthorized)
}

func TestRolesCSV_WhitespaceTolerated(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.RolesHeader, "  member , admin  ")
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("whitespace-padded CSV should admit: status=%d", w.Code)
	}
}

func TestRolesCSV_SubstringRejected(t *testing.T) {
	// "membership" must not satisfy required role "member" — equality only.
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.RolesHeader, "membership")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusForbidden, codeNotAuthorized)
}

func TestRolesCSV_Duplicate(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Add(pc.RolesHeader, "guest")
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusBadRequest, codeInvalidHeaders)
}

// ---------- optional roles: JSON-object ----------

func TestRolesJSON_KeyPresent(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	// Already set to the cap-present case in the baseline.
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}
	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("json-object with cap present should admit: status=%d", w.Code)
	}
}

func TestRolesJSON_KeyMissing(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	hdrs.Set(pc.RolesHeader, `{"example.com/cap/other":[{}]}`)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusForbidden, codeNotAuthorized)
}

func TestRolesJSON_MalformedJSON(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	hdrs.Set(pc.RolesHeader, `{not-even-json`)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusForbidden, codeNotAuthorized)
}

func TestRolesJSON_NotAnObject(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	hdrs.Set(pc.RolesHeader, `["cap-as-array"]`)
	w, rec := invoke(t, &fakeStore{}, pc, hdrs)
	// JSON array doesn't unmarshal into map[string]json.RawMessage → parse_error.
	assertNotAuthed(t, w, rec, http.StatusForbidden, codeNotAuthorized)
}

func TestRolesJSON_LogsParseReason(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()

	pc, hdrs := tailscaleBaseline()
	hdrs.Set(pc.RolesHeader, `{malformed`)
	invoke(t, &fakeStore{}, pc, hdrs)

	if !strings.Contains(buf.String(), `"reason":"parse_error"`) {
		t.Fatalf("expected parse_error log reason, got: %s", buf.String())
	}
}

func TestRoles_Unconfigured_IgnoresHeader(t *testing.T) {
	pc, hdrs := tailscaleBaseline()
	pc.RolesHeader = ""
	pc.RolesFormat = ""
	pc.RequiredRole = ""
	// Even a role header from a different provider is now ignored.
	hdrs.Set("X-Forge-Roles", "anything-here")
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("roles unconfigured should ignore the header: status=%d", w.Code)
	}
}

// ---------- avatar header ----------

func TestAvatar_RejectedScheme_PassesEmptyToInsert(t *testing.T) {
	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.AvatarHeader, "javascript:alert(1)")
	want := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	w, _ := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if store.createHistory[0].params.Avatar != "" {
		t.Fatalf("avatar should be coerced to empty for rejected scheme, got %q", store.createHistory[0].params.Avatar)
	}
}

func TestAvatar_ValidURL_StoredVerbatim(t *testing.T) {
	pc, hdrs := forgeBaseline()
	want := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	invoke(t, store, pc, hdrs)
	if store.createHistory[0].params.Avatar != "https://example.com/a.png" {
		t.Fatalf("avatar should be stored verbatim, got %q", store.createHistory[0].params.Avatar)
	}
}

// ---------- user resolution ----------

func TestUserResolution_LookupHit(t *testing.T) {
	pc, hdrs := forgeBaseline()
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	invoke(t, store, pc, hdrs)
	if store.createCalls.Load() != 0 {
		t.Fatalf("lookup hit should skip create, got %d create calls", store.createCalls.Load())
	}
}

func TestUserResolution_RaceLoserReLookups(t *testing.T) {
	pc, hdrs := forgeBaseline()
	winner := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		// First lookup: miss. Create: race-loser (ErrNoRows). Re-lookup: hit.
		lookupErrs:    []error{pgx.ErrNoRows, nil},
		lookupResults: []db.User{{}, winner},
		createErrs:    []error{pgx.ErrNoRows},
	}

	w, rec := invoke(t, store, pc, hdrs)
	if w.Code != http.StatusOK || !rec.called {
		t.Fatalf("race loser should admit via re-lookup: status=%d body=%q", w.Code, w.Body.String())
	}
	if rec.observedUID != winner.ID.String() {
		t.Fatalf("ctx user id should be the winner's row: got %q want %q", rec.observedUID, winner.ID.String())
	}
	if store.lookupCalls.Load() != 2 {
		t.Fatalf("re-lookup should fire exactly once: total lookup calls = %d", store.lookupCalls.Load())
	}
}

type dbError struct{}

func (dbError) Error() string { return "boom" }

func TestUserResolution_LookupError_500(t *testing.T) {
	pc, hdrs := forgeBaseline()
	store := &fakeStore{lookupErrs: []error{dbError{}}}

	w, rec := invoke(t, store, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusInternalServerError, codeDBError)
	if strings.Contains(w.Body.String(), "boom") {
		t.Fatal("response body must not echo internal error message")
	}
}

func TestUserResolution_CreateError_500(t *testing.T) {
	pc, hdrs := forgeBaseline()
	store := &fakeStore{
		lookupErrs: []error{pgx.ErrNoRows},
		createErrs: []error{dbError{}},
	}

	w, rec := invoke(t, store, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusInternalServerError, codeDBError)
}

func TestUserResolution_PostRaceLookupError_500(t *testing.T) {
	pc, hdrs := forgeBaseline()
	store := &fakeStore{
		lookupErrs: []error{pgx.ErrNoRows, dbError{}},
		createErrs: []error{pgx.ErrNoRows}, // race-loser
	}

	w, rec := invoke(t, store, pc, hdrs)
	assertNotAuthed(t, w, rec, http.StatusInternalServerError, codeDBError)
}

// ---------- audit logging ----------

func TestProvisioningLog_FiresOnceOnInsert(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()

	pc, hdrs := forgeBaseline()
	want := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{
		lookupErrs:    []error{pgx.ErrNoRows},
		createResults: []db.User{want},
	}

	invoke(t, store, pc, hdrs)

	logs := buf.String()
	if !strings.Contains(logs, "auth.proxy: provisioned user") {
		t.Fatalf("provisioning log line missing: %s", logs)
	}
	if !strings.Contains(logs, `"email":"alice@example.com"`) {
		t.Fatalf("provisioning log should include email: %s", logs)
	}
	if !strings.Contains(logs, want.ID.String()) {
		t.Fatalf("provisioning log should include deuce_user_id: %s", logs)
	}
}

func TestRoleRejectionLog_SanitizesEmail(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()

	pc, hdrs := forgeBaseline()
	hdrs.Set(pc.EmailHeader, "alice\r\nfoo=bar@example.com")
	hdrs.Set(pc.RolesHeader, "guest") // role mismatch

	invoke(t, &fakeStore{}, pc, hdrs)

	logs := buf.String()
	if strings.Contains(logs, "\r") || strings.Contains(logs, "\n\t") {
		t.Fatalf("log line should be sanitized free of CR/LF in field values: %q", logs)
	}
	if !strings.Contains(logs, `"reason":"role_missing"`) {
		t.Fatalf("rejection log should name the reason: %s", logs)
	}
	if !strings.Contains(logs, `"required_role":"member"`) {
		t.Fatalf("rejection log should include required_role: %s", logs)
	}
}

// ---------- integration with chi + uuid.Parse ----------

func TestIntegration_ChiMount_CtxUUIDParses(t *testing.T) {
	pc, hdrs := forgeBaseline()
	existing := makeUser(t, "alice@example.com", "Alice Example")
	store := &fakeStore{lookupResults: []db.User{existing}}

	r := chi.NewRouter()
	r.Use(ProxyMiddleware(store, pc))
	parseFailed := false
	r.Get("/api/me", func(_ http.ResponseWriter, req *http.Request) {
		if _, err := uuid.Parse(GetUserID(req.Context())); err != nil {
			parseFailed = true
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for k, vs := range hdrs {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
	if parseFailed {
		t.Fatal("downstream uuid.Parse(auth.GetUserID(ctx)) should succeed")
	}
}

// ---------- log-content helper test ----------

func TestSanitizeForLog_StripsControlAndClamps(t *testing.T) {
	in := strings.Repeat("a", 300) + "\rok\nfoo\x00bar"
	got := sanitizeForLog(in)
	if strings.ContainsAny(got, "\r\n\x00") {
		t.Fatalf("sanitizeForLog should strip CR/LF/NUL, got %q", got)
	}
	if len(got) > maxHeaderLogLen {
		t.Fatalf("sanitizeForLog should clamp to %d, got len=%d", maxHeaderLogLen, len(got))
	}
}

// ---------- coverage stub for httputil.NewRecorder body access ----------

var _ io.Reader = (*bytes.Buffer)(nil) // referenced in test imports
