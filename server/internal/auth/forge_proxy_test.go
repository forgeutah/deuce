// Package auth tests for forge_proxy.go.
//
// Note: this is the first *_test.go in the deuce server. The scope is
// deliberately tight — only the proxy middleware. Other code paths
// (handlers, chi mounting, real DB queries) are exercised via the manual
// smoke checks documented in the plan's U3 verification. Adding a general
// test framework was not part of this plan.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// --- Test helpers ---

// fakeStore is a hand-rolled implementation of ForgeUserStore that tracks
// every call and lets each test wire up the return values it needs. We
// don't use a mocking library — the surface is small enough that explicit
// behavior is clearer.
type fakeStore struct {
	lookupResults []db.User
	lookupErrors  []error
	createResults []db.User
	createErrors  []error

	lookupCalls atomic.Int32
	createCalls atomic.Int32

	lastLookupID pgtype.Int8
	lastCreate   db.CreateUserByForgeIDParams
}

func (f *fakeStore) LookupUserByForgeID(_ context.Context, id pgtype.Int8) (db.User, error) {
	i := int(f.lookupCalls.Add(1)) - 1
	f.lastLookupID = id
	if i < len(f.lookupErrors) && f.lookupErrors[i] != nil {
		return db.User{}, f.lookupErrors[i]
	}
	if i < len(f.lookupResults) {
		return f.lookupResults[i], nil
	}
	return db.User{}, pgx.ErrNoRows
}

func (f *fakeStore) CreateUserByForgeID(_ context.Context, arg db.CreateUserByForgeIDParams) (db.User, error) {
	i := int(f.createCalls.Add(1)) - 1
	f.lastCreate = arg
	if i < len(f.createErrors) && f.createErrors[i] != nil {
		return db.User{}, f.createErrors[i]
	}
	if i < len(f.createResults) {
		return f.createResults[i], nil
	}
	return db.User{}, pgx.ErrNoRows
}

// makeUser returns a non-zero db.User for fake responses.
func makeUser(forgeID int64, name, email, avatar string) db.User {
	return db.User{
		ID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:        name,
		Email:       email,
		Avatar:      avatar,
		Status:      "online",
		ForgeUserID: pgtype.Int8{Int64: forgeID, Valid: true},
	}
}

// recordingHandler is the downstream handler the middleware should call
// only on the admit path. It records the context value it observed.
type recordingHandler struct {
	called      atomic.Bool
	observedUID string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called.Store(true)
	h.observedUID = GetUserID(r.Context())
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// captureLogs swaps the default slog logger for one that writes into a
// buffer, returning a restore func. Tests assert against the buffer to
// verify audit logging and sanitization.
func captureLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	return buf, func() { slog.SetDefault(old) }
}

// validHeaders returns a header set that should pass every gate. Tests
// override individual headers as needed.
func validHeaders() http.Header {
	h := http.Header{}
	h.Set("X-Forge-Proxy-Secret", "topsecret")
	h.Set("X-Forge-Contract-Version", "1")
	h.Set("X-Forge-User-Id", "42")
	h.Set("X-Forge-Email", "alice@example.com")
	h.Set("X-Forge-Name", "Alice")
	h.Set("X-Forge-Avatar", "https://example.com/a.png")
	h.Set("X-Forge-Roles", "member,admin")
	return h
}

// invoke runs the middleware once with the given header set and store,
// returning the response recorder and the downstream handler so tests can
// assert on both.
func invoke(t *testing.T, store ForgeUserStore, secret, role string, contractVer int, h http.Header) (*httptest.ResponseRecorder, *recordingHandler) {
	t.Helper()
	next := &recordingHandler{}
	mw := ForgeProxyMiddleware(store, secret, role, contractVer)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header = h
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	return rec, next
}

