package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
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
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"createdAt"`
}

func toMessageResponse(m db.Message) messageResponse {
	ec := json.RawMessage("null")
	if m.ExpandableContent != nil {
		ec = m.ExpandableContent
	}
	return messageResponse{
		ID:                m.ID,
		SessionID:         m.SessionID,
		AuthorID:          m.AuthorID,
		AuthorType:        m.AuthorType,
		Content:           m.Content,
		ExpandableContent: ec,
		Status:            m.Status,
		CreatedAt:         m.CreatedAt,
	}
}

// deuceMentionRE detects an @deuce mention in message content, server-side
// (R5). The left guard (start-of-string or a non-word character) keeps email
// addresses like clint@deuce.dev from triggering; the trailing \b keeps
// near-misses like @deucebot from triggering. Case-insensitive.
var deuceMentionRE = regexp.MustCompile(`(?i)(^|\W)@deuce\b`)

// isStopCommand reports whether a message is the exact /stop command (R6).
// Exact match only — "@deuce make the flicker stop" must enqueue work, not
// cancel it (the old " stop"-suffix trigger was removed for this reason).
func isStopCommand(content string) bool {
	return strings.TrimSpace(content) == "/stop"
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

// sendMessageRequest no longer carries a mentions array — mention detection is
// server-side (R5). Pre-013 clients still send the field; unknown JSON fields
// are ignored by the decoder, so stale tabs degrade gracefully.
type sendMessageRequest struct {
	Content string `json:"content"`
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

	msg, err := h.queries.CreateMessage(r.Context(), db.CreateMessageParams{
		SessionID:         sessionID,
		AuthorID:          userID,
		AuthorType:        "human",
		Content:           req.Content,
		ExpandableContent: nil,
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

	if isStopCommand(req.Content) {
		if h.runtime != nil {
			h.runtime.CancelSession(r.Context(), sessionID.String())
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// @deuce mention → enqueue a task through the Pi runtime (R5).
	if deuceMentionRE.MatchString(req.Content) && h.runtime != nil {
		session, err := h.queries.GetSession(r.Context(), sessionID)
		if err == nil {
			switch session.WorkspaceStatus {
			case "starting":
				h.postSystemMessage(sessionID, "Workspace is still starting — your agent request will run when it's ready.")
			case "failed", "suspended":
				h.postSystemMessage(sessionID, "Workspace is not available. Please restart the workspace before using the agent.")
			case "ready":
				if _, err := h.runtime.Enqueue(r.Context(), agentpkg.EnqueueParams{
					SessionID:       sessionID.String(),
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

	writeJSON(w, http.StatusCreated, resp)
}

// StopAgent handles POST /api/sessions/{id}/agent/stop — cancels the session's
// running task and drains its queue (R6), same semantics as the /stop chat
// command. Session-membership gated (write class).
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

// postAgentReply posts deuce's reply as a chat message and broadcasts it.
// Wired into the Pi runtime (SetReplyPoster) so agent output shows in the
// existing chat — the Super Threads task/action cards are a separate surface.
// Authorship pins to the fixed deuce UUID; the chat visibility filter keys on
// it (R9).
func (h *Handler) postAgentReply(sessionID, reply string) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	ctx := context.Background()
	msg, err := h.queries.CreateMessage(ctx, db.CreateMessageParams{
		SessionID:         sid,
		AuthorID:          uuid.MustParse(agentpkg.DeuceAgentID),
		AuthorType:        "agent",
		Content:           reply,
		ExpandableContent: nil,
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

// postSystemMessage posts a system notice. The nil-UUID author is the system
// sentinel — distinct from deuce's fixed UUID, so the visibility filter keeps
// notices visible in chat while deuce's task replies stay hidden (R9).
func (h *Handler) postSystemMessage(sessionID uuid.UUID, content string) {
	msg, err := h.queries.CreateMessage(context.Background(), db.CreateMessageParams{
		SessionID:         sessionID,
		AuthorID:          uuid.Nil,
		AuthorType:        "agent",
		Content:           content,
		ExpandableContent: nil,
		Status:            "sent",
	})
	if err != nil {
		slog.Error("failed to post system message", "error", err)
		return
	}
	wsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID.String(), toMessageResponse(msg))
	h.hub.BroadcastToSession(sessionID.String(), wsMsg, nil)
}

