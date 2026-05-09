package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// HandleTerminalWebSocket upgrades to a WebSocket and bridges binary I/O
// between the client (xterm.js) and a PTY session attached to devpod ssh.
//
// Wire protocol:
//   - 0x00 + data: terminal I/O (stdin from client, stdout to client)
//   - 0x01 + JSON: control messages (e.g., {"cols":80,"rows":24} for resize)
func (h *Handler) HandleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionIDStr := chi.URLParam(r, "sessionID")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}

	// Look up session to get workspace name and verify status
	session, err := h.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return
	}

	if session.WorkspaceStatus != "ready" {
		writeError(w, http.StatusConflict, "WORKSPACE_NOT_READY", "workspace is not ready")
		return
	}

	// Get or create the terminal session
	termSession, err := h.terminals.GetOrCreate(sessionID.String(), func() *exec.Cmd {
		return h.workspaces.SSHCommand(r.Context(), session.Name)
	})
	if err != nil {
		slog.Error("failed to create terminal session", "sessionID", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "TERMINAL_FAILED", "failed to start terminal")
		return
	}

	// Accept WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:5173", "localhost:8080"},
	})
	if err != nil {
		slog.Error("terminal websocket accept error", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Create a writer that sends PTY output to this WebSocket client with 0x00 prefix
	clientWriter := &wsWriter{conn: conn, ctx: ctx}
	termSession.AddClient(clientWriter)
	defer termSession.RemoveClient(clientWriter)

	slog.Info("terminal client connected", "sessionID", sessionID)

	// Read from WebSocket and dispatch to PTY or control handler
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				slog.Info("terminal websocket closed", "sessionID", sessionID)
			} else {
				slog.Debug("terminal websocket read error", "sessionID", sessionID, "error", err)
			}
			return
		}

		if msgType != websocket.MessageBinary || len(data) < 1 {
			continue
		}

		switch data[0] {
		case 0x00: // Terminal data → PTY stdin
			if len(data) > 1 {
				termSession.Write(data[1:])
			}
		case 0x01: // Control message (resize)
			if len(data) > 1 {
				var resize struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if err := json.Unmarshal(data[1:], &resize); err == nil && resize.Cols > 0 && resize.Rows > 0 {
					termSession.Resize(resize.Cols, resize.Rows)
				}
			}
		}
	}
}

// wsWriter wraps a WebSocket connection as an io.Writer, prepending
// the 0x00 data prefix to each write.
type wsWriter struct {
	conn *websocket.Conn
	ctx  context.Context
}

func (w *wsWriter) Write(p []byte) (int, error) {
	msg := make([]byte, 1+len(p))
	msg[0] = 0x00
	copy(msg[1:], p)
	err := w.conn.Write(w.ctx, websocket.MessageBinary, msg)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
