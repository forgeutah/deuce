package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The deuce identity constants cross three layers (Go, the migration SQL, and
// the frontend TS constant) that no compiler links together — a typo'd nibble
// in any copy silently breaks message authorship and the chat visibility
// filter. This turns the "MUST match" comment contract into a checked one.
func TestDeuceIdentityConstantsMatchAcrossLayers(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")

	cases := []struct {
		path     string
		mustHave []string
	}{
		{
			path:     filepath.Join(repoRoot, "src", "lib", "deuce.ts"),
			mustHave: []string{DeuceAgentID, `name: "` + DeuceAgentName + `"`},
		},
		{
			path:     filepath.Join(repoRoot, "server", "internal", "db", "migrations", "013_single_deuce_agent.sql"),
			mustHave: []string{DeuceAgentID},
		},
	}
	for _, c := range cases {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read %s: %v", c.path, err)
		}
		for _, want := range c.mustHave {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s does not contain %q — the deuce identity constants have drifted", c.path, want)
			}
		}
	}
}
