package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentpkg "github.com/forgeutah/deuce/server/internal/agent"
	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

type messageResponse struct {
	ID                uuid.UUID       `json:"id"`
	SessionID         uuid.UUID       `json:"sessionId"`
	AuthorID          uuid.UUID       `json:"authorId"`
	AuthorType        string          `json:"authorType"`
	Content           string          `json:"content"`
	ExpandableContent json.RawMessage `json:"expandableContent"`
	Mentions          []string        `json:"mentions"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"createdAt"`
}

func toMessageResponse(m db.Message) messageResponse {
	ec := json.RawMessage("null")
	if m.ExpandableContent != nil {
		ec = m.ExpandableContent
	}
	mentions := m.Mentions
	if mentions == nil {
		mentions = []string{}
	}
	return messageResponse{
		ID:                m.ID,
		SessionID:         m.SessionID,
		AuthorID:          m.AuthorID,
		AuthorType:        m.AuthorType,
		Content:           m.Content,
		ExpandableContent: ec,
		Mentions:          mentions,
		Status:            m.Status,
		CreatedAt:         m.CreatedAt,
	}
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Read gate: team members may read a session's history (static snapshot)
	// even before joining it. Non-team users get 403.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	before := r.URL.Query().Get("before")

	var msgs []db.Message
	if before != "" {
		beforeID, err := uuid.Parse(before)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_CURSOR", "invalid cursor")
			return
		}
		msgs, err = h.queries.ListMessagesBefore(r.Context(), db.ListMessagesBeforeParams{
			SessionID: sessionID,
			ID:        beforeID,
			Limit:     int32(limit),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list messages")
			return
		}
	} else {
		msgs, err = h.queries.ListMessages(r.Context(), db.ListMessagesParams{
			SessionID: sessionID,
			Limit:     int32(limit),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list messages")
			return
		}
	}

	result := make([]messageResponse, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, toMessageResponse(m))
	}

	cursor := ""
	if len(msgs) > 0 {
		cursor = msgs[len(msgs)-1].ID.String()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": result,
		"cursor":   cursor,
		"hasMore":  len(msgs) == limit,
	})
}

type sendMessageRequest struct {
	Content  string   `json:"content"`
	Mentions []string `json:"mentions"`
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	// Write gate: posting (and the agent @mention routing below it) requires
	// SESSION membership. Team members who are only viewing must Join first.
	// Checked before body validation so a non-member can't probe content
	// rules. steer (WS) is already session-gated in ws/client.go.
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "EMPTY_CONTENT", "message content is required")
		return
	}

	if req.Mentions == nil {
		req.Mentions = []string{}
	}

	msg, err := h.queries.CreateMessage(r.Context(), db.CreateMessageParams{
		SessionID:         sessionID,
		AuthorID:          userID,
		AuthorType:        "human",
		Content:           req.Content,
		ExpandableContent: nil,
		Mentions:          req.Mentions,
		Status:            "sent",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to create message")
		return
	}

	// Update last activity
	_ = h.queries.UpdateSessionLastActivity(r.Context(), sessionID)

	resp := toMessageResponse(msg)

	// Broadcast to other clients in the session (exclude sender)
	wsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID.String(), resp)
	// Find the sender's WS client to exclude — for now broadcast to all (sender checks client-side)
	h.hub.BroadcastToSession(sessionID.String(), wsMsg, nil)

	// Handle /stop command
	if strings.TrimSpace(req.Content) == "/stop" || strings.HasSuffix(strings.TrimSpace(req.Content), " stop") {
		if h.runtime != nil {
			h.runtime.CancelSession(r.Context(), sessionID.String())
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Process agent mentions through the Pi runtime.
	if len(req.Mentions) > 0 && h.runtime != nil {
		session, err := h.queries.GetSession(r.Context(), sessionID)
		if err == nil {
			switch session.WorkspaceStatus {
			case "starting":
				h.postSystemMessage(sessionID, "Workspace is still starting — your agent request will run when it's ready.")
			case "failed", "suspended":
				h.postSystemMessage(sessionID, "Workspace is not available. Please restart the workspace before using agents.")
			case "ready":
				for _, mention := range req.Mentions {
					agentID, err := uuid.Parse(mention)
					if err != nil {
						continue
					}
					if _, err := h.queries.GetAgent(r.Context(), agentID); err != nil {
						continue
					}

					if _, err := h.runtime.Enqueue(r.Context(), agentpkg.EnqueueParams{
						SessionID:       sessionID.String(),
						AgentID:         agentID.String(),
						RequestedBy:     userID.String(),
						AnchorMessageID: msg.ID.String(),
						Prompt:          req.Content,
						WorkspaceID:     session.Name,
					}); err != nil {
						slog.Error("failed to enqueue agent task", "error", err)
						h.postSystemMessage(sessionID, "Could not start the agent. Please try again.")
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// StopAgent handles POST /api/sessions/{id}/agents/stop
func (h *Handler) StopAgent(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Write gate: cancelling a session's agent runs requires SESSION membership.
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}
	if !h.requireSessionMember(w, r, sessionID, userID) {
		return
	}

	if h.runtime != nil {
		h.runtime.CancelSession(r.Context(), sessionID.String())
	}
	w.WriteHeader(http.StatusNoContent)
}

// postAgentReply posts an agent's reply as a chat message and broadcasts it.
// Wired into the Pi runtime (SetReplyPoster) so agent output shows in the
// existing chat — the Super Threads task/action cards are a separate surface.
func (h *Handler) postAgentReply(sessionID, agentID, reply string) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	aid, err := uuid.Parse(agentID)
	if err != nil {
		aid = uuid.Nil
	}
	ctx := context.Background()
	msg, err := h.queries.CreateMessage(ctx, db.CreateMessageParams{
		SessionID:         sid,
		AuthorID:          aid,
		AuthorType:        "agent",
		Content:           reply,
		ExpandableContent: nil,
		Mentions:          []string{},
		Status:            "sent",
	})
	if err != nil {
		slog.Error("failed to post agent reply", "error", err)
		return
	}
	_ = h.queries.UpdateSessionLastActivity(ctx, sid)
	wsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID, toMessageResponse(msg))
	h.hub.BroadcastToSession(sessionID, wsMsg, nil)
}

func (h *Handler) postSystemMessage(sessionID uuid.UUID, content string) {
	msg, err := h.queries.CreateMessage(context.Background(), db.CreateMessageParams{
		SessionID:         sessionID,
		AuthorID:          uuid.Nil,
		AuthorType:        "agent",
		Content:           content,
		ExpandableContent: nil,
		Mentions:          []string{},
		Status:            "sent",
	})
	if err != nil {
		slog.Error("failed to post system message", "error", err)
		return
	}
	wsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID.String(), toMessageResponse(msg))
	h.hub.BroadcastToSession(sessionID.String(), wsMsg, nil)
}

