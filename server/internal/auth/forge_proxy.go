package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/forgeutah/deuce/server/internal/db"
)

const (
	headerSecret          = "X-Forge-Proxy-Secret"
	headerContractVersion = "X-Forge-Contract-Version"
	headerUserID          = "X-Forge-User-Id"
	headerEmail           = "X-Forge-Email"
	headerName            = "X-Forge-Name"
	headerAvatar          = "X-Forge-Avatar"
	headerRoles           = "X-Forge-Roles"

	codeNotAuthenticated      = "NOT_AUTHENTICATED"
	codeInvalidContractVersion = "INVALID_CONTRACT_VERSION"
	codeInvalidHeaders         = "INVALID_HEADERS"
	codeNotAuthorized          = "NOT_AUTHORIZED"
	codeDBError                = "DB_ERROR"

	maxHeaderLogLen = 256
)

// ForgeUserStore is the slice of db.Queries the middleware needs. Defined
// as an interface so tests can inject a fake without touching pgx.
type ForgeUserStore interface {
	LookupUserByForgeID(ctx context.Context, forgeUserID pgtype.Int8) (db.User, error)
	CreateUserByForgeID(ctx context.Context, arg db.CreateUserByForgeIDParams) (db.User, error)
}

// ForgeProxyMiddleware returns a chi-compatible middleware that admits requests
// authenticated by forge-proxy. It validates the shared secret in constant time,
// pins the contract version, checks the required role, resolves the user (lookup
// first, insert on miss), and stores the user's UUID in the same context key the
// existing dev middleware uses — so downstream handlers see no difference.
//
// Configuration is captured at construction time; the middleware closes over the
// secret bytes once to avoid re-allocating on every request.
func ForgeProxyMiddleware(store ForgeUserStore, secret, requiredRole string, contractVersion int) func(http.Handler) http.Handler {
	secretBytes := []byte(secret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Secret check — duplicate-header rejection then constant-time compare.
			got, ok := singleHeader(r, headerSecret)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "missing or duplicate proxy secret header")
				return
			}
			gotBytes := []byte(got)
			if len(gotBytes) != len(secretBytes) || subtle.ConstantTimeCompare(gotBytes, secretBytes) != 1 {
				writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "invalid proxy secret")
				return
			}

			// 2. Contract version check.
			ver, ok := singleHeader(r, headerContractVersion)
			if !ok {
				writeAuthError(w, http.StatusBadRequest, codeInvalidContractVersion, "missing or duplicate contract version header")
				return
			}
			parsedVer, err := strconv.Atoi(ver)
			if err != nil || parsedVer != contractVersion {
				writeAuthError(w, http.StatusBadRequest, codeInvalidContractVersion, "unsupported contract version")
				return
			}

			// 3. User id parse.
			rawUserID, ok := singleHeader(r, headerUserID)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "missing or duplicate user id header")
				return
			}
			forgeID, err := strconv.ParseInt(rawUserID, 10, 64)
			if err != nil || forgeID <= 0 {
				writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "invalid user id")
				return
			}

			// 4. Role check — CSV split, trim, equality (not substring).
			rolesHeader, ok := singleHeader(r, headerRoles)
			if !ok {
				// Missing header is treated as no roles -> not authorized.
				rolesHeader = ""
			}
			if !roleListContains(rolesHeader, requiredRole) {
				email := singleHeaderOr(r, headerEmail, "")
				slog.Info("auth.forge_proxy: rejected (missing required role)",
					"forge_user_id", forgeID,
					"email", sanitizeForLog(email),
					"required_role", requiredRole,
				)
				writeAuthError(w, http.StatusForbidden, codeNotAuthorized, "missing required role")
				return
			}

			// 5. Other header reads + avatar scheme validation.
			email, ok := singleHeader(r, headerEmail)
			if !ok {
				writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "missing or duplicate email header")
				return
			}
			name, ok := singleHeader(r, headerName)
			if !ok {
				writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "missing or duplicate name header")
				return
			}
			avatar := singleHeaderOr(r, headerAvatar, "")
			if avatar != "" {
				avatar = validatedAvatar(avatar)
			}

			// 6. Resolve user: lookup, insert-if-missing, re-lookup on race.
			forgeIDParam := pgtype.Int8{Int64: forgeID, Valid: true}
			user, err := store.LookupUserByForgeID(r.Context(), forgeIDParam)
			created := false
			switch {
			case err == nil:
				// Existing user — use as-is. Profile fields are intentionally not
				// refreshed on subsequent requests (stale-profile refresh is a
				// future plan).
			case errors.Is(err, pgx.ErrNoRows):
				user, err = store.CreateUserByForgeID(r.Context(), db.CreateUserByForgeIDParams{
					ForgeUserID: forgeIDParam,
					Name:        name,
					Email:       email,
					Avatar:      avatar,
				})
				switch {
				case err == nil:
					created = true
				case errors.Is(err, pgx.ErrNoRows):
					// Lost the concurrent first-arrival race (ON CONFLICT DO NOTHING).
					// The winning insert wrote the row; pick it up.
					user, err = store.LookupUserByForgeID(r.Context(), forgeIDParam)
					if err != nil {
						slog.Error("auth.forge_proxy: lookup after race failed", "forge_user_id", forgeID, "error", err)
						writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
						return
					}
				default:
					slog.Error("auth.forge_proxy: create failed", "forge_user_id", forgeID, "error", err)
					writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
					return
				}
			default:
				slog.Error("auth.forge_proxy: lookup failed", "forge_user_id", forgeID, "error", err)
				writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
				return
			}

			if created {
				slog.Info("auth.forge_proxy: provisioned user",
					"forge_user_id", forgeID,
					"email", sanitizeForLog(email),
					"deuce_user_id", user.ID.String(),
				)
			}

			ctx := context.WithValue(r.Context(), userIDKey, user.ID.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// singleHeader returns the header value when exactly one instance is present.
// Returns ok=false for missing or duplicate headers — duplicates are treated as
// a smuggling attempt and rejected by the caller.
func singleHeader(r *http.Request, name string) (string, bool) {
	v := r.Header.Values(name)
	if len(v) != 1 {
		return "", false
	}
	return v[0], true
}

// singleHeaderOr returns the header value when exactly one instance is present,
// otherwise the fallback. Used for optional headers (avatar, audit-log email)
// where missing is OK but duplicate must not silently coalesce.
func singleHeaderOr(r *http.Request, name, fallback string) string {
	v := r.Header.Values(name)
	if len(v) == 1 {
		return v[0]
	}
	return fallback
}

func roleListContains(csv, required string) bool {
	if required == "" {
		return false
	}
	parts := strings.Split(csv, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == required {
			return true
		}
	}
	return false
}

// validatedAvatar returns the URL if its scheme is http or https, otherwise the
// empty string. This is the only sanitization applied to header-supplied data
// before it lands in the DB; React JSX escaping handles the rest.
func validatedAvatar(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if !slices.Contains([]string{"http", "https"}, scheme) {
		return ""
	}
	return raw
}

// sanitizeForLog strips CR/LF/NUL bytes from a header value so it cannot inject
// fake log lines, and clamps it to maxHeaderLogLen so a hostile caller cannot
// flood logs with a huge value.
func sanitizeForLog(s string) string {
	if s == "" {
		return ""
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s) && len(b) < maxHeaderLogLen; i++ {
		c := s[i]
		if c == '\r' || c == '\n' || c == 0 {
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
