package config

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const (
	AuthModeDev   = "dev"
	AuthModeProxy = "proxy"

	// RolesFormatCSV parses a comma-separated header value (e.g. forge-proxy's
	// X-Forge-Roles: member,admin). Required-role match is exact-equality after
	// whitespace trim.
	RolesFormatCSV = "csv"

	// RolesFormatJSONObject parses a JSON object whose top-level keys are the
	// roles/capabilities (e.g. Tailscale's Tailscale-App-Capabilities:
	// {"example.com/cap/deuce/access":[{}]}). Required-role match is "does the
	// configured role appear as a top-level key in the object".
	RolesFormatJSONObject = "json-object"
)

// headerTokenRE matches a well-formed HTTP header name per RFC 7230 §3.2.6.
// Used to reject typos like "X-Forge Email" (space) before they cause a
// silent miscompare at runtime.
var headerTokenRE = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

type Config struct {
	Port            int    `env:"PORT" envDefault:"8080"`
	DatabaseURL     string `env:"DATABASE_URL" envDefault:"postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable"`
	UserID          string `env:"DEUCE_USER_ID" envDefault:"10000000-0000-0000-0000-000000000001"`
	GitHubToken     string `env:"GITHUB_TOKEN" envDefault:""`
	DevPodBin       string `env:"DEVPOD_BIN" envDefault:"devpod"`
	DevPodProvider  string `env:"DEVPOD_PROVIDER" envDefault:"docker"`
	AnthropicAPIKey string `env:"ANTHROPIC_API_KEY" envDefault:""`

	// PrebuildRepository turns on the devcontainer prebuild cache. Empty
	// (the default) keeps the original behaviour exactly: every session
	// runs a from-scratch `devpod up` and installs Pi over `devpod ssh`.
	// When set, each repo's devcontainer image is built once per
	// devcontainer-definition hash, has Pi and the agent tooling baked on
	// top, and later sessions start from that cached image.
	//
	// The value is a Docker repository name. It does not have to resolve
	// to a registry — tags stay local to the Docker daemon, because the
	// image is consumed via `devpod up --devcontainer-image` rather than
	// `--prebuild-repository` (which only ever does a registry lookup).
	PrebuildRepository string `env:"DEUCE_PREBUILD_REPOSITORY" envDefault:""`

	// PiProvider/PiModel configure the Pi agent backend (the sole harness);
	// v1 runs Claude models through Pi.
	PiProvider string `env:"DEUCE_PI_PROVIDER" envDefault:"anthropic"`
	PiModel    string `env:"DEUCE_PI_MODEL" envDefault:"claude-haiku-4-5"`

	// AgentSystemPrompt is a global system prompt prepended to every agent's own
	// system_prompt on the Pi path. Empty means use agent.DefaultBaseSystemPrompt
	// (the built-in "prefer ask_user when blocked" guidance); set it to override.
	AgentSystemPrompt string `env:"DEUCE_AGENT_SYSTEM_PROMPT" envDefault:""`

	AuthMode string `env:"DEUCE_AUTH_MODE" envDefault:"dev"`

	// Unified proxy-mode configuration. No defaults — operators wire each
	// header to whatever their reverse proxy emits. Optional checks
	// (secret, contract version, required role) fire only when their
	// backing env var pair is configured.
	ProxyHeaderEmail           string `env:"DEUCE_PROXY_HEADER_EMAIL" envDefault:""`
	ProxyHeaderName            string `env:"DEUCE_PROXY_HEADER_NAME" envDefault:""`
	ProxyHeaderAvatar          string `env:"DEUCE_PROXY_HEADER_AVATAR" envDefault:""`
	ProxyHeaderSecret          string `env:"DEUCE_PROXY_HEADER_SECRET" envDefault:""`
	ProxySecret                string `env:"DEUCE_PROXY_SECRET" envDefault:""`
	ProxyHeaderContractVersion string `env:"DEUCE_PROXY_HEADER_CONTRACT_VERSION" envDefault:""`
	ProxyContractVersion       int    `env:"DEUCE_PROXY_CONTRACT_VERSION" envDefault:"0"`
	ProxyHeaderRoles           string `env:"DEUCE_PROXY_HEADER_ROLES" envDefault:""`
	ProxyRolesFormat           string `env:"DEUCE_PROXY_ROLES_FORMAT" envDefault:""`
	ProxyRequiredRole          string `env:"DEUCE_PROXY_REQUIRED_ROLE" envDefault:""`

	WSAllowedOrigins string `env:"DEUCE_WS_ALLOWED_ORIGINS" envDefault:"localhost:4000,localhost:8080"`

	// SSH proxy. Empty SSHListenAddr disables the embedded SSH listener
	// entirely; HTTP keeps serving and /api/sessions/:id/vscode-uri
	// returns 503 SSH_UNAVAILABLE. The host key is generated on first
	// boot if missing; persist the directory across deploys.
	SSHListenAddr          string `env:"DEUCE_SSH_LISTEN_ADDR" envDefault:":2222"`
	SSHHostKeyPath         string `env:"DEUCE_SSH_HOST_KEY_PATH" envDefault:""`
	SSHHandshakeTimeoutSec int    `env:"DEUCE_SSH_HANDSHAKE_TIMEOUT_SEC" envDefault:"10"`
	SSHMaxHandshakesPerIP  int    `env:"DEUCE_SSH_MAX_HANDSHAKES_PER_IP" envDefault:"8"`
	SSHMaxChannelsPerConn  int    `env:"DEUCE_SSH_MAX_CHANNELS_PER_CONN" envDefault:"8"`
	SSHGoroutineCap        int    `env:"DEUCE_SSH_GOROUTINE_CAP" envDefault:"4000"`

	// PublicHostname is the externally-reachable hostname embedded in
	// the vscode:// URI returned by /api/sessions/:id/vscode-uri.
	// REQUIRED in proxy mode (Host-header spoofing would otherwise let
	// an unsanitized reverse proxy inject a malicious destination).
	// In dev mode, empty falls back to the request Host header.
	PublicHostname string `env:"DEUCE_PUBLIC_HOSTNAME" envDefault:""`
}