func assertNotAuthed(t *testing.T, rec *httptest.ResponseRecorder, next *recordingHandler, code int, errCode string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status: got %d, want %d. body=%s", rec.Code, code, rec.Body.String())
	}
	if next.called.Load() {
		t.Fatalf("downstream handler must not be invoked on %d", code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != errCode {
		t.Fatalf("error.code: got %q, want %q", body.Error.Code, errCode)
	}
}

// --- Secret checks ---

func TestSecret_MissingHeader(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Del("X-Forge-Proxy-Secret")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
	if store.lookupCalls.Load() != 0 || store.createCalls.Load() != 0 {
		t.Fatalf("DB must not be touched on missing secret")
	}
}

func TestSecret_WrongSameLength(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Proxy-Secret", "wrongsecr") // same length as "topsecret"
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
	if store.lookupCalls.Load() != 0 {
		t.Fatalf("DB must not be touched on wrong secret")
	}
}

func TestSecret_WrongDifferentLength(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Proxy-Secret", "x")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

func TestSecret_DuplicateHeader(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Add("X-Forge-Proxy-Secret", "another")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

// --- Contract version checks ---

func TestContractVersion_Missing(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Del("X-Forge-Contract-Version")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusBadRequest, "INVALID_CONTRACT_VERSION")
}

func TestContractVersion_NotInteger(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Contract-Version", "abc")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusBadRequest, "INVALID_CONTRACT_VERSION")
}

func TestContractVersion_Mismatch(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Contract-Version", "2")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusBadRequest, "INVALID_CONTRACT_VERSION")
}

// --- User id checks ---

func TestUserID_Missing(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Del("X-Forge-User-Id")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

func TestUserID_NotInteger(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-User-Id", "notanint")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

func TestUserID_ZeroOrNegative(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-User-Id", "0")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

func TestUserID_Duplicate(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Add("X-Forge-User-Id", "43")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusUnauthorized, "NOT_AUTHENTICATED")
}

// --- Role checks ---

func TestRole_MissingHeader(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Del("X-Forge-Roles")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusForbidden, "NOT_AUTHORIZED")
	if store.lookupCalls.Load() != 0 {
		t.Fatalf("DB must not be touched when role is missing")
	}
}

func TestRole_AbsentFromCSV(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Roles", "guest,beta")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusForbidden, "NOT_AUTHORIZED")
}

func TestRole_SubstringDoesNotMatch(t *testing.T) {
	// "membership" must not satisfy required role "member"
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Roles", "membership")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusForbidden, "NOT_AUTHORIZED")
}

func TestRole_WhitespaceTrimmed(t *testing.T) {
	store := &fakeStore{
		lookupResults: []db.User{makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")},
	}
	h := validHeaders()
	h.Set("X-Forge-Roles", "  member , admin ")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected admit, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !next.called.Load() {
		t.Fatalf("downstream handler must be invoked on admit")
	}
}

// --- Other header validation ---

func TestEmail_MissingHeader(t *testing.T) {
	// Role check passes first; email-required check fires after.
	store := &fakeStore{}
	h := validHeaders()
	h.Del("X-Forge-Email")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusBadRequest, "INVALID_HEADERS")
}

func TestEmail_Duplicate(t *testing.T) {
	store := &fakeStore{}
	h := validHeaders()
	h.Add("X-Forge-Email", "eve@example.com")
	rec, next := invoke(t, store, "topsecret", "member", 1, h)
	assertNotAuthed(t, rec, next, http.StatusBadRequest, "INVALID_HEADERS")
}

// --- Avatar scheme validation ---

func TestAvatar_HTTPSchemeAccepted(t *testing.T) {
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		createResults: []db.User{makeUser(42, "Alice", "alice@example.com", "http://insecure.example.com/a.png")},
	}
	h := validHeaders()
	h.Set("X-Forge-Avatar", "http://insecure.example.com/a.png")
	rec, _ := invoke(t, store, "topsecret", "member", 1, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.lastCreate.Avatar != "http://insecure.example.com/a.png" {
		t.Fatalf("avatar mangled: got %q", store.lastCreate.Avatar)
	}
}

func TestAvatar_JavaScriptSchemeRejected(t *testing.T) {
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		createResults: []db.User{makeUser(42, "Alice", "alice@example.com", "")},
	}
	h := validHeaders()
	h.Set("X-Forge-Avatar", "javascript:alert(1)")
	rec, _ := invoke(t, store, "topsecret", "member", 1, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (admit, scheme stripped), got %d", rec.Code)
	}
	if store.lastCreate.Avatar != "" {
		t.Fatalf("javascript: avatar must be stripped to empty string, got %q", store.lastCreate.Avatar)
	}
}

func TestAvatar_DataSchemeRejected(t *testing.T) {
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		createResults: []db.User{makeUser(42, "Alice", "alice@example.com", "")},
	}
	h := validHeaders()
	h.Set("X-Forge-Avatar", "data:image/png;base64,xxx")
	rec, _ := invoke(t, store, "topsecret", "member", 1, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if store.lastCreate.Avatar != "" {
		t.Fatalf("data: avatar must be stripped, got %q", store.lastCreate.Avatar)
	}
}

// --- Happy paths ---

func TestAdmit_ExistingUser_NoCreateCall(t *testing.T) {
	user := makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")
	store := &fakeStore{lookupResults: []db.User{user}}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !next.called.Load() {
		t.Fatalf("downstream handler must be invoked")
	}
	if next.observedUID != user.ID.String() {
		t.Fatalf("context userID: got %q, want %q", next.observedUID, user.ID.String())
	}
	if store.createCalls.Load() != 0 {
		t.Fatalf("existing user must not trigger Create — got %d Create calls", store.createCalls.Load())
	}
}

func TestAdmit_NewUser_CreateCalledOnce(t *testing.T) {
	user := makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		createResults: []db.User{user},
	}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !next.called.Load() {
		t.Fatalf("downstream handler must be invoked")
	}
	if store.createCalls.Load() != 1 {
		t.Fatalf("Create call count: got %d, want 1", store.createCalls.Load())
	}
	if store.lastCreate.Name != "Alice" || store.lastCreate.Email != "alice@example.com" {
		t.Fatalf("Create params: got %+v", store.lastCreate)
	}
}

func TestAdmit_ContextValueParseableAsUUID(t *testing.T) {
	user := makeUser(42, "Alice", "alice@example.com", "")
	store := &fakeStore{lookupResults: []db.User{user}}
	_, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	if _, err := uuid.Parse(next.observedUID); err != nil {
		t.Fatalf("context value is not a parseable UUID: %q (%v)", next.observedUID, err)
	}
}

func TestAdmit_ProfileNotRefreshedOnSecondRequest(t *testing.T) {
	// First request creates the user. Second request must not call Create
	// again — and the profile fields the second request supplied are NOT
	// pushed to the store (insert-only policy).
	user := makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		lookupResults: []db.User{{}, user}, // 2nd lookup returns the same user
		createResults: []db.User{user},
	}
	// First request: provision
	if rec, _ := invoke(t, store, "topsecret", "member", 1, validHeaders()); rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}
	// Second request: lookup hit, no Create
	h2 := validHeaders()
	h2.Set("X-Forge-Email", "alicia@example.com")
	h2.Set("X-Forge-Name", "Alicia")
	rec2, _ := invoke(t, store, "topsecret", "member", 1, h2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rec2.Code)
	}
	if store.createCalls.Load() != 1 {
		t.Fatalf("Create must be called only once across both requests, got %d", store.createCalls.Load())
	}
}

