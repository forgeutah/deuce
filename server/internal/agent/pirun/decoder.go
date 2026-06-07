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

	// Command reply (KindCommandReply).
	Command string
	ReplyID string
	Success bool
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
	case "message_start", "message_end", "turn_start", "turn_end",
		"tool_execution_update", "queue_update",
		"compaction_start", "compaction_end",
		"auto_retry_start", "auto_retry_end", "extension_error":
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
	return Event{
		Kind:       KindToolStarted,
		RawType:    "tool_execution_start",
		ToolCallID: p.ToolCallID,
		Tool:       normalizeTool(p.ToolName),
		Arg:        headlineArg(p.Args),
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

func decodeUIRequest(line []byte) (Event, error) {
	// The exact extension_ui_request shape is pinned when the ask-user
	// extension (U12) lands; decode best-effort by id + common prompt/kind keys
	// so the awaiting-input transition fires regardless of minor field naming.
	var p struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Kind   string `json:"kind"`
		Prompt string `json:"prompt"`
		Params struct {
			Prompt  string `json:"prompt"`
			Message string `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return Event{Kind: KindUnknown, RawType: "extension_ui_request"}, err
	}
	prompt := firstNonEmpty(p.Prompt, p.Params.Prompt, p.Params.Message)
	kind := firstNonEmpty(p.Kind, p.Method)
	return Event{
		Kind:        KindAwaitingInput,
		RawType:     "extension_ui_request",
		RequestID:   p.ID,
		RequestKind: kind,
		Prompt:      prompt,
	}, nil
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
			slog.Warn("pirun: skipping malformed event line", "error", err)
			continue
		}
		switch ev.Kind {
		case KindIgnore:
			continue
		case KindUnknown:
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
