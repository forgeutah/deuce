package agent

import (
	"encoding/json"
	"strings"
)

// outputParser parses Claude Code stream-json output into structured results.
type outputParser struct {
	summaryParts      []string
	expandableContent []map[string]string
	sessionID         string
	currentToolName   string
}

func newOutputParser() *outputParser {
	return &outputParser{}
}

// parseLine parses a single line of stream-json output and returns any events.
func (p *outputParser) parseLine(line string) []StreamEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		// Not JSON — could be raw text output
		return nil
	}

	var events []StreamEvent

	msgType, _ := raw["type"].(string)

	switch msgType {
	case "stream_event":
		event, _ := raw["event"].(map[string]any)
		if event == nil {
			break
		}

		delta, _ := event["delta"].(map[string]any)
		if delta != nil {
			deltaType, _ := delta["type"].(string)
			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string)
				if text != "" {
					p.summaryParts = append(p.summaryParts, text)
					events = append(events, StreamEvent{Type: "text", Content: text})
				}
			case "input_json_delta":
				// Tool input streaming — not critical for chat display
			}
		}

		// Handle content_block_start for tool use
		contentBlock, _ := event["content_block"].(map[string]any)
		if contentBlock != nil {
			blockType, _ := contentBlock["type"].(string)
			if blockType == "tool_use" {
				toolName, _ := contentBlock["name"].(string)
				p.currentToolName = toolName
				events = append(events, StreamEvent{
					Type:    "tool_use",
					Content: toolName,
				})
			}
		}

	case "result":
		// Final result — extract session_id and any remaining data
		if sid, ok := raw["session_id"].(string); ok {
			p.sessionID = sid
		}

		// Extract result text if present
		if result, ok := raw["result"].(string); ok && result != "" {
			if len(p.summaryParts) == 0 {
				p.summaryParts = append(p.summaryParts, result)
			}
		}

	case "system":
		// System events (init, retry, etc.) — log but don't surface
		subtype, _ := raw["subtype"].(string)
		if subtype == "api_retry" {
			events = append(events, StreamEvent{
				Type:    "error",
				Content: "Rate limited, retrying...",
			})
		}
	}

	return events
}

func (p *outputParser) getSummary() string {
	return strings.Join(p.summaryParts, "")
}

func (p *outputParser) getExpandableContent() []map[string]string {
	return p.expandableContent
}

func (p *outputParser) getSessionID() string {
	return p.sessionID
}
