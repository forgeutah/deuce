package handler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"golang.org/x/crypto/ssh"

	"github.com/forgeutah/deuce/server/internal/auth"
	db "github.com/forgeutah/deuce/server/internal/db"
)

// sshKeysTestEnv carries everything an SSH-keys handler test needs: the
// chi router with the actual production routes mounted, a Handler whose
// queries point at a freshly migrated schema, and a seeded user whose ID
// the helper injects into the auth context. A second seeded user lets us
// exercise cross-user isolation (delete-other-user's-key, same-fingerprint-
// different-user) without rebuilding the fixture.
type sshKeysTestEnv struct {
	router *chi.Mux
	h      *Handler
	pool   *pgxpool.Pool
	alice  uuid.UUID
	bob    uuid.UUID
}

// newSSHKeysTestEnv mirrors the DB-backed fixture pattern from
// server/internal/db/user_ssh_keys_test.go: drop-and-recreate the public
// schema, run all embedded migrations, then seed two users. The router is
// mounted at /me so chi URL params (keyID) match the production layout.
func newSSHKeysTestEnv(t *testing.T) *sshKeysTestEnv {
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

	var alice, bob uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('alice@example.com', 'Alice') RETURNING id`).Scan(&alice); err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('bob@example.com', 'Bob') RETURNING id`).Scan(&bob); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	// Build a Handler with only the dependencies these tests need. The rest
	// can stay nil — the SSH-keys handlers only touch queries.
	h := &Handler{queries: queries, pool: pool}

	r := chi.NewRouter()
	// auth.Middleware reads the X-User-ID header (dev-mode pass-through)
	// and injects it into the request context via the package's private
	// context key. Mounting the production middleware keeps the test
	// decoupled from auth's internal context-key shape.
	r.Use(auth.Middleware(""))
	r.Route("/me", func(r chi.Router) {
		r.Route("/ssh-keys", func(r chi.Router) {
			r.Get("/", h.ListMySSHKeys)
			r.Post("/", h.CreateMySSHKey)
			r.Delete("/{keyID}", h.DeleteMySSHKey)
		})
	})

	return &sshKeysTestEnv{router: r, h: h, pool: pool, alice: alice, bob: bob}
}

// do builds a request, sets X-User-ID so auth.Middleware injects the
// expected user into the request context, and runs it through the
// router. Returns the recorder so callers can assert on status / body.
func (env *sshKeysTestEnv) do(t *testing.T, userID uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// doRaw allows sending a non-JSON body for the INVALID_BODY path.
func (env *sshKeysTestEnv) doRaw(t *testing.T, userID uuid.UUID, method, path, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// genPublicKey returns a fresh ed25519 SSH public key in OpenSSH
// authorized_keys format (no trailing newline). Each call returns a
// distinct key, so tests don't have to share or coordinate keys.
func genPublicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey: %v", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

// errorBody is the standard error wrapper writeError produces.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (raw: %q)", err, rec.Body.String())
	}
	return body
}

// ----- tests -----

func TestSSHKeys_CreateValidKey_Returns201WithInlineConfirmation(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label":     "laptop",
		"publicKey": pub,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: want 201, got %d (body %q)", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if got["id"] == nil || got["id"] == "" {
		t.Errorf("create response should include id; got %v", got["id"])
	}
	if got["label"] != "laptop" {
		t.Errorf("label: want laptop, got %v", got["label"])
	}
	fp, ok := got["fingerprint"].(string)
	if !ok || !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint should be a SHA256:… string; got %v", got["fingerprint"])
	}
	if got["createdAt"] == "" || got["createdAt"] == nil {
		t.Errorf("createdAt should be set on create response")
	}
	// Inline-confirmation payload (R15): publicKey is present on the
	// create response so the user can verify what was stored. Strip any
	// trailing newline ssh.MarshalAuthorizedKey adds before comparing.
	if got["publicKey"] != pub {
		t.Errorf("publicKey roundtrip mismatch: want %q, got %v", pub, got["publicKey"])
	}
}

func TestSSHKeys_CreateInvalidKey_Returns400InvalidKeyFormat(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label":     "garbage",
		"publicKey": "not a key",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != "INVALID_KEY_FORMAT" {
		t.Errorf("code: want INVALID_KEY_FORMAT, got %q", got)
	}
}

func TestSSHKeys_CreateInvalidJSONBody_Returns400InvalidBody(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	rec := env.doRaw(t, env.alice, http.MethodPost, "/me/ssh-keys", "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec).Error.Code; got != "INVALID_BODY" {
		t.Errorf("code: want INVALID_BODY, got %q", got)
	}
}

func TestSSHKeys_CreateDuplicateForSameUser_Returns409(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec1 := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label": "first", "publicKey": pub,
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: status %d, body %q", rec1.Code, rec1.Body.String())
	}

	rec2 := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label": "second", "publicKey": pub,
	})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("duplicate: want 409, got %d (body %q)", rec2.Code, rec2.Body.String())
	}
	if got := decodeError(t, rec2).Error.Code; got != "KEY_ALREADY_EXISTS" {
		t.Errorf("code: want KEY_ALREADY_EXISTS, got %q", got)
	}
}

// Same fingerprint posted by two different users must succeed in both
// rows — uniqueness is per-user, not global. Lets corporate yubikeys and
// shared team keys coexist without cross-tenant key-existence leaks.
func TestSSHKeys_CreateSameFingerprintByDifferentUsers_BothSucceed(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec1 := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label": "alice", "publicKey": pub,
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("alice create: status %d, body %q", rec1.Code, rec1.Body.String())
	}

	rec2 := env.do(t, env.bob, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label": "bob", "publicKey": pub,
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("bob (same fp) create: status %d, body %q", rec2.Code, rec2.Body.String())
	}
}

