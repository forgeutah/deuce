package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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

	// Process agent mentions
	if len(req.Mentions) > 0 {
		go h.processAgentMentions(sessionID, req.Mentions)
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) processAgentMentions(sessionID uuid.UUID, mentions []string) {
	ctx := context.Background()
	for _, mention := range mentions {
		agentID, err := uuid.Parse(mention)
		if err != nil {
			continue
		}

		// Get agent info
		agent, err := h.queries.GetAgent(ctx, agentID)
		if err != nil {
			continue
		}

		// Set agent to working
		_ = h.queries.UpdateSessionAgentStatus(ctx, db.UpdateSessionAgentStatusParams{
			SessionID: sessionID,
			AgentID:   agentID,
			Status:    "working",
		})

		statusMsg, _ := ws.NewServerMessage(ws.TypeAgentStatus, sessionID.String(), map[string]any{
			"agentId": agentID.String(),
			"status":  "working",
		})
		h.hub.BroadcastToSession(sessionID.String(), statusMsg, nil)

		// Send typing indicator
		typingMsg, _ := ws.NewServerMessage(ws.TypeTypingIndicator, sessionID.String(), map[string]any{
			"agentId": agentID.String(),
			"active":  true,
		})
		h.hub.BroadcastToSession(sessionID.String(), typingMsg, nil)

		// Simulate delay
		delay := time.Duration(1500+rand.Intn(2000)) * time.Millisecond
		time.Sleep(delay)

		// Generate canned response
		content, expandable := getAgentResponse(agent.Role)

		var ec []byte
		if expandable != nil {
			ec, _ = json.Marshal(expandable)
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
			continue
		}

		_ = h.queries.UpdateSessionLastActivity(ctx, sessionID)

		// Stop typing
		stopTyping, _ := ws.NewServerMessage(ws.TypeTypingIndicator, sessionID.String(), map[string]any{
			"agentId": agentID.String(),
			"active":  false,
		})
		h.hub.BroadcastToSession(sessionID.String(), stopTyping, nil)

		// Broadcast agent message to ALL clients
		agentWsMsg, _ := ws.NewServerMessage(ws.TypeNewMessage, sessionID.String(), toMessageResponse(agentMsg))
		h.hub.BroadcastToSession(sessionID.String(), agentWsMsg, nil)

		// Set agent back to idle
		_ = h.queries.UpdateSessionAgentStatus(nil, db.UpdateSessionAgentStatusParams{
			SessionID: sessionID,
			AgentID:   agentID,
			Status:    "idle",
		})

		idleMsg, _ := ws.NewServerMessage(ws.TypeAgentStatus, sessionID.String(), map[string]any{
			"agentId": agentID.String(),
			"status":  "idle",
		})
		h.hub.BroadcastToSession(sessionID.String(), idleMsg, nil)

		// Create activity
		activityDesc := agent.Name + " completed a task"
		agentUUID := pgtype.UUID{Bytes: agentID, Valid: true}
		_, _ = h.queries.CreateActivity(ctx, db.CreateActivityParams{
			SessionID:   sessionID,
			Type:        "agent-action",
			Description: activityDesc,
			AgentID:     agentUUID,
		})
	}
}

func getAgentResponse(role string) (string, []map[string]string) {
	switch role {
	case "coder":
		return "I've updated the implementation. The changes include proper error handling and input validation.",
			[]map[string]string{{
				"type":    "diff",
				"title":   "changes",
				"summary": "auth.go (+12 -3)",
				"content": "@@ -42,8 +42,19 @@ func Validate(token string) error {\n     return ErrInvalidFormat\n   }\n\n+  // Check token expiration\n+  claims, err := ParseClaims(token)\n+  if err != nil {\n+    return fmt.Errorf(\"parse claims: %w\", err)\n+  }\n+\n+  if claims.ExpiresAt.Before(time.Now()) {\n+    return ErrTokenExpired\n+  }\n+\n   return nil\n }",
			}}
	case "reviewer":
		return "I've reviewed the changes. The code looks clean, but I have a few suggestions for improvement.", nil
	case "planner":
		return "Here's the implementation plan broken down into phases. Each phase has clear deliverables and success criteria.", nil
	case "tester":
		return "All tests are passing. I've added new test cases covering the edge cases we discussed.",
			[]map[string]string{{
				"type":    "test-results",
				"title":   "test results",
				"summary": "4/4 passing",
				"content": "=== RUN   TestValidate\n--- PASS: TestValidate (0.00s)\n=== RUN   TestValidateExpired\n--- PASS: TestValidateExpired (0.00s)\n=== RUN   TestValidateInvalid\n--- PASS: TestValidateInvalid (0.00s)\n=== RUN   TestValidateEmpty\n--- PASS: TestValidateEmpty (0.00s)\nPASS\nok  \tforge-api/auth\t0.003s",
			}}
	case "designer":
		return "I've analyzed the UI and have some suggestions for improving the user experience.", nil
	default:
		return "Task completed successfully.", nil
	}
}
