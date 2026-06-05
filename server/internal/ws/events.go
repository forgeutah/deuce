package ws

import "encoding/json"

// Client-to-server message types
const (
	TypeJoin     = "join"
	TypeLeave    = "leave"
	TypeMarkRead = "mark_read"
	// TypeSteer carries a free-text reply targeted at a live agent run (the
	// drawer composer). Routing (feed-the-run vs enqueue) and authorization are
	// handled by the steer callback; see KTD9/KTD14.
	TypeSteer = "steer"
)

// Server-to-client message types
const (
	TypeNewMessage      = "new_message"
	TypeAgentStatus     = "agent_status"
	TypeTypingIndicator = "typing_indicator"
	TypeActivityUpdate  = "activity_update"
	TypeSessionUpdate   = "session_update"
	TypeUnreadUpdate    = "unread_update"
	TypeWorkspaceLog    = "workspace_log"
	TypeAgentOutput     = "agent_output"

	// AgentRunEvent family (Super Threads). Append-only, per-session
	// monotonic-seq deltas applied client-side by seq. Deliberately NOT routed
	// through session_update, which triggers a full session-list refetch (KTD6).
	TypeTaskEnqueued      = "task_enqueued"
	TypeTaskStarted       = "task_started"
	TypeTaskAwaitingInput = "task_awaiting_input"
	TypeActionStarted     = "action_started"
	TypeActionCompleted   = "action_completed"
	TypeTaskCompleted     = "task_completed"
)

// MaxSteerLen bounds steer free-text before it is forwarded to Pi stdin,
// separate from the WS frame limit (S5).
const MaxSteerLen = 8000

// ClientMessage is a message from a client. The base shape is type+sessionId;
// steer messages additionally carry agentId + message.
type ClientMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId,omitempty"`
	Message   string `json:"message,omitempty"`
}

// TaskEventPayload is the JSON payload for the task-lifecycle AgentRunEvents
// (enqueued / started / awaiting_input / completed). Fields are populated per
// event type; seq + taskId + agentId are always set.
type TaskEventPayload struct {
	Seq             int64  `json:"seq"`
	TaskID          string `json:"taskId"`
	AgentID         string `json:"agentId"`
	RequestedBy     string `json:"requestedBy,omitempty"`
	AnchorMessageID string `json:"anchorMessageId,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	State           string `json:"state,omitempty"`
	Position        int    `json:"position,omitempty"`        // queue #N for queued tasks
	PendingQuestion string `json:"pendingQuestion,omitempty"` // awaiting_input
	Reply           string `json:"reply,omitempty"`           // completed
	Status          string `json:"status,omitempty"`          // completed: done|failed|cancelled
}

// ActionEventPayload is the JSON payload for action_started / action_completed.
type ActionEventPayload struct {
	Seq     int64  `json:"seq"`
	TaskID  string `json:"taskId"`
	AgentID string `json:"agentId"`
	CallID  string `json:"callId"`
	Tool    string `json:"tool,omitempty"`
	Arg     string `json:"arg,omitempty"`
	Text    string `json:"text,omitempty"`
	Stat    string `json:"stat,omitempty"`
	IsError bool   `json:"isError,omitempty"`
}

// ServerMessage is a message from the server to clients
type ServerMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Payload   json.RawMessage `json:"payload"`
}

// NewServerMessage creates a server message with the payload marshaled to JSON
func NewServerMessage(msgType, sessionID string, payload any) (ServerMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return ServerMessage{}, err
	}
	return ServerMessage{
		Type:      msgType,
		SessionID: sessionID,
		Payload:   data,
	}, nil
}
