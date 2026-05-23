package config

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const (
	AuthModeDev        = "dev"
	AuthModeForgeProxy = "forge-proxy"
)

type Config struct {
	Port            int    `env:"PORT" envDefault:"8080"`
	DatabaseURL     string `env:"DATABASE_URL" envDefault:"postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable"`
	UserID          string `env:"DEUCE_USER_ID" envDefault:"10000000-0000-0000-0000-000000000001"`
	GitHubToken     string `env:"GITHUB_TOKEN" envDefault:""`
	DevPodBin       string `env:"DEVPOD_BIN" envDefault:"devpod"`
	DevPodProvider  string `env:"DEVPOD_PROVIDER" envDefault:"docker"`
	AnthropicAPIKey string `env:"ANTHROPIC_API_KEY" envDefault:""`

	AuthMode             string `env:"DEUCE_AUTH_MODE" envDefault:"dev"`
	ForgeProxySecret     string `env:"FORGE_PROXY_SECRET" envDefault:""`
	ForgeRequiredRole    string `env:"FORGE_REQUIRED_ROLE" envDefault:""`
	ForgeContractVersion int    `env:"FORGE_CONTRACT_VERSION" envDefault:"1"`
	WSAllowedOrigins     string `env:"DEUCE_WS_ALLOWED_ORIGINS" envDefault:"localhost:4000,localhost:8080"`
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

// Validate checks the config for self-consistency before the server binds.
// Returns a non-nil error when proxy mode is enabled without the secrets it
// requires, when an unknown auth mode is set, or when a wildcard WS origin
// would re-open cross-site WebSocket hijacking.
func (c *Config) Validate() error {
	switch c.AuthMode {
	case AuthModeDev, AuthModeForgeProxy:
	default:
		return fmt.Errorf("DEUCE_AUTH_MODE must be %q or %q, got %q", AuthModeDev, AuthModeForgeProxy, c.AuthMode)
	}

	origins := c.WSAllowedOriginList()
	if slices.Contains(origins, "*") {
		return errors.New("DEUCE_WS_ALLOWED_ORIGINS cannot contain '*' — wildcard origins re-open cross-site WebSocket hijacking")
	}

	if c.AuthMode == AuthModeForgeProxy {
		var missing []string
		if c.ForgeProxySecret == "" {
			missing = append(missing, "FORGE_PROXY_SECRET")
		}
		if c.ForgeRequiredRole == "" {
			missing = append(missing, "FORGE_REQUIRED_ROLE")
		}
		if len(origins) == 0 {
			missing = append(missing, "DEUCE_WS_ALLOWED_ORIGINS")
		}
		if len(missing) > 0 {
			return fmt.Errorf("forge-proxy mode requires %s", strings.Join(missing, ", "))
		}
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
