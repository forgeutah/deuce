package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port           int    `env:"PORT" envDefault:"8080"`
	DatabaseURL    string `env:"DATABASE_URL" envDefault:"postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable"`
	UserID         string `env:"DEUCE_USER_ID" envDefault:"10000000-0000-0000-0000-000000000001"`
	GitHubToken    string `env:"GITHUB_TOKEN" envDefault:""`
	DevPodBin      string `env:"DEVPOD_BIN" envDefault:"devpod"`
	DevPodProvider string `env:"DEVPOD_PROVIDER" envDefault:""`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
