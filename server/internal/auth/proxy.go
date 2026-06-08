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

	"github.com/forgeutah/deuce/server/internal/config"
	db "github.com/forgeutah/deuce/server/internal/db"
)

const (
	codeNotAuthenticated       = "NOT_AUTHENTICATED"
	codeInvalidContractVersion = "INVALID_CONTRACT_VERSION"
	codeInvalidHeaders         = "INVALID_HEADERS"
	codeNotAuthorized          = "NOT_AUTHORIZED"
	codeDBError                = "DB_ERROR"

	maxHeaderLogLen = 256
)

// ProxyUserStore is the slice of db.Queries the middleware needs. Defined as
// an interface so tests can inject a fake without touching pgx.
type ProxyUserStore interface {
	LookupUserByEmail(ctx context.Context, email string) (db.User, error)
	CreateUserByEmail(ctx context.Context, arg db.CreateUserByEmailParams) (db.User, error)
	GetDefaultTeam(ctx context.Context) (db.Team, error)
	AddTeamMember(ctx context.Context, arg db.AddTeamMemberParams) error
}

// ProxyConfig is the resolved subset of *config.Config the middleware needs.
// Pulled out as its own type so tests construct it directly without going
// through env parsing.
type ProxyConfig struct {
	EmailHeader  string
	NameHeader   string
	AvatarHeader string

	SecretHeader string
	Secret       string

	ContractVersionHeader string
	ContractVersion       int

	RolesHeader  string
	RolesFormat  string
	RequiredRole string
}

// ProxyConfigFromConfig builds the middleware's view of config from the loaded
// application config. Used at server startup.
func ProxyConfigFromConfig(c *config.Config) ProxyConfig {
	return ProxyConfig{
		EmailHeader:           c.ProxyHeaderEmail,
		NameHeader:            c.ProxyHeaderName,
		AvatarHeader:          c.ProxyHeaderAvatar,
		SecretHeader:          c.ProxyHeaderSecret,
		Secret:                c.ProxySecret,
		ContractVersionHeader: c.ProxyHeaderContractVersion,
		ContractVersion:       c.ProxyContractVersion,
		RolesHeader:           c.ProxyHeaderRoles,
		RolesFormat:           c.ProxyRolesFormat,
		RequiredRole:          c.ProxyRequiredRole,
	}
}

