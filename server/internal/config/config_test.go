package config

import (
	"strings"
	"testing"
)

func TestValidate_DevDefaultsPass(t *testing.T) {
	cfg := &Config{
		AuthMode:         AuthModeDev,
		WSAllowedOrigins: "localhost:4000,localhost:8080",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("dev defaults should validate: %v", err)
	}
}

func TestValidate_UnknownAuthModeRejected(t *testing.T) {
	cfg := &Config{
		AuthMode:         "mystery",
		WSAllowedOrigins: "localhost:4000",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown auth mode")
	}
	if !strings.Contains(err.Error(), "DEUCE_AUTH_MODE") {
		t.Fatalf("error should reference the env var name: %v", err)
	}
}

func TestValidate_WildcardWSOriginRejected(t *testing.T) {
	cfg := &Config{
		AuthMode:         AuthModeDev,
		WSAllowedOrigins: "localhost:4000,*",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for wildcard ws origin")
	}
	if !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("error should mention wildcard hazard: %v", err)
	}
}

func TestValidate_ForgeProxyFullyConfiguredPasses(t *testing.T) {
	cfg := &Config{
		AuthMode:             AuthModeForgeProxy,
		ForgeProxySecret:     "topsecret",
		ForgeRequiredRole:    "member",
		ForgeContractVersion: 1,
		WSAllowedOrigins:     "deuce.example.com",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("forge-proxy with full config should validate: %v", err)
	}
}

func TestValidate_ForgeProxyMissingSecret(t *testing.T) {
	cfg := &Config{
		AuthMode:          AuthModeForgeProxy,
		ForgeRequiredRole: "member",
		WSAllowedOrigins:  "deuce.example.com",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "FORGE_PROXY_SECRET") {
		t.Fatalf("expected error naming FORGE_PROXY_SECRET, got: %v", err)
	}
}

func TestValidate_ForgeProxyMissingRole(t *testing.T) {
	cfg := &Config{
		AuthMode:         AuthModeForgeProxy,
		ForgeProxySecret: "topsecret",
		WSAllowedOrigins: "deuce.example.com",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "FORGE_REQUIRED_ROLE") {
		t.Fatalf("expected error naming FORGE_REQUIRED_ROLE, got: %v", err)
	}
}

func TestValidate_ForgeProxyMissingWSOrigins(t *testing.T) {
	cfg := &Config{
		AuthMode:          AuthModeForgeProxy,
		ForgeProxySecret:  "topsecret",
		ForgeRequiredRole: "member",
		WSAllowedOrigins:  "",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DEUCE_WS_ALLOWED_ORIGINS") {
		t.Fatalf("expected error naming DEUCE_WS_ALLOWED_ORIGINS, got: %v", err)
	}
}

func TestValidate_ForgeProxyAllMissingErrorLists(t *testing.T) {
	cfg := &Config{
		AuthMode:         AuthModeForgeProxy,
		WSAllowedOrigins: "",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected aggregate error for all missing fields")
	}
	for _, want := range []string{"FORGE_PROXY_SECRET", "FORGE_REQUIRED_ROLE", "DEUCE_WS_ALLOWED_ORIGINS"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("aggregate error should mention %s, got: %v", want, err)
		}
	}
}

func TestWSAllowedOriginList_TrimsAndDropsEmpty(t *testing.T) {
	cfg := &Config{WSAllowedOrigins: " localhost:4000 , , localhost:8080 "}
	got := cfg.WSAllowedOriginList()
	want := []string{"localhost:4000", "localhost:8080"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
