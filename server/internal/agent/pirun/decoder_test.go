package pirun

import (
	"bytes"
	"encoding/json"
	"log/slog"
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

// captureLogs redirects the default slog logger into a buffer for the duration
// of a test and returns the accumulated output.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf.String
}

// TestDecodeExtensionError: Pi's extension_error is the only visibility Deuce
// has into a broken extension. It decodes to its own kind, is logged with the
// context needed to identify it, and is NOT forwarded to the runtime — a
// load-time failure has no task to attach to (R9).
func TestDecodeExtensionError(t *testing.T) {
	f := loadUIFixture(t)
	line := jsonlLine(t, f.ExtensionError.Line)

	ev, err := Decode(line)
	if err != nil {
		t.Fatalf("decode extension_error: %v", err)
	}
	if ev.Kind != KindExtensionError {
		t.Fatalf("kind = %q, want %q — ignoring it is why a broken extension is invisible", ev.Kind, KindExtensionError)
	}
	if ev.ExtensionPath != "/home/vscode/.pi/agent/extensions/ask-user.ts" ||
		ev.ExtensionEvent != "tool_call" ||
		ev.ErrorText != "TypeError: ui.select is not a function" {
		t.Errorf("extension error decoded as %+v", ev)
	}

	logs := captureLogs(t)
	var forwarded []Event
	in := strings.Join([]string{`{"type":"agent_start"}`, string(line), `{"type":"agent_end"}`}, "\n")
	if err := DecodeStream(strings.NewReader(in), func(ev Event) { forwarded = append(forwarded, ev) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	for _, ev := range forwarded {
		if ev.Kind == KindExtensionError {
			t.Errorf("extension error was forwarded to the runtime; it must be logged in the decoder instead")
		}
	}
	if len(forwarded) != 2 {
		t.Errorf("forwarded %d events, want the 2 surrounding lifecycle events", len(forwarded))
	}
	out := logs()
	for _, want := range []string{"extension error", "ask-user.ts", "tool_call", "ui.select is not a function"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("extension error must be logged at warn, got:\n%s", out)
	}
}

// TestDecodeStreamMalformedLineNamesType covers R8: a dropped line must name
// what it was, or a future protocol break is undiagnosable from a running
// server.
func TestDecodeStreamMalformedLineNamesType(t *testing.T) {
	logs := captureLogs(t)
	// Valid JSON, recognized type, but the payload does not fit the arm.
	in := strings.Join([]string{
		`{"type":"agent_start"}`,
		`{"type":"tool_execution_start","toolCallId":42,"toolName":"bash"}`,
		`{"type":"agent_end"}`,
	}, "\n")
	var kinds []EventKind
	if err := DecodeStream(strings.NewReader(in), func(ev Event) { kinds = append(kinds, ev.Kind) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if len(kinds) != 2 || kinds[0] != KindRunStarted || kinds[1] != KindRunCompleted {
		t.Errorf("emitted kinds = %v, want the stream to continue past the bad line", kinds)
	}
	out := logs()
	if !strings.Contains(out, "skipping malformed event line") || !strings.Contains(out, "type=tool_execution_start") {
		t.Errorf("malformed-line warning must name the event type, got:\n%s", out)
	}
}

// TestDecodeStreamUnknownUIMethodLogged covers R8 for the UI path: a dialog
// method Deuce cannot classify is reported, not silently discarded.
func TestDecodeStreamUnknownUIMethodLogged(t *testing.T) {
	f := loadUIFixture(t)
	logs := captureLogs(t)
	var got []Event
	if err := DecodeStream(strings.NewReader(string(f.offContract(t, "unknownMethod"))), func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("forwarded %+v, want an unknown UI method to reach the runtime as nothing", got)
	}
	out := logs()
	if !strings.Contains(out, "unknown extension UI method") || !strings.Contains(out, "someFutureDialog") {
		t.Errorf("unknown UI method must be logged with its method name, got:\n%s", out)
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("unknown UI method must be logged at warn, got:\n%s", out)
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

// --- Pi extension-UI contract fixture ---------------------------------------
//
// These assertions are derived from Pi's published RpcExtensionUIRequest union
// (testdata/pi-ui-protocol.json), not from what the decoder happens to accept.
// The previous fixtures here fed the decoder the exact shape the decoder
// guessed, so they stayed green while the product failed (KTD6).

type uiFixtureEntry struct {
	Name        string          `json:"name"`
	Method      string          `json:"method"`
	Blocking    bool            `json:"blocking"`
	ResponseArm string          `json:"responseArm"`
	Line        json.RawMessage `json:"line"`
}

type uiFixture struct {
	Package        string                    `json:"package"`
	PiVersion      string                    `json:"piVersion"`
	DerivedFrom    []string                  `json:"derivedFrom"`
	Requests       []uiFixtureEntry          `json:"requests"`
	Responses      map[string]uiFixtureEntry `json:"responses"`
	ExtensionError uiFixtureEntry            `json:"extensionError"`
	OffContract    map[string]uiFixtureEntry `json:"offContract"`
}

func loadUIFixture(t *testing.T) uiFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pi-ui-protocol.json"))
	if err != nil {
		t.Fatalf("read pi-ui-protocol fixture: %v", err)
	}
	var f uiFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse pi-ui-protocol fixture: %v", err)
	}
	return f
}

func (f uiFixture) request(t *testing.T, name string) []byte {
	t.Helper()
	for _, r := range f.Requests {
		if r.Name == name {
			return jsonlLine(t, r.Line)
		}
	}
	t.Fatalf("fixture has no request named %q", name)
	return nil
}

func (f uiFixture) offContract(t *testing.T, name string) []byte {
	t.Helper()
	e, ok := f.OffContract[name]
	if !ok {
		t.Fatalf("fixture has no offContract entry named %q", name)
	}
	return jsonlLine(t, e.Line)
}

// jsonlLine flattens a pretty-printed fixture object into the single physical
// line Pi's LF-framed JSONL stream would carry.
func jsonlLine(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact fixture line: %v", err)
	}
	return buf.Bytes()
}

// TestUIFixtureRecordsPiVersion: a fixture that cannot be traced back to a
// specific published Pi is not a contract, it is another assumption.
func TestUIFixtureRecordsPiVersion(t *testing.T) {
	f := loadUIFixture(t)
	if f.PiVersion == "" || f.Package == "" {
		t.Errorf("fixture must record the package and version it was transcribed from, got %q %q", f.Package, f.PiVersion)
	}
	if len(f.DerivedFrom) == 0 {
		t.Error("fixture must record which published files it was derived from")
	}
}

// TestDecodeUIRequestContract asserts one decode outcome for every arm of Pi's
// published request union, and fails if the fixture grows an arm with no
// expectation (completeness against the fixture, KTD6).
func TestDecodeUIRequestContract(t *testing.T) {
	f := loadUIFixture(t)

	type want struct {
		kind        EventKind
		requestKind string
		requestID   string
		prompt      string
		options     []string
	}
	// Blocking arms raise a pending question; the five fire-and-forget arms are
	// ignored at the decoder so they can never wedge a task (KTD4 / R3).
	expected := map[string]want{
		"select": {
			kind: KindAwaitingInput, requestKind: "select", requestID: "ui-select-1",
			prompt: "Which framework should I use?", options: []string{"React", "Vue", "Svelte"},
		},
		"confirm": {
			kind: KindAwaitingInput, requestKind: "confirm", requestID: "ui-confirm-1",
			prompt: "Force-push to main?\n\nThis rewrites remote history.",
		},
		"confirm_empty_message": {
			kind: KindAwaitingInput, requestKind: "confirm", requestID: "ui-confirm-2",
			prompt: "Should I delete the stale branches?",
		},
		"input": {
			kind: KindAwaitingInput, requestKind: "input", requestID: "ui-input-1",
			prompt: "Which environment should I deploy to?",
		},
		// editor has no frontend QuestionKind of its own — it rides the existing
		// input control rather than putting an undeclared kind on the wire.
		"editor": {
			kind: KindAwaitingInput, requestKind: "input", requestID: "ui-editor-1",
			prompt: "Edit the release notes before I publish them",
		},
		"notify":          {kind: KindIgnore},
		"setStatus":       {kind: KindIgnore},
		"setWidget":       {kind: KindIgnore},
		"setTitle":        {kind: KindIgnore},
		"set_editor_text": {kind: KindIgnore},
	}

	if len(expected) != len(f.Requests) {
		t.Errorf("fixture has %d request arms but %d have asserted outcomes — every arm needs one", len(f.Requests), len(expected))
	}
	for _, entry := range f.Requests {
		w, ok := expected[entry.Name]
		if !ok {
			t.Errorf("fixture arm %q has no asserted decode outcome", entry.Name)
			continue
		}
		t.Run(entry.Name, func(t *testing.T) {
			ev, err := Decode(entry.Line)
			if err != nil {
				t.Fatalf("decode %s: %v", entry.Name, err)
			}
			if ev.Kind != w.kind {
				t.Fatalf("kind = %q, want %q (decoded %+v)", ev.Kind, w.kind, ev)
			}
			if w.kind != KindAwaitingInput {
				return
			}
			if ev.RequestKind != w.requestKind {
				t.Errorf("requestKind = %q, want %q", ev.RequestKind, w.requestKind)
			}
			if ev.RequestID != w.requestID {
				t.Errorf("requestID = %q, want %q", ev.RequestID, w.requestID)
			}
			if ev.Prompt != w.prompt {
				t.Errorf("prompt = %q, want %q", ev.Prompt, w.prompt)
			}
			if !sameStrings(ev.Options, w.options) {
				t.Errorf("options = %v, want %v", ev.Options, w.options)
			}
		})
	}
}

// TestDecodeUIRequestBareStringOptions covers the version-skew case: a stale
// prebuild image runs the pre-fix extension, which puts the question in the
// argument slot Pi spreads into `options`. The question text is recoverable
// only from that string — deriving the prompt from `title` yields the constant
// boilerplate and discards the question (R2).
func TestDecodeUIRequestBareStringOptions(t *testing.T) {
	f := loadUIFixture(t)
	ev, err := Decode(f.offContract(t, "selectOptionsAsBareString"))
	if err != nil {
		t.Fatalf("bare-string options must not error: %v", err)
	}
	if ev.Kind != KindAwaitingInput {
		t.Fatalf("kind = %q, want %q — losing the line is what turned a mistyped argument into a ten-minute hang", ev.Kind, KindAwaitingInput)
	}
	if ev.Prompt != "Which framework should I use?" {
		t.Errorf("prompt = %q, want the question carried in options", ev.Prompt)
	}
	if ev.RequestKind != "input" {
		t.Errorf("requestKind = %q, want input — with no option labels the request degrades to free text", ev.RequestKind)
	}
	if len(ev.Options) != 0 {
		t.Errorf("options = %v, want none", ev.Options)
	}
	if ev.RequestID != "ui-skew-1" {
		t.Errorf("requestID = %q, want ui-skew-1", ev.RequestID)
	}
}

// TestDecodeUIRequestUnknownMethod: a dialog method a future Pi adds must not
// raise a pending question Deuce cannot render or answer.
func TestDecodeUIRequestUnknownMethod(t *testing.T) {
	f := loadUIFixture(t)
	ev, err := Decode(f.offContract(t, "unknownMethod"))
	if err != nil {
		t.Fatalf("unknown method must not error: %v", err)
	}
	if ev.Kind == KindAwaitingInput {
		t.Errorf("unknown UI method decoded as %+v, want no pending question", ev)
	}
}

// TestDecodeStreamNotificationDoesNotInterrupt covers AE4: a fire-and-forget
// notification riding mid-stream leaves the surrounding events untouched and
// raises no pending question.
func TestDecodeStreamNotificationDoesNotInterrupt(t *testing.T) {
	f := loadUIFixture(t)
	lines := []string{
		`{"type":"agent_start"}`,
		string(f.request(t, "notify")),
		string(f.request(t, "setStatus")),
		string(f.request(t, "select")),
		`{"type":"agent_end"}`,
	}
	var kinds []EventKind
	if err := DecodeStream(strings.NewReader(strings.Join(lines, "\n")), func(ev Event) { kinds = append(kinds, ev.Kind) }); err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	wantKinds := []EventKind{KindRunStarted, KindAwaitingInput, KindRunCompleted}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("emitted kinds = %v, want %v", kinds, wantKinds)
	}
	for i := range wantKinds {
		if kinds[i] != wantKinds[i] {
			t.Fatalf("emitted kinds = %v, want %v", kinds, wantKinds)
		}
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNormalizeTool(t *testing.T) {
	cases := map[string]string{
		"bash": "Bash", "read": "Read", "write": "Write", "edit": "Edit",
		"grep": "Grep", "BASH": "Bash", "custom_tool": "Custom_tool", "": "",
		"ask_user": "Ask", "Ask_user": "Ask",
	}
	for in, want := range cases {
		if got := normalizeTool(in); got != want {
			t.Errorf("normalizeTool(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDecodeAskUserToolStart pins the no-JSON guarantee on the action-log path
// (R9): an ask_user tool call surfaces the question text as its arg, never the
// args object dumped as JSON.
func TestDecodeAskUserToolStart(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "question only",
			line: `{"type":"tool_execution_start","toolCallId":"t1","toolName":"ask_user","args":{"question":"Which file?"}}`,
			want: "Which file?",
		},
		{
			name: "question with kind and options",
			line: `{"type":"tool_execution_start","toolCallId":"t2","toolName":"ask_user","args":{"question":"Which framework?","kind":"select","options":["React","Vue"]}}`,
			want: "Which framework?",
		},
		{
			name: "JSON-ish question text passes through verbatim",
			line: `{"type":"tool_execution_start","toolCallId":"t3","toolName":"ask_user","args":{"question":"Use {\"mode\":\"strict\"} here?"}}`,
			want: `Use {"mode":"strict"} here?`,
		},
		{
			name: "missing question degrades to placeholder",
			line: `{"type":"tool_execution_start","toolCallId":"t4","toolName":"ask_user","args":{"kind":"confirm"}}`,
			want: askUserPlaceholder,
		},
		{
			name: "non-string question degrades to placeholder",
			line: `{"type":"tool_execution_start","toolCallId":"t5","toolName":"ask_user","args":{"question":{"nested":true}}}`,
			want: askUserPlaceholder,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := Decode([]byte(tc.line))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if ev.Kind != KindToolStarted || ev.Tool != "Ask" {
				t.Fatalf("decoded as kind=%q tool=%q, want tool_started/Ask", ev.Kind, ev.Tool)
			}
			if ev.Arg != tc.want {
				t.Errorf("arg = %q, want %q", ev.Arg, tc.want)
			}
			if strings.Contains(ev.Arg, `"question"`) {
				t.Errorf("arg leaked raw args JSON: %q", ev.Arg)
			}
		})
	}
}

// TestDecodeAskUserToolEnd: the tool result (the user's answer) rides through
// unchanged under the normalized name.
func TestDecodeAskUserToolEnd(t *testing.T) {
	ev, err := Decode([]byte(`{"type":"tool_execution_end","toolCallId":"t1","toolName":"ask_user","isError":false,"result":{"content":[{"type":"text","text":"Vue"}]}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Kind != KindToolCompleted || ev.Tool != "Ask" || ev.Output != "Vue" || ev.IsError {
		t.Errorf("decoded as %+v, want completed Ask with output Vue", ev)
	}
}

// TestDecodeOtherToolsKeepJSONFallback: the generic compact-JSON arg fallback
// is unchanged for tools that aren't question-bearing.
func TestDecodeOtherToolsKeepJSONFallback(t *testing.T) {
	ev, err := Decode([]byte(`{"type":"tool_execution_start","toolCallId":"t9","toolName":"custom_tool","args":{"question":"not special here","foo":1}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Tool != "Custom_tool" {
		t.Fatalf("tool = %q, want Custom_tool", ev.Tool)
	}
	if !strings.Contains(ev.Arg, `"foo":1`) {
		t.Errorf("generic fallback arg = %q, want compact JSON of all args", ev.Arg)
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

	// Only the envelope is asserted here. The old assertion pinned a `response`
	// field that no arm of Pi's published RpcExtensionUIResponse contains — Pi
	// accepts and discards it. The per-arm assertions belong with the unit that
	// builds the arms (U3), against testdata/pi-ui-protocol.json.
	b, _ = Marshal(ExtensionUIResponse{ID: "ui-7"})
	if !strings.Contains(string(b), `"type":"extension_ui_response"`) || !strings.Contains(string(b), `"id":"ui-7"`) {
		t.Errorf("marshaled ui response = %s", string(b))
	}
}
