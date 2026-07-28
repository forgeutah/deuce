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

func TestValidate_ForgeProxyLiteralGivesMigrationHint(t *testing.T) {
	cfg := &Config{
		AuthMode:         "forge-proxy",
		WSAllowedOrigins: "deuce.example.com",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected migration hint for legacy mode value")
	}
	// The hint must name the new mode value AND the new env-var prefix so
	// an operator copy-pasting an old env file sees both halves of the
	// rename in one error.
	for _, want := range []string{"forge-proxy is no longer supported", "DEUCE_AUTH_MODE=proxy", "DEUCE_PROXY_HEADER_"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("migration hint should mention %q, got: %v", want, err)
		}
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

// forgeStyleConfig returns a proxy-mode config wired up the way a real
// forge-proxy deployment would (CSV roles + secret + contract version).
func forgeStyleConfig() *Config {
	return &Config{
		AuthMode:                   AuthModeProxy,
		ProxyHeaderEmail:           "X-Forge-Email",
		ProxyHeaderName:            "X-Forge-Name",
		ProxyHeaderAvatar:          "X-Forge-Avatar",
		ProxyHeaderSecret:          "X-Forge-Proxy-Secret",
		ProxySecret:                "topsecret",
		ProxyHeaderContractVersion: "X-Forge-Contract-Version",
		ProxyContractVersion:       1,
		ProxyHeaderRoles:           "X-Forge-Roles",
		ProxyRolesFormat:           RolesFormatCSV,
		ProxyRequiredRole:          "member",
		WSAllowedOrigins:           "deuce.example.com",
	}
}

// tailscaleStyleConfig returns a proxy-mode config wired up the way a real
// Tailscale Serve deployment would (JSON-object roles, no secret, no
// contract version — the tailnet plus bind-to-loopback is the trust
// boundary).
func tailscaleStyleConfig() *Config {
	return &Config{
		AuthMode:          AuthModeProxy,
		ProxyHeaderEmail:  "Tailscale-User-Login",
		ProxyHeaderName:   "Tailscale-User-Name",
		ProxyHeaderAvatar: "Tailscale-User-Profile-Pic",
		ProxyHeaderRoles:  "Tailscale-App-Capabilities",
		ProxyRolesFormat:  RolesFormatJSONObject,
		ProxyRequiredRole: "example.com/cap/deuce/access",
		WSAllowedOrigins:  "deuce.example.com",
	}
}

func TestValidate_ProxyForgeStyleFullyConfiguredPasses(t *testing.T) {
	if err := forgeStyleConfig().Validate(); err != nil {
		t.Fatalf("forge-style proxy config should validate: %v", err)
	}
}

func TestValidate_ProxyTailscaleStyleFullyConfiguredPasses(t *testing.T) {
	if err := tailscaleStyleConfig().Validate(); err != nil {
		t.Fatalf("Tailscale-style proxy config should validate: %v", err)
	}
}

func TestValidate_ProxyEmptyAggregatesRequired(t *testing.T) {
	cfg := &Config{AuthMode: AuthModeProxy, WSAllowedOrigins: ""}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected aggregate error for bare proxy mode")
	}
	// Email and origins are required; name is optional (operator can let
	// users pick their display name via the welcome screen).
	for _, want := range []string{"DEUCE_PROXY_HEADER_EMAIL", "DEUCE_WS_ALLOWED_ORIGINS"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("aggregate error should mention %s, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "DEUCE_PROXY_HEADER_NAME") {
		t.Fatalf("name header is optional now; error should not require it: %v", err)
	}
}

func TestValidate_ProxyWithoutNameHeader_Passes(t *testing.T) {
	// exe.dev-style config: only email, no name, no roles, no secret.
	// The welcome screen handles name collection at the frontend.
	cfg := &Config{
		AuthMode:         AuthModeProxy,
		ProxyHeaderEmail: "X-ExeDev-Email",
		WSAllowedOrigins: "vmname.exe.xyz",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("proxy mode with only email header should validate: %v", err)
	}
}

func TestValidate_ProxySecretPairAsymmetric(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		secret    string
		wantMatch string
	}{
		{"header without value", "X-Forge-Proxy-Secret", "", "DEUCE_PROXY_HEADER_SECRET and DEUCE_PROXY_SECRET"},
		{"value without header", "", "topsecret", "DEUCE_PROXY_HEADER_SECRET and DEUCE_PROXY_SECRET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tailscaleStyleConfig()
			cfg.ProxyHeaderSecret = tc.header
			cfg.ProxySecret = tc.secret
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantMatch) {
				t.Fatalf("expected asymmetric-pair error mentioning %q, got: %v", tc.wantMatch, err)
			}
		})
	}
}