// ProxyMiddleware returns a chi-compatible middleware that admits requests
// authenticated by a header-trust reverse proxy (forge-proxy, Tailscale Serve,
// or any other proxy that injects identity headers and is the sole ingress).
//
// Header names are configured per deployment. Optional checks (shared secret,
// contract version, required role) fire only when their backing config is
// supplied, so forge-proxy turns all three on and Tailscale Serve turns only
// the role check on.
//
// The user is identified by email — case- and whitespace-normalized before
// lookup so a misconfigured upstream that emits "Alice@Example.COM" and
// "alice@example.com" does not provision separate accounts. The middleware
// looks up first, inserts on miss (ON CONFLICT (email) DO NOTHING), and
// re-looks-up if the insert lost a concurrent race. The user's UUID is stored
// in the same context key the existing dev middleware uses, so downstream
// handlers are mode-agnostic.
func ProxyMiddleware(store ProxyUserStore, pc ProxyConfig) func(http.Handler) http.Handler {
	secretBytes := []byte(pc.Secret)
	secretCheckEnabled := pc.SecretHeader != ""
	contractCheckEnabled := pc.ContractVersionHeader != ""
	roleCheckEnabled := pc.RolesHeader != ""

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Optional secret check — constant-time compare after length
			// precheck. Reads via r.Header.Values() so duplicate-header
			// smuggling is rejected.
			if secretCheckEnabled {
				got, ok := singleHeader(r, pc.SecretHeader)
				if !ok {
					writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "missing or duplicate proxy secret header")
					return
				}
				gotBytes := []byte(got)
				if len(gotBytes) != len(secretBytes) || subtle.ConstantTimeCompare(gotBytes, secretBytes) != 1 {
					writeAuthError(w, http.StatusUnauthorized, codeNotAuthenticated, "invalid proxy secret")
					return
				}
			}

			// 2. Optional contract version check.
			if contractCheckEnabled {
				ver, ok := singleHeader(r, pc.ContractVersionHeader)
				if !ok {
					writeAuthError(w, http.StatusBadRequest, codeInvalidContractVersion, "missing or duplicate contract version header")
					return
				}
				parsed, err := strconv.Atoi(ver)
				if err != nil || parsed != pc.ContractVersion {
					writeAuthError(w, http.StatusBadRequest, codeInvalidContractVersion, "unsupported contract version")
					return
				}
			}

			// 3. Email is the only required identity header — it's the
			// lookup key. Name is optional: when no name header is
			// configured (or the proxy doesn't supply one), the user is
			// provisioned with an empty name and the frontend gates the
			// app behind a welcome screen until they choose a display
			// name. When a name header IS configured, it must be present
			// and non-empty — a configured-but-missing name is operator
			// misconfiguration, not "new user, please welcome."
			rawEmail, ok := singleHeader(r, pc.EmailHeader)
			if !ok {
				writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "missing or duplicate email header")
				return
			}
			email := strings.ToLower(strings.TrimSpace(rawEmail))
			if email == "" {
				writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "empty email header")
				return
			}
			var name string
			if pc.NameHeader != "" {
				got, ok := singleHeader(r, pc.NameHeader)
				if !ok {
					writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "missing or duplicate name header")
					return
				}
				if strings.TrimSpace(got) == "" {
					writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "empty name header")
					return
				}
				name = got
			}

			// 4. Optional avatar — scheme-validated, silently coerced to
			// empty when invalid (operator-controlled, not auth-bearing).
			var avatar string
			if pc.AvatarHeader != "" {
				avatar = singleHeaderOr(r, pc.AvatarHeader, "")
				if avatar != "" {
					avatar = validatedAvatar(avatar)
				}
			}

			// 5. Optional role check. Treat parse failures, unrecognized
			// formats, and missing-required-role identically: 403. The
			// HTTP response body never echoes parse errors (no information
			// disclosure); the server-side log carries the reason so an
			// operator can distinguish proxy misconfiguration from real
			// role denials.
			if roleCheckEnabled {
				vals := r.Header.Values(pc.RolesHeader)
				if len(vals) > 1 {
					writeAuthError(w, http.StatusBadRequest, codeInvalidHeaders, "duplicate roles header")
					return
				}
				rolesHeader := ""
				if len(vals) == 1 {
					rolesHeader = vals[0]
				}
				admitted, reason := rolesContains(rolesHeader, pc.RolesFormat, pc.RequiredRole)
				if !admitted {
					slog.Info("auth.proxy: rejected (role check failed)",
						"email", sanitizeForLog(email),
						"required_role", pc.RequiredRole,
						"reason", reason,
					)
					writeAuthError(w, http.StatusForbidden, codeNotAuthorized, "missing required role")
					return
				}
			}

			// 6. Resolve user: lookup, insert-if-missing, re-lookup on race.
			// ON CONFLICT (email) DO NOTHING makes the race-loser path
			// return pgx.ErrNoRows from the INSERT; the re-lookup picks up
			// the winner's row.
			user, err := store.LookupUserByEmail(r.Context(), email)
			created := false
			switch {
			case err == nil:
				// Existing user — name/avatar are intentionally not refreshed
				// (stale-profile refresh is a future plan).
			case errors.Is(err, pgx.ErrNoRows):
				user, err = store.CreateUserByEmail(r.Context(), db.CreateUserByEmailParams{
					Email:  email,
					Name:   name,
					Avatar: avatar,
				})
				switch {
				case err == nil:
					created = true
				case errors.Is(err, pgx.ErrNoRows):
					// Race-loser: ON CONFLICT DO NOTHING swallowed the
					// insert; the winner's row is now there to look up.
					user, err = store.LookupUserByEmail(r.Context(), email)
					if err != nil {
						slog.Error("auth.proxy: lookup after race failed", "email", sanitizeForLog(email), "error", err)
						writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
						return
					}
				default:
					slog.Error("auth.proxy: create failed", "email", sanitizeForLog(email), "error", err)
					writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
					return
				}
			default:
				slog.Error("auth.proxy: lookup failed", "email", sanitizeForLog(email), "error", err)
				writeAuthError(w, http.StatusInternalServerError, codeDBError, "internal error")
				return
			}

			if created {
				// Team membership is the read boundary for sessions, so a
				// brand-new user with no team would see nothing. Auto-join them
				// to the default team. Non-fatal and idempotent: if the default
				// team is missing or the add fails, the user is still admitted
				// (they see no sessions until added to a team out-of-band)
				// rather than blocked by a 500.
				if team, terr := store.GetDefaultTeam(r.Context()); terr != nil {
					slog.Warn("auth.proxy: no default team to auto-join",
						"email", sanitizeForLog(email), "error", terr)
				} else if aerr := store.AddTeamMember(r.Context(), db.AddTeamMemberParams{
					TeamID: team.ID,
					UserID: user.ID,
				}); aerr != nil {
					slog.Warn("auth.proxy: default-team auto-join failed",
						"email", sanitizeForLog(email), "error", aerr)
				}

				slog.Info("auth.proxy: provisioned user",
					"email", sanitizeForLog(email),
					"deuce_user_id", user.ID.String(),
				)
			}

			ctx := context.WithValue(r.Context(), userIDKey, user.ID.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// rolesContains parses the roles header per the configured format and returns
// whether the required role is present, plus a reason string for the audit
// log when admission fails. Reasons are stable identifiers ("role_missing",
// "parse_error", "wrong_shape", "unknown_format") so log filters work.
func rolesContains(header, format, required string) (bool, string) {
	if required == "" {
		return false, "no_required_role_configured"
	}
	switch format {
	case config.RolesFormatCSV:
		for _, p := range strings.Split(header, ",") {
			if strings.TrimSpace(p) == required {
				return true, ""
			}
		}
		return false, "role_missing"
	case config.RolesFormatJSONObject:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(header), &obj); err != nil {
			return false, "parse_error"
		}
		if _, ok := obj[required]; ok {
			return true, ""
		}
		return false, "role_missing"
	default:
		return false, "unknown_format"
	}
}

// singleHeader returns the header value when exactly one instance is present.
// Returns ok=false for missing or duplicate headers — duplicates are treated
// as a smuggling attempt and rejected by the caller.
func singleHeader(r *http.Request, name string) (string, bool) {
	v := r.Header.Values(name)
	if len(v) != 1 {
		return "", false
	}
	return v[0], true
}

// singleHeaderOr returns the header value when exactly one instance is
// present, otherwise the fallback. Used for optional headers (avatar) where
// missing is OK but duplicate must not silently coalesce.
func singleHeaderOr(r *http.Request, name, fallback string) string {
	v := r.Header.Values(name)
	if len(v) == 1 {
		return v[0]
	}
	return fallback
}

// validatedAvatar returns the URL if its scheme is http or https, otherwise
// the empty string. This is the only sanitization applied to header-supplied
// data before it lands in the DB; React JSX escaping handles the rest.
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

// sanitizeForLog strips CR/LF/NUL bytes from a header value so it cannot
// inject fake log lines, and clamps it to maxHeaderLogLen so a hostile caller
// cannot flood logs with a huge value.
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
