package agent

import "testing"

func TestSanitizeNarratedQuestion(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{
			// AE3: the canonical leak — a bare narrated call posts as the question.
			name:  "bare call",
			reply: `Ask_user({"question":"What file should I create?"})`,
			want:  "What file should I create?",
		},
		{
			// Lowercase tool name, the literal extension name.
			name:  "lowercase call",
			reply: `ask_user({"question": "Which framework?"})`,
			want:  "Which framework?",
		},
		{
			// Escaped newlines in the question are decoded, not shown raw.
			name:  "escaped multiline question",
			reply: `Ask_user({"question":"What file would you like me to create? Please provide:\n1. The filename\n2. The content"})`,
			want:  "What file would you like me to create? Please provide:\n1. The filename\n2. The content",
		},
		{
			// Leading prose is preserved; only the call shape is rewritten.
			name:  "prose then call",
			reply: `Sure — let me check. ask_user({"question":"Which env?"})`,
			want:  "Sure — let me check. Which env?",
		},
		{
			// AE4: truncated call still yields the question (closed string), no JSON.
			name:  "truncated but quoted",
			reply: `ask_user({"question":"What file?"`,
			want:  "What file?",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeNarratedQuestion(tc.reply); got != tc.want {
				t.Errorf("sanitize(%q) = %q, want %q", tc.reply, got, tc.want)
			}
		})
	}
}

func TestSanitizeNarratedQuestionFloor(t *testing.T) {
	// AE4: a garbled call with no recoverable question degrades to readable
	// prose, never a JSON fragment.
	got := sanitizeNarratedQuestion(`ask_user({"questi`)
	if got != malformedQuestionFloor {
		t.Errorf("garbled call = %q, want the readable floor", got)
	}
	if containsJSONFragment(got) {
		t.Errorf("floor should not contain a JSON fragment: %q", got)
	}
}

func TestSanitizeNarratedQuestionPassthrough(t *testing.T) {
	cases := []string{
		"",
		"(The agent finished without a text response.)",
		"I considered using ask_user to confirm, but proceeded with the default.",
		"Here is your summary: all tests pass and the build is green.",
	}
	for _, reply := range cases {
		if got := sanitizeNarratedQuestion(reply); got != reply {
			t.Errorf("passthrough reply changed: sanitize(%q) = %q", reply, got)
		}
	}
}

func containsJSONFragment(s string) bool {
	return len(s) > 0 && (s[0] == '{' || s[len(s)-1] == '}')
}