func TestValidate_ProxyContractPairAsymmetric(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		version   int
		wantMatch string
	}{
		{"header without version", "X-Forge-Contract-Version", 0, "DEUCE_PROXY_CONTRACT_VERSION must be > 0"},
		{"version without header", "", 1, "DEUCE_PROXY_HEADER_CONTRACT_VERSION must be set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tailscaleStyleConfig()
			cfg.ProxyHeaderContractVersion = tc.header
			cfg.ProxyContractVersion = tc.version
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantMatch) {
				t.Fatalf("expected contract-pair error mentioning %q, got: %v", tc.wantMatch, err)
			}
		})
	}
}

func TestValidate_ProxyRolesHeaderWithoutFormat(t *testing.T) {
	cfg := tailscaleStyleConfig()
	cfg.ProxyRolesFormat = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DEUCE_PROXY_ROLES_FORMAT") {
		t.Fatalf("expected error for missing roles format: %v", err)
	}
}

func TestValidate_ProxyRolesHeaderWithoutRequiredRole(t *testing.T) {
	cfg := tailscaleStyleConfig()
	cfg.ProxyRequiredRole = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DEUCE_PROXY_REQUIRED_ROLE") {
		t.Fatalf("expected error for missing required role: %v", err)
	}
}

func TestValidate_ProxyRolesUnknownFormat(t *testing.T) {
	cfg := tailscaleStyleConfig()
	cfg.ProxyRolesFormat = "xml"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `"xml" is invalid`) {
		t.Fatalf("expected invalid-format error: %v", err)
	}
}

func TestValidate_ProxyRolesValueWithoutHeader(t *testing.T) {
	cfg := tailscaleStyleConfig()
	cfg.ProxyHeaderRoles = ""
	// Leave RolesFormat + RequiredRole set; the validator must catch this
	// because silently ignoring them would mask a typo in the header env var.
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DEUCE_PROXY_HEADER_ROLES must be set") {
		t.Fatalf("expected error for orphaned roles config: %v", err)
	}
}

func TestValidate_ProxyHeaderCollision(t *testing.T) {
	cfg := forgeStyleConfig()
	cfg.ProxyHeaderName = cfg.ProxyHeaderEmail // intentional collision
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected collision error")
	}
	// Both env var names must appear so the operator can find the typo.
	for _, want := range []string{"DEUCE_PROXY_HEADER_EMAIL", "DEUCE_PROXY_HEADER_NAME", "both map to header"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("collision error should mention %q, got: %v", want, err)
		}
	}
}

func TestValidate_ProxyMalformedHeaderName(t *testing.T) {
	cases := []struct {
		name      string
		headerVal string
	}{
		{"space in name", "X-Forge Email"},
		{"newline in name", "X-Forge-Email\n"},
		{"empty after canonicalize", "  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := forgeStyleConfig()
			cfg.ProxyHeaderEmail = tc.headerVal
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "DEUCE_PROXY_HEADER_EMAIL") {
				t.Fatalf("expected malformed-header error: %v", err)
			}
		})
	}
}

func TestValidate_ProxyMissingWSOrigins(t *testing.T) {
	cfg := tailscaleStyleConfig()
	cfg.WSAllowedOrigins = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DEUCE_WS_ALLOWED_ORIGINS") {
		t.Fatalf("expected origins-required error: %v", err)
	}
}

