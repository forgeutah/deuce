package pirun

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
)

// EventKind is the internal classification the runtime acts on. It is a small,
// stable vocabulary derived from Pi's larger event surface; many Pi events
// (message_start, turn_start, message_end, tool_execution_update, queue_update,
// …) carry no signal the runtime needs and decode to KindIgnore.
type EventKind string

const (
	// KindRunStarted maps from Pi "agent_start": the agent picked up the run.
	KindRunStarted EventKind = "run_started"
	// KindToolStarted maps from "tool_execution_start": a tool call began.
	KindToolStarted EventKind = "tool_started"
	// KindToolCompleted maps from "tool_execution_end": a tool call finished.
	KindToolCompleted EventKind = "tool_completed"
	// KindThinking maps from a "thinking_delta" assistant message update.
	KindThinking EventKind = "thinking"
	// KindAssistantText maps from a "text_delta" assistant message update — the
	// streamed reply text the runtime accumulates into the task's final reply.
	KindAssistantText EventKind = "assistant_text"
	// KindAwaitingInput maps from "extension_ui_request": the agent is blocked
	// on a human answer via the ask-user extension (KTD15).
	KindAwaitingInput EventKind = "awaiting_input"
	// KindExtensionError maps from "extension_error": an extension threw, at
	// load time or while handling an event. It is the only visibility Deuce has
	// into a broken extension, and it is logged rather than forwarded to the
	// runtime — a load-time error has no task to attach to (R9).
	KindExtensionError EventKind = "extension_error"
	// KindRunCompleted maps from "agent_end": the run finished.
	KindRunCompleted EventKind = "run_completed"
	// KindCommandReply maps from a "response" envelope (reply to a client command).
	KindCommandReply EventKind = "command_reply"
	// KindIgnore is a recognized Pi event the runtime intentionally drops.
	KindIgnore EventKind = "ignore"
	// KindUnknown is an unrecognized Pi event type — tolerated, never fatal
	// (KTD2). Callers may log it; the stream continues.
	KindUnknown EventKind = "unknown"
)

// Event is the decoder's normalized output. Only the fields relevant to a given
// Kind are populated.
type Event struct {
	Kind    EventKind
	RawType string // the original Pi "type" (or "command" for replies)

	// Tool events (KindToolStarted / KindToolCompleted).
	ToolCallID string
	Tool       string // normalized display name (Bash, Read, …)
	Arg        string // headline argument (command/path/…)
	Output     string // tool result text (KindToolCompleted)
	IsError    bool   // tool result error flag (KindToolCompleted)

	// Text events (KindThinking / KindAssistantText): the incremental delta.
	Text string

	// Awaiting-input (KindAwaitingInput).
	RequestID   string
	RequestKind string // select / confirm / input / editor
	Prompt      string
	Options     []string // choice labels for a select request (empty otherwise)

	// Command reply (KindCommandReply).
	Command string
	ReplyID string
	Success bool

	// Extension error (KindExtensionError).
	ExtensionPath  string // the extension file that threw
	ExtensionEvent string // the Pi event it was handling ("tool_call", …)
	ErrorText      string
}

// envelope is the minimal shared shape used to classify a line before decoding
// the type-specific fields.
type envelope struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	ID      string `json:"id"`
	Success *bool  `json:"success"`
}

// Decode parses a single JSONL line from Pi's stdout. It returns an error only
// for malformed JSON; unrecognized event types are returned as KindUnknown so
// the caller can log and continue (schema-drift tolerance, KTD2).
func Decode(line []byte) (Event, error) {
	line = trimLine(line)
	if len(line) == 0 {
		return Event{Kind: KindIgnore}, nil
	}

	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Event{Kind: KindUnknown}, err
	}

	// A "response" envelope is a reply to a client command, not a lifecycle
	// event. Pi marks these with type:"response" and a "command" field.
	if env.Type == "response" {
		ev := Event{Kind: KindCommandReply, RawType: "response", Command: env.Command, ReplyID: env.ID}
		if env.Success != nil {
			ev.Success = *env.Success
		}
		return ev, nil
	}

	switch env.Type {
	case "agent_start":
		return Event{Kind: KindRunStarted, RawType: env.Type}, nil
	case "agent_end":
		return Event{Kind: KindRunCompleted, RawType: env.Type}, nil
	case "tool_execution_start":
		return decodeToolStart(line)
	case "tool_execution_end":
		return decodeToolEnd(line)
	case "message_update":
		return decodeMessageUpdate(line)
	case "extension_ui_request":
		return decodeUIRequest(line)
	case "extension_error":
		return decodeExtensionError(line)
	case "message_start", "message_end", "turn_start", "turn_end",
		"tool_execution_update", "queue_update",
		"compaction_start", "compaction_end",
		"auto_retry_start", "auto_retry_end":
		// Recognized but not acted on by the runtime.
		return Event{Kind: KindIgnore, RawType: env.Type}, nil
	default:
		return Event{Kind: KindUnknown, RawType: env.Type}, nil
	}
}

