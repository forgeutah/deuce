package ws

import (
	"log/slog"
	"sync"
)

// Hub manages WebSocket connections and broadcasts messages to subscribed clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool

	// sessionSubscriptions maps sessionID to set of clients subscribed to heavy events
	sessionSubscriptions map[string]map[*Client]bool

	// globalClients are subscribed to lightweight events (unread, session updates) for all their sessions
	// Keyed by userID -> set of clients
	userClients map[string]map[*Client]bool

	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:              make(map[*Client]bool),
		sessionSubscriptions: make(map[string]map[*Client]bool),
		userClients:          make(map[string]map[*Client]bool),
		register:             make(chan *Client),
		unregister:           make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			if _, ok := h.userClients[client.UserID]; !ok {
				h.userClients[client.UserID] = make(map[*Client]bool)
			}
			h.userClients[client.UserID][client] = true
			h.mu.Unlock()
			slog.Info("client connected", "userID", client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// Remove from all session subscriptions
				for sid, subs := range h.sessionSubscriptions {
					delete(subs, client)
					if len(subs) == 0 {
						delete(h.sessionSubscriptions, sid)
					}
				}

				// Remove from user clients
				if uc, ok := h.userClients[client.UserID]; ok {
					delete(uc, client)
					if len(uc) == 0 {
						delete(h.userClients, client.UserID)
					}
				}
			}
			h.mu.Unlock()
			slog.Info("client disconnected", "userID", client.UserID)
		}
	}
}

// Subscribe adds a client to a session's heavy event subscription
func (h *Hub) Subscribe(client *Client, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessionSubscriptions[sessionID]; !ok {
		h.sessionSubscriptions[sessionID] = make(map[*Client]bool)
	}
	h.sessionSubscriptions[sessionID][client] = true
	slog.Info("client subscribed to session", "userID", client.UserID, "sessionID", sessionID)
}

// Unsubscribe removes a client from a session's heavy event subscription
func (h *Hub) Unsubscribe(client *Client, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs, ok := h.sessionSubscriptions[sessionID]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.sessionSubscriptions, sessionID)
		}
	}
}

// BroadcastToSession sends a message to all clients subscribed to a session.
// If excludeClient is non-nil, that client is skipped.
func (h *Hub) BroadcastToSession(sessionID string, msg ServerMessage, excludeClient *Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := marshalMessage(msg)
	if err != nil {
		slog.Error("failed to marshal message", "error", err)
		return
	}

	if subs, ok := h.sessionSubscriptions[sessionID]; ok {
		for client := range subs {
			if client == excludeClient {
				continue
			}
			select {
			case client.send <- data:
			default:
				// Client send buffer full, skip
				slog.Warn("client send buffer full, dropping message", "userID", client.UserID)
			}
		}
	}
}

// BroadcastToUser sends a message to all connections for a given userID (lightweight events)
func (h *Hub) BroadcastToUser(userID string, msg ServerMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := marshalMessage(msg)
	if err != nil {
		slog.Error("failed to marshal message", "error", err)
		return
	}

	if clients, ok := h.userClients[userID]; ok {
		for client := range clients {
			select {
			case client.send <- data:
			default:
			}
		}
	}
}

func marshalMessage(msg ServerMessage) ([]byte, error) {
	return marshalJSON(msg)
}
