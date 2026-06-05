package ws

import (
	"encoding/json"
	"testing"
)

func TestClientMessageSteerDecodes(t *testing.T) {
	var msg ClientMessage
	raw := `{"type":"steer","sessionId":"s1","agentId":"a1","message":"use the staging db"}`
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != TypeSteer || msg.SessionID != "s1" || msg.AgentID != "a1" || msg.Message != "use the staging db" {
		t.Errorf("decoded steer = %+v", msg)
	}

	// A legacy join message (no agentId/message) still decodes.
	var join ClientMessage
	if err := json.Unmarshal([]byte(`{"type":"join","sessionId":"s2"}`), &join); err != nil {
		t.Fatalf("unmarshal join: %v", err)
	}
	if join.Type != TypeJoin || join.AgentID != "" || join.Message != "" {
		t.Errorf("decoded join = %+v", join)
	}
}

func TestAgentRunEventPayloadShape(t *testing.T) {
	sm, err := NewServerMessage(TypeActionStarted, "s1", ActionEventPayload{
		Seq: 7, TaskID: "t1", AgentID: "a1", CallID: "toolu_1", Tool: "Bash", Arg: "ls -la",
	})
	if err != nil {
		t.Fatalf("NewServerMessage: %v", err)
	}
	if sm.Type != TypeActionStarted || sm.SessionID != "s1" {
		t.Errorf("server message envelope = %+v", sm)
	}
	var decoded map[string]any
	if err := json.Unmarshal(sm.Payload, &decoded); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if decoded["seq"].(float64) != 7 || decoded["callId"] != "toolu_1" || decoded["tool"] != "Bash" {
		t.Errorf("camelCase payload = %v", decoded)
	}
	// omitempty drops zero fields: a started action carries no isError:true.
	if _, present := decoded["isError"]; present {
		t.Errorf("isError should be omitted when false, payload = %v", decoded)
	}
}

func TestAuthorizedGate(t *testing.T) {
	// nil gate → allowed (production wires it; documented in client.go).
	open := &Client{UserID: "u1"}
	if !open.authorized("s1") {
		t.Error("nil Authorize should allow")
	}

	allowOnly := map[string]bool{"u1|s1": true}
	gated := &Client{UserID: "u1", Authorize: func(uid, sid string) bool { return allowOnly[uid+"|"+sid] }}
	if !gated.authorized("s1") {
		t.Error("member should be allowed")
	}
	if gated.authorized("s2") {
		t.Error("non-member should be denied")
	}
}
