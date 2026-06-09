package pirun

import (
	"strings"
	"testing"
)

func TestPiLaunchSpecNoSystemPrompt(t *testing.T) {
	inner, env := piLaunchSpec("anthropic", "claude-haiku-4-5", "")
	if strings.Contains(inner, "append-system-prompt") {
		t.Errorf("no system prompt should not add the flag: %q", inner)
	}
	if len(env) != 0 {
		t.Errorf("no system prompt should add no env, got %v", env)
	}
	if !strings.Contains(inner, "pi --mode rpc --provider anthropic --model claude-haiku-4-5") {
		t.Errorf("unexpected base command: %q", inner)
	}
}

func TestPiLaunchSpecWithSystemPrompt(t *testing.T) {
	inner, env := piLaunchSpec("anthropic", "", "You are Coder. Use ask_user when blocked.")
	// The flag references the env var, not the literal text — so the prompt
	// never lands in the shell-interpreted command string.
	if !strings.Contains(inner, `--append-system-prompt "$DEUCE_SYSTEM_PROMPT"`) {
		t.Errorf("flag should reference the env var: %q", inner)
	}
	if len(env) != 1 || env[0] != "DEUCE_SYSTEM_PROMPT=You are Coder. Use ask_user when blocked." {
		t.Errorf("prompt should ride DEUCE_SYSTEM_PROMPT env, got %v", env)
	}
}

func TestPiLaunchSpecArbitraryContentRidesEnvUntouched(t *testing.T) {
	// Quotes, newlines, and shell metacharacters must pass through verbatim in
	// the env value (no shell quoting), and never appear in the command string.
	tricky := "Line 1\n\"quoted\" 'single' $HOME `backtick` && rm -rf"
	inner, env := piLaunchSpec("anthropic", "m", tricky)
	if len(env) != 1 || env[0] != "DEUCE_SYSTEM_PROMPT="+tricky {
		t.Errorf("tricky prompt should be carried verbatim in env, got %v", env)
	}
	if strings.Contains(inner, "rm -rf") || strings.Contains(inner, "backtick") {
		t.Errorf("prompt content must not leak into the command string: %q", inner)
	}
}