func decodeToolStart(line []byte) (Event, error) {
	var p struct {
		ToolCallID string                     `json:"toolCallId"`
		ToolName   string                     `json:"toolName"`
		Args       map[string]json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "tool_execution_start"}, err
	}
	// ask_user is question-bearing: surface the question text, never the args
	// JSON (R9 — a question must not render as a raw tool-call string).
	arg := headlineArg(p.Args)
	if strings.EqualFold(p.ToolName, "ask_user") {
		arg = askUserHeadline(p.Args)
	}
	return Event{
		Kind:       KindToolStarted,
		RawType:    "tool_execution_start",
		ToolCallID: p.ToolCallID,
		Tool:       normalizeTool(p.ToolName),
		Arg:        arg,
	}, nil
}

func decodeToolEnd(line []byte) (Event, error) {
	var p struct {
		ToolCallID string `json:"toolCallId"`
		ToolName   string `json:"toolName"`
		IsError    bool   `json:"isError"`
		Result     struct {
			Content []contentBlock `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "tool_execution_end"}, err
	}
	return Event{
		Kind:       KindToolCompleted,
		RawType:    "tool_execution_end",
		ToolCallID: p.ToolCallID,
		Tool:       normalizeTool(p.ToolName),
		Output:     joinText(p.Result.Content),
		IsError:    p.IsError,
	}, nil
}

func decodeMessageUpdate(line []byte) (Event, error) {
	var p struct {
		Event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "message_update"}, err
	}
	switch p.Event.Type {
	case "text_delta":
		return Event{Kind: KindAssistantText, RawType: "message_update", Text: p.Event.Delta}, nil
	case "thinking_delta":
		return Event{Kind: KindThinking, RawType: "message_update", Text: p.Event.Delta}, nil
	default:
		// start/end/toolcall_* sub-events carry no incremental signal the
		// runtime needs — tool calls are tracked via tool_execution_* events.
		return Event{Kind: KindIgnore, RawType: "message_update"}, nil
	}
}

// decodeExtensionError decodes Pi's "extension_error" event (docs/rpc.md):
// extensionPath, the event being handled, and the error text.
func decodeExtensionError(line []byte) (Event, error) {
	var p struct {
		ExtensionPath string `json:"extensionPath"`
		Event         string `json:"event"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "extension_error"}, err
	}
	return Event{
		Kind:           KindExtensionError,
		RawType:        "extension_error",
		ExtensionPath:  p.ExtensionPath,
		ExtensionEvent: p.Event,
		ErrorText:      p.Error,
	}, nil
}

// Pi's extension UI methods (RpcExtensionUIRequest, nine arms keyed on
// "method"). The first four block on a client response; the rest are
// fire-and-forget and must never raise a pending question (KTD4 / R3).
const (
	uiMethodSelect        = "select"
	uiMethodConfirm       = "confirm"
	uiMethodInput         = "input"
	uiMethodEditor        = "editor"
	uiMethodNotify        = "notify"
	uiMethodSetStatus     = "setStatus"
	uiMethodSetWidget     = "setWidget"
	uiMethodSetTitle      = "setTitle"
	uiMethodSetEditorText = "set_editor_text"
)

