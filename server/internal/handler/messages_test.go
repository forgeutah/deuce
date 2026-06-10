package handler

import "testing"

// R5: server-side @deuce mention detection. The left guard keeps email
// addresses from triggering; the trailing boundary keeps prefix near-misses
// from triggering; matching is case-insensitive.
func TestDeuceMentionRE(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"@deuce do x", true},
		{"@Deuce please review", true},
		{"hey @deuce!", true},
		{"(@deuce) in parens", true},
		{"prefix @DEUCE suffix", true},
		{"@deuce's task", true},
		{"clint@deuce.dev mailed me", false},
		{"@deucebot is not deuce", false},
		{"deuce without the at-sign", false},
		{"no mention at all", false},
		{"", false},
	}
	for _, c := range cases {
		if got := deuceMentionRE.MatchString(c.content); got != c.want {
			t.Errorf("deuceMentionRE.MatchString(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

// R6: /stop is an exact match — "@deuce make the flicker stop" must enqueue
// work, not cancel it.
func TestStopIsExactMatchOnly(t *testing.T) {
	if !isStopCommand("/stop") || !isStopCommand("  /stop  ") {
		t.Error("exact /stop (with surrounding whitespace) must cancel")
	}
	for _, content := range []string{"@deuce make the flicker stop", "please stop", "/stop now", "stop"} {
		if isStopCommand(content) {
			t.Errorf("%q must NOT be treated as /stop", content)
		}
	}
}