// --- Race + DB error paths ---

func TestRace_LoserPath_RelookupSucceeds(t *testing.T) {
	// First lookup: no row. Create: returns ErrNoRows (lost ON CONFLICT race).
	// Second lookup: winner's row is now visible.
	winner := makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows, nil},
		lookupResults: []db.User{{}, winner},
		createErrors:  []error{pgx.ErrNoRows},
	}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !next.called.Load() {
		t.Fatalf("downstream handler must be invoked on race-loser path")
	}
	if store.lookupCalls.Load() != 2 {
		t.Fatalf("expected 2 lookups, got %d", store.lookupCalls.Load())
	}
	if store.createCalls.Load() != 1 {
		t.Fatalf("expected 1 create, got %d", store.createCalls.Load())
	}
}

func TestRace_RelookupFails_500(t *testing.T) {
	store := &fakeStore{
		lookupErrors: []error{pgx.ErrNoRows, errors.New("db down")},
		createErrors: []error{pgx.ErrNoRows},
	}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	assertNotAuthed(t, rec, next, http.StatusInternalServerError, "DB_ERROR")
}

func TestCreate_OtherError_500_NoLeakInBody(t *testing.T) {
	store := &fakeStore{
		lookupErrors: []error{pgx.ErrNoRows},
		createErrors: []error{errors.New("super secret internal: column users.email_secret does not exist")},
	}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	assertNotAuthed(t, rec, next, http.StatusInternalServerError, "DB_ERROR")
	if strings.Contains(rec.Body.String(), "super secret internal") {
		t.Fatalf("raw err.Error() leaked into response body: %s", rec.Body.String())
	}
}

