package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/ws"
)

func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)

	ws.ServeWS(h.hub, w, r, userID)
}

// SetupWSCallbacks configures the WebSocket client callbacks (called after client creation)
func (h *Handler) SetupWSCallbacks(client *ws.Client) {
	client.OnMarkRead = func(c *ws.Client, sessionID string) {
		sid, err := uuid.Parse(sessionID)
		if err != nil {
			return
		}
		uid, err := uuid.Parse(c.UserID)
		if err != nil {
			return
		}
		_ = h.queries.MarkSessionRead(context.Background(), db.MarkSessionReadParams{
			SessionID: sid,
			UserID:    uid,
		})
	}
}