// WSAllowedOriginList returns the configured allowed origins split and trimmed.
// Empty entries are dropped.
func (c *Config) WSAllowedOriginList() []string {
	parts := strings.Split(c.WSAllowedOrigins, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ProxySecretCheckEnabled reports whether the operator configured a shared
// secret header. Used by the middleware to short-circuit the check when
// unconfigured, and by main.go's startup WARN when it surfaces disabled
// application-layer gates.
func (c *Config) ProxySecretCheckEnabled() bool {
	return c.ProxyHeaderSecret != ""
}

// ProxyContractCheckEnabled mirrors ProxySecretCheckEnabled for the
// contract-version pin.
func (c *Config) ProxyContractCheckEnabled() bool {
	return c.ProxyHeaderContractVersion != ""
}

// ProxyRoleCheckEnabled mirrors ProxySecretCheckEnabled for the required-role
// gate. When false in proxy mode, admission relies entirely on network
// boundary plus identity-header presence.
func (c *Config) ProxyRoleCheckEnabled() bool {
	return c.ProxyHeaderRoles != ""
}

// prebuildRepoRE matches a Docker repository name without a tag: optional
// registry host (with optional port), then one or more lowercase path
// components. Deliberately strict — the value reaches `docker build -t` and
// `devpod up --devcontainer-image` argv, and a tag separator here would
// collide with the hash tag Deuce appends.
var prebuildRepoRE = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)

// PrebuildEnabled reports whether the devcontainer prebuild cache is turned
// on. When false, workspace creation keeps its original from-scratch path.
func (c *Config) PrebuildEnabled() bool {
	return c.PrebuildRepository != ""
}

// Validate checks the config for self-consistency before the server binds.
// In proxy mode it refuses to start when the optional-check env-var pairs
// are asymmetric (a header without its value, or vice versa), when the
// required identity headers are missing, when configured header names
// collide or are malformed, or when WebSocket origins are wildcarded.
func (c *Config) Validate() error {
	switch c.AuthMode {
	case AuthModeDev, AuthModeProxy:
	case "forge-proxy":
		// Migration hint for operators upgrading from the previous
		// vendor-specific mode. The unified middleware replaces it.
		return errors.New("DEUCE_AUTH_MODE=forge-proxy is no longer supported; set DEUCE_AUTH_MODE=proxy and configure DEUCE_PROXY_HEADER_* env vars (see CLAUDE.md for forge-proxy example block)")
	default:
		return fmt.Errorf("DEUCE_AUTH_MODE must be %q or %q, got %q", AuthModeDev, AuthModeProxy, c.AuthMode)
	}

	origins := c.WSAllowedOriginList()
	if slices.Contains(origins, "*") {
		return errors.New("DEUCE_WS_ALLOWED_ORIGINS cannot contain '*' — wildcard origins re-open cross-site WebSocket hijacking")
	}

	// Caught at startup rather than at first session create, where the
	// failure would surface as a confusing mid-provisioning docker error.
	if c.PrebuildRepository != "" && !prebuildRepoRE.MatchString(c.PrebuildRepository) {
		return fmt.Errorf("DEUCE_PREBUILD_REPOSITORY=%q is not a valid Docker repository name (lowercase, no tag — Deuce appends the devcontainer hash as the tag)", c.PrebuildRepository)
	}

	if c.AuthMode == AuthModeProxy {
		return c.validateProxyMode(origins)
	}

	return nil
}

// validateProxyMode aggregates every misconfiguration in proxy mode into a
// single error so operators see all of them at once rather than fixing one,
// restarting, hitting the next, repeating.
func (c *Config) validateProxyMode(origins []string) error {
	var problems []string

	// Email is the only required identity header — it's the lookup key.
	// Name is optional: when not provided, the user lands on a welcome
	// screen and supplies their display name before reaching the app.
	// Avatar is also optional; rejected schemes silently coerce to empty.
	if c.ProxyHeaderEmail == "" {
		problems = append(problems, "DEUCE_PROXY_HEADER_EMAIL")
	}
	if len(origins) == 0 {
		problems = append(problems, "DEUCE_WS_ALLOWED_ORIGINS")
	}

	// PublicHostname required in proxy mode: the vscode:// URI builder
	// would otherwise fall back to the request Host header, which a
	// misconfigured reverse proxy could let an attacker inject. In dev
	// mode the Host header is fine — there is no untrusted ingress.
	if c.PublicHostname == "" && c.SSHListenAddr != "" {
		problems = append(problems, "DEUCE_PUBLIC_HOSTNAME (required when DEUCE_AUTH_MODE=proxy and SSH proxy is enabled)")
	}

	// Optional-check pairs must be set symmetrically. Setting one without
	// the other is almost always a typo; treating it as a startup error
	// rather than silently disabling the check matches the fail-closed
	// posture the proxy mode is supposed to enforce.
	if (c.ProxyHeaderSecret == "") != (c.ProxySecret == "") {
		problems = append(problems, "DEUCE_PROXY_HEADER_SECRET and DEUCE_PROXY_SECRET must both be set or both empty")
	}

	if c.ProxyHeaderContractVersion != "" && c.ProxyContractVersion <= 0 {
		problems = append(problems, "DEUCE_PROXY_CONTRACT_VERSION must be > 0 when DEUCE_PROXY_HEADER_CONTRACT_VERSION is set")
	}
	if c.ProxyContractVersion > 0 && c.ProxyHeaderContractVersion == "" {
		problems = append(problems, "DEUCE_PROXY_HEADER_CONTRACT_VERSION must be set when DEUCE_PROXY_CONTRACT_VERSION is set")
	}

	if c.ProxyHeaderRoles != "" {
		if c.ProxyRequiredRole == "" {
			problems = append(problems, "DEUCE_PROXY_REQUIRED_ROLE must be set when DEUCE_PROXY_HEADER_ROLES is set")
		}
		switch c.ProxyRolesFormat {
		case RolesFormatCSV, RolesFormatJSONObject:
		case "":
			problems = append(problems, fmt.Sprintf("DEUCE_PROXY_ROLES_FORMAT must be %q or %q when DEUCE_PROXY_HEADER_ROLES is set", RolesFormatCSV, RolesFormatJSONObject))
		default:
			problems = append(problems, fmt.Sprintf("DEUCE_PROXY_ROLES_FORMAT=%q is invalid; expected %q or %q", c.ProxyRolesFormat, RolesFormatCSV, RolesFormatJSONObject))
		}
	}
	if c.ProxyHeaderRoles == "" && (c.ProxyRequiredRole != "" || c.ProxyRolesFormat != "") {
		problems = append(problems, "DEUCE_PROXY_HEADER_ROLES must be set when DEUCE_PROXY_REQUIRED_ROLE or DEUCE_PROXY_ROLES_FORMAT is set")
	}

	// Configured header names must be unique among themselves — two slots
	// pointing at the same incoming header would have ambiguous semantics
	// (which value lands in which middleware variable). And each name must
	// be a well-formed HTTP header token so a typo like "X-Forge Email"
	// (with a space) fails at startup rather than silently never matching.
	headerSlots := []struct {
		envVar string
		value  string
	}{
		{"DEUCE_PROXY_HEADER_EMAIL", c.ProxyHeaderEmail},
		{"DEUCE_PROXY_HEADER_NAME", c.ProxyHeaderName},
		{"DEUCE_PROXY_HEADER_AVATAR", c.ProxyHeaderAvatar},
		{"DEUCE_PROXY_HEADER_SECRET", c.ProxyHeaderSecret},
		{"DEUCE_PROXY_HEADER_CONTRACT_VERSION", c.ProxyHeaderContractVersion},
		{"DEUCE_PROXY_HEADER_ROLES", c.ProxyHeaderRoles},
	}
	seen := make(map[string]string, len(headerSlots))
	for _, slot := range headerSlots {
		if slot.value == "" {
			continue
		}
		if !headerTokenRE.MatchString(slot.value) {
			problems = append(problems, fmt.Sprintf("%s=%q is not a valid HTTP header name", slot.envVar, slot.value))
			continue
		}
		if other, ok := seen[slot.value]; ok {
			problems = append(problems, fmt.Sprintf("%s and %s both map to header %q", other, slot.envVar, slot.value))
		} else {
			seen[slot.value] = slot.envVar
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("proxy mode misconfigured: %s", strings.Join(problems, "; "))
	}
	return nil
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
