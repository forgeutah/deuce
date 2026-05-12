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

// ServeWS upgrades an HTTP connection to WebSocket and starts the client
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:4000", "localhost:8080"},
	})
	if err != nil {
		slog.Error("websocket accept error", "error", err)
		return
	}

	client := NewClient(hub, conn, userID)
	hub.register <- client

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}
