package ws

import "encoding/json"

// Client-to-server message types
const (
	TypeJoin     = "join"
	TypeLeave    = "leave"
	TypeMarkRead = "mark_read"
)

// Server-to-client message types
const (
	TypeNewMessage     = "new_message"
	TypeAgentStatus    = "agent_status"
	TypeTypingIndicator = "typing_indicator"
	TypeActivityUpdate = "activity_update"
	TypeSessionUpdate  = "session_update"
	TypeUnreadUpdate   = "unread_update"
)

// ClientMessage is a message from a client
type ClientMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
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
