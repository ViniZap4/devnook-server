package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

// Event is a typed message sent over WebSocket.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// ClientMessage is a message received from a client.
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// CallSignal is the data payload for call-related messages.
type CallSignal struct {
	TargetUserID   int64  `json:"target_user_id"`
	ConversationID int64  `json:"conversation_id"`
	SDP            string `json:"sdp,omitempty"`
	Candidate      string `json:"candidate,omitempty"`
}

// routedMessage is an internal message from a client that needs routing.
type routedMessage struct {
	SenderUserID int64
	Message      ClientMessage
}

type Client struct {
	UserID    int64
	Username  string
	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once
}

type Hub struct {
	// userClients maps user ID → set of connected clients
	userClients map[int64]map[*Client]bool
	broadcast   chan []byte
	register    chan *Client
	unregister  chan *Client
	route       chan routedMessage
	mu          sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		userClients: make(map[int64]map[*Client]bool),
		broadcast:   make(chan []byte, 256),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		route:       make(chan routedMessage, 256),
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for _, clients := range h.userClients {
				for client := range clients {
					client.closeOnce.Do(func() { close(client.send) })
				}
			}
			h.userClients = make(map[int64]map[*Client]bool)
			h.mu.Unlock()
			return
		case client := <-h.register:
			h.mu.Lock()
			firstConnection := h.userClients[client.UserID] == nil

			// Build presence_list BEFORE adding the new client to avoid self-inclusion.
			onlineUsernames := make([]string, 0, len(h.userClients))
			for _, clients := range h.userClients {
				for c := range clients {
					if c.Username != "" {
						onlineUsernames = append(onlineUsernames, c.Username)
						break
					}
				}
			}

			if firstConnection {
				h.userClients[client.UserID] = make(map[*Client]bool)
			}
			h.userClients[client.UserID][client] = true
			h.mu.Unlock()

			// Send presence_list to the new client.
			if data, err := json.Marshal(Event{
				Type: "presence_list",
				Data: map[string]any{"usernames": onlineUsernames},
			}); err == nil {
				select {
				case client.send <- data:
				default:
				}
			}

			// If this is the user's first connection, broadcast presence_update to all OTHER users.
			if firstConnection && client.Username != "" {
				if data, err := json.Marshal(Event{
					Type: "presence_update",
					Data: map[string]any{"username": client.Username, "online": true},
				}); err == nil {
					h.mu.RLock()
					for uid, clients := range h.userClients {
						if uid == client.UserID {
							continue
						}
						for c := range clients {
							select {
							case c.send <- data:
							default:
							}
						}
					}
					h.mu.RUnlock()
				}
			}

		case client := <-h.unregister:
			h.mu.Lock()
			userGoneOffline := false
			if clients, ok := h.userClients[client.UserID]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					client.closeOnce.Do(func() { close(client.send) })
					if len(clients) == 0 {
						delete(h.userClients, client.UserID)
						userGoneOffline = true
					}
				}
			}
			h.mu.Unlock()

			// Broadcast offline status only when the user has no more active connections.
			if userGoneOffline && client.Username != "" {
				if data, err := json.Marshal(Event{
					Type: "presence_update",
					Data: map[string]any{"username": client.Username, "online": false},
				}); err == nil {
					h.mu.RLock()
					for _, clients := range h.userClients {
						for c := range clients {
							select {
							case c.send <- data:
							default:
							}
						}
					}
					h.mu.RUnlock()
				}
			}

		case msg := <-h.broadcast:
			h.mu.RLock()
			var stale []*Client
			for _, clients := range h.userClients {
				for client := range clients {
					select {
					case client.send <- msg:
					default:
						stale = append(stale, client)
					}
				}
			}
			h.mu.RUnlock()
			// Clean up stale clients under write lock
			if len(stale) > 0 {
				h.mu.Lock()
				for _, client := range stale {
					if clients, ok := h.userClients[client.UserID]; ok {
						if _, exists := clients[client]; exists {
							delete(clients, client)
							client.closeOnce.Do(func() { close(client.send) })
							if len(clients) == 0 {
								delete(h.userClients, client.UserID)
							}
						}
					}
				}
				h.mu.Unlock()
			}
		case rm := <-h.route:
			go h.handleClientMessage(rm)
		}
	}
}

// handleClientMessage routes incoming client messages (call signaling).
func (h *Hub) handleClientMessage(rm routedMessage) {
	switch rm.Message.Type {
	case "call_offer", "call_answer", "call_ice_candidate", "call_end", "call_reject":
		var signal CallSignal
		if err := json.Unmarshal(rm.Message.Data, &signal); err != nil {
			log.Printf("ws: failed to unmarshal call signal: %v", err)
			return
		}
		// Forward to target user with sender info
		h.SendToUser(signal.TargetUserID, Event{
			Type: rm.Message.Type,
			Data: map[string]any{
				"sender_user_id":  rm.SenderUserID,
				"conversation_id": signal.ConversationID,
				"sdp":             signal.SDP,
				"candidate":       signal.Candidate,
			},
		})
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

// SendToUser sends a typed event to all connections of a specific user.
func (h *Hub) SendToUser(userID int64, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("ws: failed to marshal event %q: %v", event.Type, err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.userClients[userID]; ok {
		for client := range clients {
			select {
			case client.send <- data:
			default:
			}
		}
	}
}

// SendToUsers sends a typed event to multiple users.
func (h *Hub) SendToUsers(userIDs []int64, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("ws: failed to marshal event %q: %v", event.Type, err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, uid := range userIDs {
		if clients, ok := h.userClients[uid]; ok {
			for client := range clients {
				select {
				case client.send <- data:
				default:
				}
			}
		}
	}
}

// GetOnlineUsernames returns a snapshot of all currently online usernames.
func (h *Hub) GetOnlineUsernames() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	usernames := make([]string, 0, len(h.userClients))
	for _, clients := range h.userClients {
		for c := range clients {
			if c.Username != "" {
				usernames = append(usernames, c.Username)
				break
			}
		}
	}
	return usernames
}

// HandleWebSocket takes a gofiber/contrib websocket connection.
// This function BLOCKS until the connection is closed (required by Fiber's websocket handler).
func (h *Hub) HandleWebSocket(conn *websocket.Conn, userID int64, username string) {
	client := &Client{UserID: userID, Username: username, conn: conn, send: make(chan []byte, 256)}
	h.register <- client

	go client.writePump()
	client.readPump(h) // blocks until connection closes
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		// Parse client message and route it
		var cm ClientMessage
		if err := json.Unmarshal(message, &cm); err != nil {
			continue
		}
		hub.route <- routedMessage{
			SenderUserID: c.UserID,
			Message:      cm,
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