func TestProxyCheckEnabledHelpers(t *testing.T) {
	forge := forgeStyleConfig()
	if !forge.ProxySecretCheckEnabled() {
		t.Error("forge-style config should report secret check enabled")
	}
	if !forge.ProxyContractCheckEnabled() {
		t.Error("forge-style config should report contract check enabled")
	}
	if !forge.ProxyRoleCheckEnabled() {
		t.Error("forge-style config should report role check enabled")
	}

	ts := tailscaleStyleConfig()
	if ts.ProxySecretCheckEnabled() {
		t.Error("Tailscale-style config should report secret check disabled")
	}
	if ts.ProxyContractCheckEnabled() {
		t.Error("Tailscale-style config should report contract check disabled")
	}
	if !ts.ProxyRoleCheckEnabled() {
		t.Error("Tailscale-style config has roles header → role check enabled")
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

func TestPrebuildEnabled(t *testing.T) {
	cfg := &Config{}
	if cfg.PrebuildEnabled() {
		t.Error("prebuild should be off by default — an empty repository keeps the original from-scratch path")
	}
	cfg.PrebuildRepository = "deuce-prebuild"
	if !cfg.PrebuildEnabled() {
		t.Error("prebuild should be on when a repository is configured")
	}
}

func TestValidate_PrebuildRepositoryAccepted(t *testing.T) {
	valid := []string{
		"",                                 // off
		"deuce-prebuild",                   // bare local name
		"ghcr.io/forgeutah/deuce-prebuild", // registry path
		"localhost:5000/deuce-prebuild",    // registry with port
		"deuce_prebuild",                   // underscore separator
	}
	for _, repo := range valid {
		cfg := &Config{
			AuthMode:           AuthModeDev,
			WSAllowedOrigins:   "localhost:4000",
			PrebuildRepository: repo,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("PrebuildRepository=%q should validate: %v", repo, err)
		}
	}
}

func TestValidate_PrebuildRepositoryRejected(t *testing.T) {
	// A tag here would collide with the devcontainer hash Deuce appends,
	// and the flag- and shell-shaped values must never reach docker argv.
	invalid := []string{
		"deuce-prebuild:latest",
		"Deuce-Prebuild",
		"--build-arg=evil",
		"repo with space",
		"repo;rm -rf /",
		"repo$(hostile)",
	}
	for _, repo := range invalid {
		cfg := &Config{
			AuthMode:           AuthModeDev,
			WSAllowedOrigins:   "localhost:4000",
			PrebuildRepository: repo,
		}
		err := cfg.Validate()
		if err == nil {
			t.Errorf("PrebuildRepository=%q should be rejected", repo)
			continue
		}
		if !strings.Contains(err.Error(), "DEUCE_PREBUILD_REPOSITORY") {
			t.Errorf("error for %q should name the env var: %v", repo, err)
		}
	}
}

func TestValidate_VSCodeCacheDirMustBeAbsolute(t *testing.T) {
	// A relative root would resolve against the server's working directory,
	// which differs between `make dev` and a deployed unit — the cache would
	// silently land somewhere different depending on how deuce was started.
	for _, dir := range []string{"relative/path", "./cache", "cache"} {
		cfg := &Config{
			AuthMode:         AuthModeDev,
			WSAllowedOrigins: "localhost:4000",
			VSCodeCacheDir:   dir,
		}
		err := cfg.Validate()
		if err == nil {
			t.Errorf("VSCodeCacheDir=%q should be rejected", dir)
			continue
		}
		if !strings.Contains(err.Error(), "DEUCE_VSCODE_SERVER_CACHE_DIR") {
			t.Errorf("error for %q should name the env var: %v", dir, err)
		}
	}

	for _, dir := range []string{"", "/var/lib/deuce/vscode"} {
		cfg := &Config{
			AuthMode:         AuthModeDev,
			WSAllowedOrigins: "localhost:4000",
			VSCodeCacheDir:   dir,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("VSCodeCacheDir=%q should validate: %v", dir, err)
		}
	}
}
