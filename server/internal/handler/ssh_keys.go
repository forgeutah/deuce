package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/ssh"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// maxSSHKeyLabelLen mirrors the CHECK constraint on user_ssh_keys.label
// (length <= 255). Validating here returns a clean 400 with a stable error
// code instead of leaking the underlying SQLSTATE 23514.
const maxSSHKeyLabelLen = 255

// maxSSHPublicKeyLen mirrors the CHECK constraint on user_ssh_keys.public_key
// (length BETWEEN 1 AND 8192). Pre-validating lets us return 400
// KEY_TOO_LONG instead of paying for a round-trip just to surface the
// CHECK violation.
const maxSSHPublicKeyLen = 8192

// sshKeyResponse is the over-the-wire shape for a user-owned SSH key.
// JSON tags are camelCase to match the rest of the frontend contract
// (CLAUDE.md rule). publicKey is intentionally omitted on list/get
// responses — only the create response (R15 inline confirmation) sets it
// so the user can verify what was stored. Returning the raw key in every
// list response would let any compromised client snapshot every key on
// file in one GET.
type sshKeyResponse struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Fingerprint string  `json:"fingerprint"`
	PublicKey   string  `json:"publicKey,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	LastUsedAt  *string `json:"lastUsedAt"`
}

// toSSHKeyResponse projects a db row into the wire shape with public_key
// omitted. Use newSSHKeyCreateResponse for the create path that needs the
// inline-confirmation payload.
func toSSHKeyResponse(row db.UserSshKey) sshKeyResponse {
	resp := sshKeyResponse{
		ID:          row.ID.String(),
		Label:       row.Label,
		Fingerprint: row.Fingerprint,
		CreatedAt:   row.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	if row.LastUsedAt.Valid {
		t := row.LastUsedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z")
		resp.LastUsedAt = &t
	}
	return resp
}

// newSSHKeyCreateResponse is the only path that fills publicKey — it's
// the 201 response body so the frontend can render an inline
// "Key added: <label> (SHA256:…)" confirmation against the value the
// server actually stored, not the user's pasted text.
func newSSHKeyCreateResponse(row db.UserSshKey) sshKeyResponse {
	resp := toSSHKeyResponse(row)
	resp.PublicKey = row.PublicKey
	return resp
}

// ListMySSHKeys returns the authenticated user's SSH keys sorted by
// created_at desc (handled by the sqlc query). publicKey is never
// returned here — only the create response includes it.
func (h *Handler) ListMySSHKeys(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	rows, err := h.queries.ListUserSSHKeys(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "internal error")
		return
	}

	out := make([]sshKeyResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSSHKeyResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateMySSHKey parses a {label, publicKey} body, validates the key
// with ssh.ParseAuthorizedKey, computes the SHA256 fingerprint, and
// inserts via the sqlc query. The 201 response carries the full new key
// (id, label, fingerprint, createdAt, publicKey) so the frontend can
// render the R15 inline confirmation immediately.
func (h *Handler) CreateMySSHKey(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	var body struct {
		Label     string `json:"label"`
		PublicKey string `json:"publicKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}

	label := strings.TrimSpace(body.Label)
	if len(label) > maxSSHKeyLabelLen {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "label too long")
		return
	}

	publicKey := strings.TrimSpace(body.PublicKey)
	if publicKey == "" {
		writeError(w, http.StatusBadRequest, "INVALID_KEY_FORMAT", "publicKey is required")
		return
	}
	// Length-gate BEFORE parse so a multi-megabyte paste cannot force the
	// SSH parser to allocate against the full string. The CHECK constraint
	// would also reject this, but pre-validating returns a stable error
	// code rather than leaking SQLSTATE 23514.
	if len(publicKey) > maxSSHPublicKeyLen {
		writeError(w, http.StatusBadRequest, "KEY_TOO_LONG", "publicKey exceeds 8192 bytes")
		return
	}

	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_KEY_FORMAT", "could not parse public key")
		return
	}
	fingerprint := ssh.FingerprintSHA256(parsedKey)

	row, err := h.queries.CreateUserSSHKey(r.Context(), db.CreateUserSSHKeyParams{
		UserID:      userID,
		Label:       label,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
	})
	if err != nil {
		// Unique-constraint violation on (user_id, fingerprint) means this
		// user already has this exact key. 409 returns the canonical "already
		// exists" — note that per-user uniqueness (not global) prevents this
		// path from leaking key existence across tenants.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "KEY_ALREADY_EXISTS", "this SSH key is already on file for your account")
			return
		}
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, newSSHKeyCreateResponse(row))
}

// DeleteMySSHKey scopes by (id, user_id) at the SQL layer. A path-keyed
// mismatch — non-existent key OR another user's key — returns 404
// KEY_NOT_FOUND rather than 403 so we don't reveal whether the key
// exists at all on another account.
func (h *Handler) DeleteMySSHKey(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	keyID, err := uuid.Parse(chi.URLParam(r, "keyID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_KEY_ID", "invalid key ID")
		return
	}

	// GetUserSSHKey is scoped by (id, user_id) so a missing-or-wrong-owner
	// row indistinguishably returns pgx.ErrNoRows. Mapping that to 404
	// avoids leaking key existence.
	if _, err := h.queries.GetUserSSHKey(r.Context(), db.GetUserSSHKeyParams{
		ID:     keyID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "KEY_NOT_FOUND", "key not found")
		return
	}

	if err := h.queries.DeleteUserSSHKey(r.Context(), db.DeleteUserSSHKeyParams{
		ID:     keyID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
