package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The no-JSON backstop (R9/R11/R12). When the structured extension_ui_request
// never fires — the ask-user extension failed to install, or the model narrated
// the call instead of invoking the tool — the question arrives as assistant text
// shaped like `ask_user({"question":"..."})`. Left alone it posts to chat as raw
// JSON, which reads as a broken product. sanitizeNarratedQuestion rewrites that
// text into the plain question before it is persisted, broadcast, or posted.
var (
	// Leak shape: an ask_user call opening with a JSON object. Requires `({`
	// so a bare prose mention of "ask_user" never matches. Catches truncated
	// calls too (no closing required here — that's the AE4 floor).
	askUserLeakRe = regexp.MustCompile(`(?is)ask_user\s*\(\s*\{`)
	// A complete `ask_user({...})` call, captured so surrounding prose survives.
	askUserCallRe = regexp.MustCompile(`(?is)ask_user\s*\(\s*\{.*\}\s*\)`)
	// The JSON object within a matched call.
	askUserObjRe = regexp.MustCompile(`(?s)\{.*\}`)
	// Best-effort "question" value extraction when the object won't parse as
	// JSON (handles a closed string even if the call braces are truncated).
	askUserQuestionRe = regexp.MustCompile(`(?is)"question"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

const malformedQuestionFloor = "(The agent tried to ask you a question, but the request was malformed.)"

// looksLikeNarratedQuestion reports whether reply contains an ask_user tool call
// rendered as text rather than a normal prose reply.
func looksLikeNarratedQuestion(reply string) bool {
	return askUserLeakRe.MatchString(reply)
}

// sanitizeNarratedQuestion turns a narrated ask_user call into the plain
// question text. A complete call is replaced in place so any surrounding prose
// is preserved; a truncated/garbled call degrades to the extracted question or,
// failing that, a readable placeholder — never a JSON fragment. A reply with no
// leak shape is returned unchanged.
func sanitizeNarratedQuestion(reply string) string {
	if !looksLikeNarratedQuestion(reply) {
		return reply
	}
	if askUserCallRe.MatchString(reply) {
		out := strings.TrimSpace(askUserCallRe.ReplaceAllStringFunc(reply, replaceNarratedCall))
		if out != "" {
			return out
		}
	}
	// Truncated / garbled call (no complete ({...}) to replace): salvage the
	// question if a closed "question" string survives, else the floor.
	if q := extractQuestionText(reply); q != "" {
		return q
	}
	return malformedQuestionFloor
}

// replaceNarratedCall maps one complete ask_user(...) call to its question text,
// or the floor when the object can't yield a question.
func replaceNarratedCall(match string) string {
	obj := askUserObjRe.FindString(match)
	if obj != "" {
		var parsed struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal([]byte(obj), &parsed); err == nil {
			if q := strings.TrimSpace(parsed.Question); q != "" {
				return q
			}
		}
	}
	if q := extractQuestionText(match); q != "" {
		return q
	}
	return malformedQuestionFloor
}

// extractQuestionText pulls a "question":"..." value out of arbitrary text by
// regex (a fallback for malformed JSON) and unescapes it.
func extractQuestionText(s string) string {
	m := askUserQuestionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(unescapeJSONString(m[1]))
}

// unescapeJSONString decodes JSON string escapes (\n, \", \\, …) in a raw
// captured value, falling back to the input when it isn't decodable.
func unescapeJSONString(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err == nil {
		return out
	}
	return s
}
