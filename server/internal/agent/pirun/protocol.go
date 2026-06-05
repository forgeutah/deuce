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

// SwitchSession re-attaches a restarted process to a prior session by file
// path (KTD13 continuity). Pi identifies sessions by file path or partial UUID.
type SwitchSession struct {
	SessionPath string `json:"sessionPath"`
}

func (SwitchSession) commandType() string { return "switch_session" }

// ExtensionUIResponse answers a blocking extension_ui_request (the ask-user
// mechanism, KTD15). The ID must match the originating request's ID.
type ExtensionUIResponse struct {
	ID       string `json:"id"`
	Response any    `json:"response"`
}

func (ExtensionUIResponse) commandType() string { return "extension_ui_response" }

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
	case "":
		return ""
	default:
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

// argKeys are the Pi tool-argument keys we surface as the action's headline
// argument, in priority order. bash uses "command"; file tools use a path key.
var argKeys = []string{"command", "path", "file_path", "filePath", "pattern", "url"}

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
