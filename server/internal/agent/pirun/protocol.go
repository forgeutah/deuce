// Package pirun models the Pi (pi.dev) `--mode rpc` JSONL protocol and decodes
// its event stream into a small set of internal events the Deuce agent runtime
// consumes. It is the Go-side counterpart to the Pi RPC interface chosen in
// Topology A (see docs/plans/2026-06-03-002-feat-pi-harness-integration-plan.md).
//
// The protocol surface here is pinned against the real Pi CLI
// (@earendil-works/pi-coding-agent) verified during the U1 spike; a captured
// golden stream lives in testdata/golden-stream.jsonl. Pi's schema drifts, so
// the decoder tolerates unknown event variants rather than hard-failing (KTD2).
package pirun

import (
	"encoding/json"
	"strings"
)

// Command is a client→stdin RPC command. Construct one of the typed commands
// below and Marshal it to a single JSONL line.
type Command interface {
	commandType() string
}

// Prompt starts a new agent run. streamingBehavior is required only when a run
// is already in flight ("steer" or "followUp"); leave empty otherwise.
type Prompt struct {
	Message           string `json:"message"`
	ID                string `json:"id,omitempty"`
	StreamingBehavior string `json:"streamingBehavior,omitempty"`
}

func (Prompt) commandType() string { return "prompt" }

// Steer injects guidance into the currently-running turn (unsolicited
// redirection, R17).
type Steer struct {
	Message string `json:"message"`
}

func (Steer) commandType() string { return "steer" }

// FollowUp queues a message to be delivered after the current steer/turn.
type FollowUp struct {
	Message string `json:"message"`
}

func (FollowUp) commandType() string { return "follow_up" }

// Abort cancels the in-flight run (maps to /stop, R21).
type Abort struct{}

func (Abort) commandType() string { return "abort" }

// GetState requests the current session state; used for the post-launch
// readiness handshake before the first prompt (U1 transport caveat).
type GetState struct{}

func (GetState) commandType() string { return "get_state" }

// SetSteeringMode pins serial delivery ("one-at-a-time") vs "all".
type SetSteeringMode struct {
	Mode string `json:"mode"`
}

func (SetSteeringMode) commandType() string { return "set_steering_mode" }

// ExtensionUIResponse answers a blocking extension_ui_request (the ask-user
// mechanism, KTD15). The ID must match the originating request's ID.
//
// Pi's RpcExtensionUIResponse is a three-arm union (transcribed in
// testdata/pi-ui-protocol.json from the version recorded there):
//
//	{value: string}       answers select, input and editor — for select it is
//	                      the chosen option LABEL, not an index
//	{confirmed: boolean}  answers confirm — a JSON boolean
//	{cancelled: true}     valid for any dialog
//
// Exactly one arm may be set. Pi's stdin dispatcher correlates on type+id only
// and hands the raw object to a per-method parser, so an unrecognized key is
// not an error — it resolves to that parser's fallback (undefined for the value
// arms, false for confirm). That is why the previous single `response` field
// silently discarded every answer, and why the arms are pointers here: a
// zero-valued scalar would emit a stray second key that Pi would read as the
// answer. Build one with UIResponseValue / UIResponseConfirmed /
// UIResponseCancelled rather than by hand.
type ExtensionUIResponse struct {
	ID        string  `json:"id"`
	Value     *string `json:"value,omitempty"`
	Confirmed *bool   `json:"confirmed,omitempty"`
	Cancelled bool    `json:"cancelled,omitempty"`
}

func (ExtensionUIResponse) commandType() string { return "extension_ui_response" }

// UIResponseValue answers a select, input or editor dialog. For select, value
// must be the chosen option's label.
func UIResponseValue(id, value string) ExtensionUIResponse {
	return ExtensionUIResponse{ID: id, Value: &value}
}

// UIResponseConfirmed answers a confirm dialog with Pi's boolean arm.
func UIResponseConfirmed(id string, confirmed bool) ExtensionUIResponse {
	return ExtensionUIResponse{ID: id, Confirmed: &confirmed}
}

// UIResponseCancelled dismisses any dialog without an answer. Pi resolves a
// cancelled select/input/editor to undefined and a cancelled confirm to false.
// Deuce does not send this today — a stopped run tears the Pi process down, so
// the dialog dies with it — but the arm is part of the union Pi publishes and
// this is the only shape it may take.
func UIResponseCancelled(id string) ExtensionUIResponse {
	return ExtensionUIResponse{ID: id, Cancelled: true}
}

// IsConfirmMethod reports whether a dialog opened with this UI method (as the
// decoder reports it in Event.RequestKind) is answered with the `confirmed`
// boolean arm rather than the `value` string arm. Every other blocking method —
// select, input, and editor, which the decoder folds into input — takes value.
func IsConfirmMethod(method string) bool { return method == uiMethodConfirm }

// Marshal renders a command as a single JSONL line (no trailing newline; the
// writer adds it). It injects the discriminator "type" field Pi expects.
func Marshal(c Command) ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	// Splice the "type" discriminator in without a second struct: decode to a
	// map, set type, re-encode. Commands are tiny, so this is cheap and keeps
	// each command struct free of a redundant constant Type field.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["type"] = c.commandType()
	return json.Marshal(m)
}

// normalizeTool maps Pi's tool names to the action-log vocabulary used by the
// Super Threads UI (Read/Grep/Edit/Write/Bash/Think). Unknown tools are
// title-cased so a new Pi tool still renders sensibly rather than being dropped.
func normalizeTool(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "Read"
	case "grep":
		return "Grep"
	case "edit":
		return "Edit"
	case "write":
		return "Write"
	case "bash":
		return "Bash"
	case "think":
		return "Think"
	case "ask_user":
		return "Ask"
	case "":
		return ""
	default:
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

// argKeys are the Pi tool-argument keys we surface as the action's headline
// argument, in priority order. bash uses "command"; file tools use a path key.
var argKeys = []string{"command", "path", "file_path", "filePath", "pattern", "url"}

// askUserPlaceholder is the headline arg for an ask_user call whose question
// can't be extracted. The question must never surface as raw JSON (R9), so the
// generic compact-JSON fallback is off-limits for this tool.
const askUserPlaceholder = "(question unavailable)"

// askUserHeadline extracts the question text from an ask_user call's args. The
// extension schema requires "question" as a string; anything else degrades to a
// readable placeholder, never the args JSON.
func askUserHeadline(args map[string]json.RawMessage) string {
	if v, ok := args["question"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return askUserPlaceholder
}

// headlineArg picks a single human-readable argument string from a tool's args
// object: a known key if present, otherwise the compact JSON of the whole map
// so nothing is silently lost.
func headlineArg(args map[string]json.RawMessage) string {
	for _, k := range argKeys {
		if v, ok := args[k]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				return s
			}
			return strings.TrimSpace(string(v))
		}
	}
	if len(args) == 0 {
		return ""
	}
	b, _ := json.Marshal(args)
	return string(b)
}
