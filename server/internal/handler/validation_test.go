package handler

import (
	"strings"
	"testing"
)

func TestValidateWorkspaceID_Accepts(t *testing.T) {
	for _, name := range []string{
		"a",
		"auth-module",
		"api_rate_limiting",
		"A1B2C3",
		"abc123",
		"workspace-name-with-dashes",
		strings.Repeat("a", 64),
	} {
		if err := validateWorkspaceID(name); err != nil {
			t.Errorf("expected %q to be accepted, got: %v", name, err)
		}
	}
}

func TestValidateWorkspaceID_Rejects(t *testing.T) {
	for _, name := range []string{
		"",
		"-leading-dash",   // would be parsed as a flag by devpod
		"--force",         // flag-shaped
		"name with space",
		"name/with/slash",
		"name:with:colon",
		"name;rm -rf /",   // shell-meta (defense in depth)
		"name\nrunfile",
		"name$(hostile)",
		strings.Repeat("a", 65), // too long
	} {
		if err := validateWorkspaceID(name); err == nil {
			t.Errorf("expected %q to be REJECTED but validation passed", name)
		}
	}
}

func TestValidateRepoURL_AcceptsEmpty(t *testing.T) {
	// Empty repoURL is allowed — sessions without one skip devpod creation
	// in the existing CreateSession guard.
	if err := validateRepoURL(""); err != nil {
		t.Errorf("expected empty to be accepted, got: %v", err)
	}
}

func TestValidateRepoURL_AcceptsValidSchemes(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/forgeutah/deuce",
		"https://github.com/forgeutah/deuce.git",
		"ssh://git@github.com/forgeutah/deuce.git",
		"git@github.com:forgeutah/deuce.git",
		"git@gitlab.example.com:team/project.git",
	} {
		if err := validateRepoURL(raw); err != nil {
			t.Errorf("expected %q to be accepted, got: %v", raw, err)
		}
	}
}

func TestValidateRepoURL_RejectsBadSchemes(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"/etc/passwd",
		"../../relative/path",
		"http://github.com/forgeutah/deuce", // http not allowed
		"ftp://example.com/repo.git",
		"javascript:alert(1)",
		"git@",                // no host
		"git@host",            // no colon
		"git@host:",           // no path
		"git@host with space:/repo",
	} {
		if err := validateRepoURL(raw); err == nil {
			t.Errorf("expected %q to be REJECTED but validation passed", raw)
		}
	}
}

func TestValidateRepoURL_RejectsEmbeddedCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://user:token@github.com/owner/repo.git",
		"https://user@github.com/owner/repo.git",
		"ssh://user:pass@github.com/owner/repo.git",
	} {
		if err := validateRepoURL(raw); err == nil {
			t.Errorf("expected %q to be REJECTED (embedded creds) but validation passed", raw)
		}
	}
}
