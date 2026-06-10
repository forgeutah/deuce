package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
	maxMsgSize = 8192
	sendBufLen = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	UserID string

	// onJoin/onLeave/onMarkRead callbacks set by the handler layer
	OnJoin     func(client *Client, sessionID string)
	OnLeave    func(client *Client, sessionID string)
	OnMarkRead func(client *Client, sessionID string)

	// Authorize gates access to a session for join (heavy event subscription)
	// and steer (feeding a live agent run). Set by the handler layer to a
	// session-membership check (KTD14). When nil, access is allowed — production
	// wiring MUST set it; an unset gate is a misconfiguration, not a default.
	Authorize func(userID, sessionID string) bool
	// OnSteer routes a steer reply for a session; set by the handler.
	OnSteer func(client *Client, sessionID, message string)
}

func (c *Client) authorized(sessionID string) bool {
	if c.Authorize == nil {
		return true
	}
	return c.Authorize(c.UserID, sessionID)
}

func NewClient(hub *Hub, conn *websocket.Conn, userID string) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, sendBufLen),
		UserID: userID,
	}
}

// ReadPump reads messages from the WebSocket connection
func (c *Client) ReadPump(ctx context.Context) {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	c.conn.SetReadLimit(maxMsgSize)

	for {
		var msg ClientMessage
		err := wsjson.Read(ctx, c.conn, &msg)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				slog.Info("websocket closed", "userID", c.UserID)
			} else {
				slog.Error("websocket read error", "error", err, "userID", c.UserID)
			}
			return
		}

		switch msg.Type {
		case TypeJoin:
			// Gate the heavy-event subscription on session membership (KTD14):
			// without this, any authenticated user could join an arbitrary
			// session and receive its AgentRunEvent stream.
			if !c.authorized(msg.SessionID) {
				slog.Warn("rejected join: not a session member", "userID", c.UserID, "sessionID", msg.SessionID)
				continue
			}
			c.hub.Subscribe(c, msg.SessionID)
			if c.OnJoin != nil {
				c.OnJoin(c, msg.SessionID)
			}
		case TypeLeave:
			c.hub.Unsubscribe(c, msg.SessionID)
			if c.OnLeave != nil {
				c.OnLeave(c, msg.SessionID)
			}
		case TypeMarkRead:
			if c.OnMarkRead != nil {
				c.OnMarkRead(c, msg.SessionID)
			}
		case TypeSteer:
			if !c.authorized(msg.SessionID) {
				slog.Warn("rejected steer: not a session member", "userID", c.UserID, "sessionID", msg.SessionID)
				continue
			}
			text := msg.Message
			if len(text) > MaxSteerLen {
				text = text[:MaxSteerLen]
			}
			if c.OnSteer != nil {
				c.OnSteer(c, msg.SessionID, text)
			}
		default:
			slog.Warn("unknown message type", "type", msg.Type, "userID", c.UserID)
		}
	}
}

// WritePump writes messages to the WebSocket connection
func (c *Client) WritePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Write(writeCtx, websocket.MessageText, message)
			cancel()
			if err != nil {
				slog.Error("websocket write error", "error", err, "userID", c.UserID)
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				slog.Error("websocket ping error", "error", err, "userID", c.UserID)
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// ServeWS upgrades an HTTP connection to WebSocket and starts the client.
// originPatterns is the allow-list checked against the request's Origin header
// (e.g., {"localhost:4000", "deuce.example.com"}). An empty slice still allows
// same-origin and non-browser upgrades through coder/websocket; only
// cross-origin browser upgrades are denied. Callers that want a real
// allow-list must configure at least one pattern — config.Validate refuses
// to start the server in forge-proxy mode with an empty list.
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, userID string, originPatterns []string, configure func(*Client)) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: originPatterns,
	})
	if err != nil {
		slog.Error("websocket accept error", "error", err)
		return
	}

	client := NewClient(hub, conn, userID)
	// Wire per-connection callbacks (membership gate, steer routing, mark-read)
	// before the client is registered so no event races an unconfigured client.
	if configure != nil {
		configure(client)
	}
	hub.register <- client

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}
