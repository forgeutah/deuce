package workspace

import (
	"testing"
)

// TestValidContainerName_AcceptsLegitimateNames confirms the regex accepts
// real Docker container names. DevPod typically names containers like
// "devpod-<workspace-id>".
func TestValidContainerName_AcceptsLegitimateNames(t *testing.T) {
	valid := []string{
		"devpod-abc123",
		"abc",
		"a",
		"deuce-postgres-1",
		"my_container.with-dots",
		"A1B2C3",
	}
	for _, name := range valid {
		if !validContainerName.MatchString(name) {
			t.Errorf("expected %q to be a valid container name", name)
		}
	}
}

// TestValidContainerName_RejectsHostileInputs locks in the defense-in-depth
// behavior — even if Docker's output format changes or a malicious label
// produces a payload, the regex blocks it from reaching `docker exec` argv.
func TestValidContainerName_RejectsHostileInputs(t *testing.T) {
	invalid := []string{
		"",                  // empty
		"-leading-dash",     // leading dash
		"--privileged",      // flag-shaped
		"-v/:/host",         // mount-flag shaped
		"name with space",   // shell-meta
		"name;rm -rf /",     // shell-meta
		"name`hostile`",     // shell-meta
		"name$(hostile)",    // shell-meta
		"name\nrunfile",     // embedded newline
		"a:b",               // colon
		"a/b",               // slash
	}
	for _, name := range invalid {
		if validContainerName.MatchString(name) {
			t.Errorf("expected %q to be REJECTED but regex matched", name)
		}
	}
}

// TestValidContainerName_TooLongRejected verifies the 255-char ceiling
// matches Docker's own naming rules.
func TestValidContainerName_TooLongRejected(t *testing.T) {
	tooLong := "a"
	for i := 0; i < 255; i++ {
		tooLong += "a"
	}
	if validContainerName.MatchString(tooLong) {
		t.Errorf("expected %d-char name to be rejected", len(tooLong))
	}
}