func TestSSHKeys_CreateOverlongKey_Returns400KeyTooLong(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	// 8193-byte ssh-ed25519 line: prefix + a long base64-ish blob. Doesn't
	// need to parse — the length gate fires before ssh.ParseAuthorizedKey.
	overlong := "ssh-ed25519 " + strings.Repeat("A", 8193-len("ssh-ed25519 "))
	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{
		"label": "overlong", "publicKey": overlong,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != "KEY_TOO_LONG" {
		t.Errorf("code: want KEY_TOO_LONG, got %q", got)
	}
}

func TestSSHKeys_GetReturnsOnlyCallingUserKeys_SortedDesc(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	// Alice gets two keys, Bob gets one. List for Alice should return
	// exactly her two, newest first; List for Bob should return his one.
	pubA1 := genPublicKey(t)
	pubA2 := genPublicKey(t)
	pubB := genPublicKey(t)

	if rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "a1", "publicKey": pubA1}); rec.Code != http.StatusCreated {
		t.Fatalf("seed a1: %d %s", rec.Code, rec.Body.String())
	}
	// Force a tick of created_at separation so ORDER BY desc is deterministic.
	time.Sleep(10 * time.Millisecond)
	if rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "a2", "publicKey": pubA2}); rec.Code != http.StatusCreated {
		t.Fatalf("seed a2: %d %s", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, env.bob, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "b1", "publicKey": pubB}); rec.Code != http.StatusCreated {
		t.Fatalf("seed b1: %d %s", rec.Code, rec.Body.String())
	}

	rec := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", rec.Code)
	}
	var keys []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys for alice, got %d (%v)", len(keys), keys)
	}
	if keys[0]["label"] != "a2" || keys[1]["label"] != "a1" {
		t.Errorf("sort order: want [a2, a1] (newest first), got [%v, %v]", keys[0]["label"], keys[1]["label"])
	}
}

// GET must never expose the raw publicKey field — only fingerprint,
// label, and timestamps. This guards against an accidental
// "always-on inline confirmation" refactor leaking every stored key.
func TestSSHKeys_GetNeverReturnsPublicKey(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	if rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "k", "publicKey": pub}); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}

	rec := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "publicKey") {
		t.Errorf("list response leaked publicKey field: %s", body)
	}
	if strings.Contains(body, "ssh-ed25519") {
		t.Errorf("list response leaked raw key material: %s", body)
	}
}

func TestSSHKeys_Delete_Returns204AndKeyGone(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "k", "publicKey": pub})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	keyID, _ := created["id"].(string)
	if keyID == "" {
		t.Fatal("created key missing id")
	}

	del := env.do(t, env.alice, http.MethodDelete, "/me/ssh-keys/"+keyID, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (body %q)", del.Code, del.Body.String())
	}

	list := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if !strings.Contains(list.Body.String(), "[]") {
		t.Errorf("post-delete list should be empty array; got %s", list.Body.String())
	}
}

// Deleting another user's key returns 404, not 403 — leaking 403 vs 404
// would let an attacker enumerate which key IDs belong to other users.
func TestSSHKeys_DeleteOtherUsersKey_Returns404(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "alice-key", "publicKey": pub})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	keyID, _ := created["id"].(string)

	// Bob attempts to delete Alice's key.
	del := env.do(t, env.bob, http.MethodDelete, "/me/ssh-keys/"+keyID, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete: want 404, got %d", del.Code)
	}
	if got := decodeError(t, del).Error.Code; got != "KEY_NOT_FOUND" {
		t.Errorf("code: want KEY_NOT_FOUND, got %q", got)
	}

	// Alice's key still exists.
	list := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if !strings.Contains(list.Body.String(), keyID) {
		t.Errorf("alice's key should still exist after bob's failed delete; list: %s", list.Body.String())
	}
}

func TestSSHKeys_DeleteNonUUID_Returns400InvalidKeyID(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	rec := env.do(t, env.alice, http.MethodDelete, "/me/ssh-keys/not-a-uuid", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-UUID delete: want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec).Error.Code; got != "INVALID_KEY_ID" {
		t.Errorf("code: want INVALID_KEY_ID, got %q", got)
	}
}

func TestSSHKeys_DeleteMissingUUID_Returns404KeyNotFound(t *testing.T) {
	env := newSSHKeysTestEnv(t)

	rec := env.do(t, env.alice, http.MethodDelete, "/me/ssh-keys/"+uuid.New().String(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing UUID delete: want 404, got %d", rec.Code)
	}
	if got := decodeError(t, rec).Error.Code; got != "KEY_NOT_FOUND" {
		t.Errorf("code: want KEY_NOT_FOUND, got %q", got)
	}
}

// Full create → list → delete → list lifecycle. Catches regressions in
// the boundary between handlers (e.g., delete succeeds at SQL but list
// somehow still sees the row).
func TestSSHKeys_Integration_CreateListDeleteList(t *testing.T) {
	env := newSSHKeysTestEnv(t)
	pub := genPublicKey(t)

	rec := env.do(t, env.alice, http.MethodPost, "/me/ssh-keys", map[string]string{"label": "integration", "publicKey": pub})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	keyID, _ := created["id"].(string)

	listRec := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if !strings.Contains(listRec.Body.String(), keyID) {
		t.Errorf("list after create should contain %s; got %s", keyID, listRec.Body.String())
	}

	del := env.do(t, env.alice, http.MethodDelete, "/me/ssh-keys/"+keyID, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", del.Code)
	}

	finalList := env.do(t, env.alice, http.MethodGet, "/me/ssh-keys", nil)
	if strings.Contains(finalList.Body.String(), keyID) {
		t.Errorf("list after delete should not contain %s; got %s", keyID, finalList.Body.String())
	}
}
