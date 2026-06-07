package extension

import "testing"
import "strings"

func TestEmbedNonEmpty(t *testing.T) {
	if !strings.Contains(AskUser, "registerTool") || !strings.Contains(AskUser, "ask_user") {
		t.Fatalf("embedded extension missing expected content (len=%d)", len(AskUser))
	}
	if AskUserFilename != "ask-user.ts" {
		t.Errorf("filename=%q", AskUserFilename)
	}
}
