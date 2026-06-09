package agent

import (
	"strings"
	"testing"
)

func TestJoinSystemPrompts(t *testing.T) {
	cases := []struct {
		name        string
		base, agent string
		want        string
	}{
		{"both empty", "", "", ""},
		{"base only", "BASE", "", "BASE"},
		{"agent only", "", "AGENT", "AGENT"},
		{"both", "BASE", "AGENT", "BASE\n\nAGENT"},
		{"trims whitespace", "  BASE \n", "\n AGENT  ", "BASE\n\nAGENT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinSystemPrompts(tc.base, tc.agent); got != tc.want {
				t.Errorf("joinSystemPrompts(%q, %q) = %q, want %q", tc.base, tc.agent, got, tc.want)
			}
		})
	}
}

func TestDefaultBaseSystemPromptMentionsAskUser(t *testing.T) {
	// The whole point of the global prompt is to steer agents to ask_user.
	if !strings.Contains(DefaultBaseSystemPrompt, "ask_user") {
		t.Error("DefaultBaseSystemPrompt must reference the ask_user tool")
	}
}