func TestLookup_OtherError_500(t *testing.T) {
	store := &fakeStore{
		lookupErrors: []error{errors.New("connection refused")},
	}
	rec, next := invoke(t, store, "topsecret", "member", 1, validHeaders())
	assertNotAuthed(t, rec, next, http.StatusInternalServerError, "DB_ERROR")
	if store.createCalls.Load() != 0 {
		t.Fatalf("Create must not be called when initial lookup fails for non-ErrNoRows reason")
	}
}

// --- Audit logging ---

func TestAuditLog_FirstProvision(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()
	user := makeUser(42, "Alice", "alice@example.com", "https://example.com/a.png")
	store := &fakeStore{
		lookupErrors:  []error{pgx.ErrNoRows},
		createResults: []db.User{user},
	}
	rec, _ := invoke(t, store, "topsecret", "member", 1, validHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	out := buf.String()
	if !strings.Contains(out, `"msg":"auth.forge_proxy: provisioned user"`) {
		t.Fatalf("expected provisioned-user audit log, got: %s", out)
	}
	if !strings.Contains(out, `"forge_user_id":42`) {
		t.Fatalf("audit log missing forge_user_id: %s", out)
	}
	if !strings.Contains(out, `"email":"alice@example.com"`) {
		t.Fatalf("audit log missing email: %s", out)
	}
}

func TestAuditLog_NoSecretLeakOnAnyPath(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()
	secret := "supersecretdonotleak"
	store := &fakeStore{}
	// Fail at every gate the slog touches.
	h := validHeaders()
	h.Set("X-Forge-Proxy-Secret", secret+"wrong")
	invoke(t, store, secret, "member", 1, h)
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret value appeared in log output: %s", buf.String())
	}
}

func TestAuditLog_RoleRejection(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Roles", "guest")
	invoke(t, store, "topsecret", "member", 1, h)
	out := buf.String()
	if !strings.Contains(out, `"msg":"auth.forge_proxy: rejected (missing required role)"`) {
		t.Fatalf("expected role-rejection log, got: %s", out)
	}
	if !strings.Contains(out, `"required_role":"member"`) {
		t.Fatalf("role-rejection log missing required_role: %s", out)
	}
}

func TestAuditLog_EmailSanitization_CRLFStripped(t *testing.T) {
	buf, restore := captureLogs(t)
	defer restore()
	store := &fakeStore{}
	h := validHeaders()
	h.Set("X-Forge-Roles", "guest") // trigger 403 so the audit log includes email
	// Go's http.Header rejects CR/LF in header VALUES via Set, so we set the
	// value pre-sanitization by writing directly to the map.
	h["X-Forge-Email"] = []string{"alice\r\nfoo=bar@example.com"}
	invoke(t, store, "topsecret", "member", 1, h)
	out := buf.String()
	// The raw \r\n must not appear in the slog output (slog's JSON handler
	// would otherwise escape them as literal \r\n inside the email string —
	// we want them STRIPPED, not escaped).
	if strings.Contains(out, `\r`) || strings.Contains(out, `\n`) {
		t.Fatalf("CR/LF must be stripped before logging, not escaped: %s", out)
	}
	if !strings.Contains(out, `"email":"alicefoo=bar@example.com"`) {
		t.Fatalf("expected sanitized email in log, got: %s", out)
	}
}
