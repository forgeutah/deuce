package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	agentpkg "github.com/forgeutah/deuce/server/internal/agent"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	ws.ServeWS(h.hub, w, r, userID, h.wsOrigins, h.configureWSClient)
}

// configureWSClient wires per-connection callbacks: the session-membership gate
// (KTD14), steer routing into the runtime (KTD9), and mark-read.
func (h *Handler) configureWSClient(client *ws.Client) {
	client.Authorize = h.isSessionMember
	client.OnMarkRead = func(c *ws.Client, sessionID string) {
		sid, err := uuid.Parse(sessionID)
		if err != nil {
			return
		}
		uid, err := uuid.Parse(c.UserID)
		if err != nil {
			return
		}
		_ = h.queries.MarkSessionRead(context.Background(), db.MarkSessionReadParams{SessionID: sid, UserID: uid})
	}
	if h.runtime != nil {
		client.OnSteer = h.handleSteer
	}
}

// isSessionMember backs the WS Authorize gate: only members may subscribe to a
// session's heavy event stream or steer its agents (KTD14).
func (h *Handler) isSessionMember(userID, sessionID string) bool {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return false
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return false
	}
	member, err := h.queries.IsSessionMember(context.Background(), db.IsSessionMemberParams{SessionID: sid, UserID: uid})
	return err == nil && member
}

// handleSteer posts the reply to the channel for shared visibility (R15) and
// routes it into the live run (feed/answer) or enqueues a new task (R19).
func (h *Handler) handleSteer(c *ws.Client, sessionID, agentID, message string) {
	if message == "" {
		return
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		return
	}
	ctx := context.Background()

	// Post the reply as a channel message so every session member sees it.
	if msg, err := h.queries.CreateMessage(ctx, db.CreateMessageParams{
		SessionID: sid, AuthorID: uid, AuthorType: "human", Content: message, Mentions: []string{}, Status: "sent",
	}); err == nil {
		_ = h.queries.UpdateSessionLastActivity(ctx, sid)
		if wsMsg, e := ws.NewServerMessage(ws.TypeNewMessage, sessionID, toMessageResponse(msg)); e == nil {
			h.hub.BroadcastToSession(sessionID, wsMsg, nil)
		}
	}

	session, err := h.queries.GetSession(ctx, sid)
	workspaceID := ""
	if err == nil {
		workspaceID = session.Name
	}
	_, _ = h.runtime.RouteOrEnqueue(ctx, agentpkg.EnqueueParams{
		SessionID:   sessionID,
		AgentID:     agentID,
		RequestedBy: c.UserID,
		Prompt:      message,
		WorkspaceID: workspaceID,
	})
}
