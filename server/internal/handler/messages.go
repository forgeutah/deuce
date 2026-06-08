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
	"github.com/jackc/pgx/v5/pgtype"

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
		} else if h.agentQueue != nil {
			h.agentQueue.Cancel(sessionID.String())
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Process agent mentions: route through the Pi runtime when enabled,
	// otherwise the legacy executor queue (DEUCE_AGENT_HARNESS, KTD11).
	agentsEnabled := h.runtime != nil || (h.agentQueue != nil && h.executor != nil)
	if len(req.Mentions) > 0 && agentsEnabled {
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
					ag, err := h.queries.GetAgent(r.Context(), agentID)
					if err != nil {
						continue
					}

					if h.runtime != nil {
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
						continue
					}

					task := agentpkg.Task{
						SessionID: sessionID.String(),
						AgentID:   agentID.String(),
						AgentName: ag.Name,
						Prompt:    req.Content,
						Callback:  func(t agentpkg.Task) { h.executeAgent(t, session.Name) },
					}
					if err := h.agentQueue.Enqueue(task); err != nil {
						h.postSystemMessage(sessionID, "Agent queue is full. Please wait for current work to complete.")
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// StopAgent handles POST /api/sessions/{id}/agents/stop
func (h *Handler) StopAgent(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if h.agentQueue != nil {
		h.agentQueue.Cancel(sessionID)
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

func (h *Handler) executeAgent(task agentpkg.Task, workspaceName string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionID, _ := uuid.Parse(task.SessionID)
	agentID, _ := uuid.Parse(task.AgentID)

	// Store cancel function for stop button
	h.agentQueue.SetCancel(task.SessionID, cancel)
	defer h.agentQueue.ClearCancel(task.SessionID)

	// Set agent to working
	_ = h.queries.UpdateSessionAgentStatus(ctx, db.UpdateSessionAgentStatusParams{
		SessionID: sessionID,
		AgentID:   agentID,
		Status:    "working",
	})
	statusMsg, _ := ws.NewServerMessage(ws.TypeAgentStatus, sessionID.String(), map[string]any{
		"agentId": task.AgentID,
		"status":  "working",
	})
	h.hub.BroadcastToSession(sessionID.String(), statusMsg, nil)

	// Send typing indicator
	typingMsg, _ := ws.NewServerMessage(ws.TypeTypingIndicator, sessionID.String(), map[string]any{
		"agentId": task.AgentID,
		"active":  true,
	})
	h.hub.BroadcastToSession(sessionID.String(), typingMsg, nil)

	// Get agent info for system prompt
	ag, err := h.queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("failed to get agent", "error", err)
		h.finishAgent(sessionID, agentID, task.AgentID, "Failed to load agent configuration.", true)
		return
	}

	// Get Claude session ID for continuity
	claudeSessionID := ""
	if csid, err := h.queries.GetClaudeSessionID(ctx, db.GetClaudeSessionIDParams{
		SessionID: sessionID,
		AgentID:   agentID,
	}); err == nil {
		claudeSessionID = csid
	}

	// Build chat history context
	chatHistory := h.buildChatHistory(ctx, sessionID, claudeSessionID != "")

	// Streaming callback — broadcast agent output via WebSocket
	streamFn := func(event agentpkg.StreamEvent) {
		outMsg, _ := ws.NewServerMessage(ws.TypeAgentOutput, sessionID.String(), map[string]any{
			"agentId":     task.AgentID,
			"content":     event.Content,
			"contentType": event.Type,
		})
		h.hub.BroadcastToSession(sessionID.String(), outMsg, nil)
	}

	// Execute
	result, execErr := h.executor.Execute(ctx, agentpkg.ExecuteParams{
		WorkspaceID:     workspaceName,
		AgentName:       ag.Name,
		SystemPrompt:    ag.SystemPrompt,
		UserMessage:     task.Prompt,
		ChatHistory:     chatHistory,
		ClaudeSessionID: claudeSessionID,
		Model:           ag.Model,
	}, streamFn)

	if execErr != nil {
		errMsg := "Agent encountered an error: " + execErr.Error()
		if result != nil && result.Error == "cancelled" {
			errMsg = "Cancelled by user."
		} else if result != nil && result.Error == "timeout" {
			errMsg = "Agent execution timed out."
		}
		h.finishAgent(sessionID, agentID, task.AgentID, errMsg, true)
		return
	}

	// Store Claude session ID for continuity
	if result.ClaudeSessionID != "" {
		_ = h.queries.UpdateClaudeSessionID(ctx, db.UpdateClaudeSessionIDParams{
			SessionID:       sessionID,
			AgentID:         agentID,
			ClaudeSessionID: result.ClaudeSessionID,
		})
	}

	// Create the agent message
	var ec []byte
	if len(result.ExpandableContent) > 0 {
		ec, _ = json.Marshal(result.ExpandableContent)
	}

	content := result.Summary
	if content == "" {
		content = "Task completed."
	}

	h.finishAgent(sessionID, agentID, task.AgentID, content, false, ec)
}

func (h *Handler) finishAgent(sessionID, agentID uuid.UUID, agentIDStr, content string, isError bool, expandableContent ...[]byte) {
	ctx := context.Background()

	// Stop typing
	stopTyping, _ := ws.NewServerMessage(ws.TypeTypingIndicator, sessionID.String(), map[string]any{
		"agentId": agentIDStr,
		"active":  false,
	})
	h.hub.BroadcastToSession(sessionID.String(), stopTyping, nil)

	// Create message
	var ec []byte
	if len(expandableContent) > 0 {
		ec = expandableContent[0]
	}

	agentMsg, err := h.queries.CreateMessage(ctx, db.CreateMessageParams{
		SessionID:         sessionID,
		AuthorID:          agentID,
		AuthorType:        "agent",
		Content:           content,
		ExpandableContent: ec,
		Mentions:          []string{},
		Status:            "sent",
	})
	if err != nil {
		slog.Error("failed to create agent message", "error", err)
	} else {
		_ = h.queries.UpdateSessionLastActivity(ctx, sessionID)
		agentWsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID.String(), toMessageResponse(agentMsg))
		h.hub.BroadcastToSession(sessionID.String(), agentWsMsg, nil)
	}

	// Set status
	finalStatus := "idle"
	if isError {
		finalStatus = "error"
		// Reset to idle after a delay
		go func() {
			time.Sleep(10 * time.Second)
			_ = h.queries.UpdateSessionAgentStatus(context.Background(), db.UpdateSessionAgentStatusParams{
				SessionID: sessionID,
				AgentID:   agentID,
				Status:    "idle",
			})
			idleMsg, _ := ws.NewServerMessage(ws.TypeAgentStatus, sessionID.String(), map[string]any{
				"agentId": agentIDStr,
				"status":  "idle",
			})
			h.hub.BroadcastToSession(sessionID.String(), idleMsg, nil)
		}()
	}

	_ = h.queries.UpdateSessionAgentStatus(ctx, db.UpdateSessionAgentStatusParams{
		SessionID: sessionID,
		AgentID:   agentID,
		Status:    finalStatus,
	})

	statusMsg, _ := ws.NewServerMessage(ws.TypeAgentStatus, sessionID.String(), map[string]any{
		"agentId": agentIDStr,
		"status":  finalStatus,
	})
	h.hub.BroadcastToSession(sessionID.String(), statusMsg, nil)

	// Create activity
	agentUUID := pgtype.UUID{Bytes: agentID, Valid: true}
	desc := "completed a task"
	if isError {
		desc = "encountered an error"
	}
	_, _ = h.queries.CreateActivity(ctx, db.CreateActivityParams{
		SessionID:   sessionID,
		Type:        "agent-action",
		Description: desc,
		AgentID:     agentUUID,
	})
}

func (h *Handler) buildChatHistory(ctx context.Context, sessionID uuid.UUID, hasResume bool) string {
	msgs, err := h.queries.ListMessages(ctx, db.ListMessagesParams{
		SessionID: sessionID,
		Limit:     20,
	})
	if err != nil || len(msgs) == 0 {
		return ""
	}

	// If resuming, only include messages since the last agent response
	if hasResume {
		msgs = msgs[:min(5, len(msgs))]
	}

	var parts []string
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		prefix := "[Human]"
		if m.AuthorType == "agent" {
			prefix = "[Agent]"
		}
		parts = append(parts, prefix+" "+m.Content)
	}
	return strings.Join(parts, "\n")
}