// decodeUIRequest decodes an extension_ui_request against Pi's published
// RpcExtensionUIRequest union. The union is FLAT: every field sits at the top
// level next to type/id/method, and no arm nests anything under "params". The
// contract is transcribed arm-by-arm in testdata/pi-ui-protocol.json, which
// records the Pi version it came from.
//
// Prompt text per arm (the union carries no single "prompt" field):
//
//	select  → title            confirm → title + message
//	input   → title            editor  → title
//
// placeholder and prefill are input hints, not the question, so they are not
// folded into the prompt.
func decodeUIRequest(line []byte) (Event, error) {
	var p struct {
		ID      string          `json:"id"`
		Method  string          `json:"method"`
		Title   string          `json:"title"`
		Message string          `json:"message"`
		Options json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "extension_ui_request"}, err
	}

	ev := Event{
		Kind:        KindAwaitingInput,
		RawType:     "extension_ui_request",
		RequestID:   p.ID,
		RequestKind: p.Method,
		Prompt:      p.Title,
	}

	switch p.Method {
	case uiMethodSelect:
		labels, question, ok := decodeUIOptions(p.Options)
		switch {
		case ok && len(labels) > 0:
			ev.Options = labels
		case question != "":
			// Version skew: a pre-fix extension called select(title, question,
			// options), so Pi spread the question into the options slot. The
			// question text exists only there — falling back to the title would
			// surface the extension's boilerplate and discard the question (R2).
			// Degrade to free text: there are no option labels to render.
			ev.RequestKind = uiMethodInput
			ev.Prompt = question
		default:
			// A select with no usable labels can only be answered as free text.
			ev.RequestKind = uiMethodInput
		}
	case uiMethodConfirm:
		ev.Prompt = joinPrompt(p.Title, p.Message)
	case uiMethodInput:
		// Prompt is the title, already set.
	case uiMethodEditor:
		// The drawer has no editor control and the frontend QuestionKind union
		// has no "editor" member — an editor dialog is answered through the
		// composer, so it rides the existing input kind rather than putting an
		// undeclared value on the wire.
		ev.RequestKind = uiMethodInput
	case uiMethodNotify, uiMethodSetStatus, uiMethodSetWidget, uiMethodSetTitle, uiMethodSetEditorText:
		// Fire-and-forget: carries an id but expects no response. Answering one
		// is impossible and treating one as a question wedges the task.
		return Event{Kind: KindIgnore, RawType: "extension_ui_request"}, nil
	default:
		// A method a future Pi adds. Tolerated like an unknown event type: the
		// stream continues, but no unanswerable question reaches the user.
		// DecodeStream logs it (R8).
		return Event{
			Kind:        KindUnknown,
			RawType:     "extension_ui_request",
			RequestID:   p.ID,
			RequestKind: p.Method,
		}, nil
	}
	return ev, nil
}

// decodeUIOptions reads Pi's select `options` field tolerantly. It returns the
// labels when the field is the published string array; otherwise, when the
// field is a bare string, it returns that string — which in the version-skew
// case is the question text itself.
func decodeUIOptions(raw json.RawMessage) (labels []string, bare string, ok bool) {
	if len(raw) == 0 {
		return nil, "", false
	}
	if err := json.Unmarshal(raw, &labels); err == nil {
		return labels, "", true
	}
	if err := json.Unmarshal(raw, &bare); err == nil {
		return nil, strings.TrimSpace(bare), false
	}
	return nil, "", false
}

// joinPrompt renders confirm's two-field prompt as one block. Pi's confirm arm
// requires a message, but Deuce's own extension sends the question as the title
// and an empty message, so the blank line must not be emitted for it.
func joinPrompt(title, message string) string {
	switch {
	case strings.TrimSpace(message) == "":
		return title
	case strings.TrimSpace(title) == "":
		return message
	default:
		return title + "\n\n" + message
	}
}

// DecodeStream reads JSONL lines from r and invokes fn for every emitted event
// whose Kind is not KindIgnore. Malformed lines and unknown event types are
// logged and skipped — the stream is never aborted by a single bad line
// (KTD2). It returns the first non-EOF read error from the underlying reader.
func DecodeStream(r io.Reader, fn func(Event)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // Pi lines can be large
	for sc.Scan() {
		ev, err := Decode(sc.Bytes())
		if err != nil {
			// Name the event type: a dropped line that says only "malformed"
			// cannot be traced back to the arm that broke (R8). Empty when the
			// line was not parseable as JSON at all.
			slog.Warn("pirun: skipping malformed event line", "type", ev.RawType, "error", err)
			continue
		}
		switch ev.Kind {
		case KindIgnore:
			continue
		case KindExtensionError:
			// Logged here, not forwarded: an extension can throw at load time,
			// when there is no task to attach the failure to, and the runtime's
			// translation returns early for a key with no current task — a
			// forwarded error would be silently dropped (R9).
			slog.Warn("pirun: extension error",
				"extensionPath", ev.ExtensionPath, "event", ev.ExtensionEvent, "error", ev.ErrorText)
			continue
		case KindUnknown:
			if ev.RawType == "extension_ui_request" {
				// A UI method Deuce cannot classify. Louder than an unknown
				// event type: it means Pi opened a dialog no one will answer.
				slog.Warn("pirun: unknown extension UI method",
					"method", ev.RequestKind, "requestId", ev.RequestID)
				continue
			}
			slog.Debug("pirun: skipping unknown event type", "type", ev.RawType)
			continue
		default:
			fn(ev)
		}
	}
	return sc.Err()
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func joinText(blocks []contentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" || blk.Type == "" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func trimLine(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r' || line[len(line)-1] == ' ') {
		line = line[:len(line)-1]
	}
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	return line
}
