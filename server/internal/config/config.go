package config

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	Port           int    `env:"PORT" envDefault:"8080"`
	DatabaseURL    string `env:"DATABASE_URL" envDefault:"postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable"`
	UserID         string `env:"DEUCE_USER_ID" envDefault:"10000000-0000-0000-0000-000000000001"`
	GitHubToken    string `env:"GITHUB_TOKEN" envDefault:""`
	DevPodBin      string `env:"DEVPOD_BIN" envDefault:"devpod"`
	DevPodProvider  string `env:"DEVPOD_PROVIDER" envDefault:"docker"`
	AnthropicAPIKey string `env:"ANTHROPIC_API_KEY" envDefault:""`
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
