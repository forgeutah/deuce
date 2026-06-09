package pirun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeGoldenStream decodes the real Pi RPC stream captured during the U1
// spike (a bash tool run) and asserts the lifecycle the runtime depends on.
func TestDecodeGoldenStream(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "golden-stream.jsonl"))
	if err != nil {
		t.Fatalf("open golden stream: %v", err)
	}
	defer f.Close()

	var events []Event
	if err := DecodeStream(f, func(ev Event) { events = append(events, ev) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("expected several emitted events, got %d", len(events))
	}

	// First emitted is the command reply to our prompt; second is run start.
	if events[0].Kind != KindCommandReply || events[0].Command != "prompt" || !events[0].Success {
		t.Errorf("first event = %+v, want successful prompt command reply", events[0])
	}
	if events[1].Kind != KindRunStarted {
		t.Errorf("second event kind = %q, want %q", events[1].Kind, KindRunStarted)
	}
	if last := events[len(events)-1]; last.Kind != KindRunCompleted {
		t.Errorf("last event kind = %q, want %q", last.Kind, KindRunCompleted)
	}

	// Exactly one tool start and one tool end, correlated by toolCallId.
	var starts, ends []Event
	var reply strings.Builder
	for _, ev := range events {
		switch ev.Kind {
		case KindToolStarted:
			starts = append(starts, ev)
		case KindToolCompleted:
			ends = append(ends, ev)
		case KindAssistantText:
			reply.WriteString(ev.Text)
		}
	}
	if len(starts) != 1 || len(ends) != 1 {
		t.Fatalf("tool events: got %d starts, %d ends; want 1 and 1", len(starts), len(ends))
	}
	start, end := starts[0], ends[0]
	if start.ToolCallID == "" || start.ToolCallID != end.ToolCallID {
		t.Errorf("toolCallId mismatch: start=%q end=%q", start.ToolCallID, end.ToolCallID)
	}
	if start.Tool != "Bash" {
		t.Errorf("tool name = %q, want Bash", start.Tool)
	}
	if start.Arg != "cat sample.txt" {
		t.Errorf("tool arg = %q, want %q", start.Arg, "cat sample.txt")
	}
	if !strings.Contains(end.Output, "alpha") || !strings.Contains(end.Output, "charlie") {
		t.Errorf("tool output = %q, want it to contain the file contents", end.Output)
	}
	if end.IsError {
		t.Errorf("tool IsError = true, want false")
	}

	// The streamed reply text accumulates from text_delta events.
	wantReply := "The file `sample.txt` has **3 lines**:\n1. alpha\n2. bravo\n3. charlie"
	if got := reply.String(); got != wantReply {
		t.Errorf("accumulated reply = %q, want %q", got, wantReply)
	}
}

func TestDecodeResponseVsEvent(t *testing.T) {
	reply, err := Decode([]byte(`{"id":"r1","type":"response","command":"get_state","success":true}`))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if reply.Kind != KindCommandReply || reply.Command != "get_state" || reply.ReplyID != "r1" || !reply.Success {
		t.Errorf("response decoded as %+v", reply)
	}

	ev, err := Decode([]byte(`{"type":"agent_start"}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if ev.Kind != KindRunStarted {
		t.Errorf("agent_start kind = %q, want %q", ev.Kind, KindRunStarted)
	}
}

func TestDecodeUnknownTypeTolerated(t *testing.T) {
	ev, err := Decode([]byte(`{"type":"some_future_pi_event","foo":1}`))
	if err != nil {
		t.Fatalf("unknown type should not error, got %v", err)
	}
	if ev.Kind != KindUnknown || ev.RawType != "some_future_pi_event" {
		t.Errorf("unknown event decoded as %+v, want KindUnknown", ev)
	}
}

func TestDecodeMalformedLineErrors(t *testing.T) {
	if _, err := Decode([]byte(`{not valid json`)); err == nil {
		t.Error("expected error for malformed JSON line")
	}
}

func TestDecodeStreamSkipsBadLines(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"agent_start"}`,
		`{not json}`,                    // malformed → skipped
		`{"type":"some_unknown_event"}`, // unknown → skipped
		``,                              // blank → skipped
		`{"type":"agent_end"}`,
	}, "\n")
	var kinds []EventKind
	if err := DecodeStream(strings.NewReader(in), func(ev Event) { kinds = append(kinds, ev.Kind) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if len(kinds) != 2 || kinds[0] != KindRunStarted || kinds[1] != KindRunCompleted {
		t.Errorf("emitted kinds = %v, want [run_started run_completed]", kinds)
	}
}

func TestDecodeExtensionUIRequest(t *testing.T) {
	// extension_ui_request is the ask-user mechanism (KTD15); not in the golden
	// stream (no extension yet), so exercise the best-effort shape directly.
	ev, err := Decode([]byte(`{"type":"extension_ui_request","id":"ui-7","method":"input","params":{"prompt":"Which environment?"}}`))
	if err != nil {
		t.Fatalf("decode ui request: %v", err)
	}
	if ev.Kind != KindAwaitingInput {
		t.Fatalf("kind = %q, want %q", ev.Kind, KindAwaitingInput)
	}
	if ev.RequestID != "ui-7" || ev.RequestKind != "input" || ev.Prompt != "Which environment?" {
		t.Errorf("ui request decoded as %+v", ev)
	}
	if len(ev.Options) != 0 {
		t.Errorf("free-text request should carry no options, got %v", ev.Options)
	}
}

func TestDecodeExtensionUIRequestSelectOptions(t *testing.T) {
	// A select-kind request carries choice options; decode them best-effort
	// whether they ride top-level or under params.
	ev, err := Decode([]byte(`{"type":"extension_ui_request","id":"ui-9","kind":"select","prompt":"Which framework?","options":["React","Vue","Svelte"]}`))
	if err != nil {
		t.Fatalf("decode select request: %v", err)
	}
	if ev.Kind != KindAwaitingInput || ev.RequestKind != "select" {
		t.Fatalf("kind=%q requestKind=%q, want awaiting_input/select", ev.Kind, ev.RequestKind)
	}
	if got := ev.Options; len(got) != 3 || got[0] != "React" || got[2] != "Svelte" {
		t.Errorf("options = %v, want [React Vue Svelte]", got)
	}
}

func TestDecodeExtensionUIRequestParamsOptions(t *testing.T) {
	ev, err := Decode([]byte(`{"type":"extension_ui_request","id":"ui-10","method":"select","params":{"prompt":"Pick","options":["a","b"]}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ev.Options) != 2 || ev.Options[1] != "b" || ev.Prompt != "Pick" {
		t.Errorf("params-options request decoded as %+v", ev)
	}
}

func TestNormalizeTool(t *testing.T) {
	cases := map[string]string{
		"bash": "Bash", "read": "Read", "write": "Write", "edit": "Edit",
		"grep": "Grep", "BASH": "Bash", "custom_tool": "Custom_tool", "": "",
	}
	for in, want := range cases {
		if got := normalizeTool(in); got != want {
			t.Errorf("normalizeTool(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarshalCommandInjectsType(t *testing.T) {
	b, err := Marshal(Prompt{Message: "hi", ID: "r1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"prompt"`) || !strings.Contains(s, `"message":"hi"`) {
		t.Errorf("marshaled prompt = %s", s)
	}

	b, _ = Marshal(ExtensionUIResponse{ID: "ui-7", Response: "prod"})
	if !strings.Contains(string(b), `"type":"extension_ui_response"`) || !strings.Contains(string(b), `"id":"ui-7"`) {
		t.Errorf("marshaled ui response = %s", string(b))
	}
}
