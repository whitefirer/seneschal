package api

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// WSClient represents a WebSocket client
type WSClient struct {
	hub  *WSHub
	conn *websocket.Conn
	send chan WSProgressEvent
	sub  map[string]bool // subscribed execution IDs
	mu   sync.RWMutex
}

// WSHub manages WebSocket connections
type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan WSProgressEvent
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
}

// NewWSHub creates a new WebSocket hub
func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan WSProgressEvent, 512), // Increased buffer
		register:   make(chan *WSClient, 16),
		unregister: make(chan *WSClient, 16),
	}
}

// Run starts the hub
func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Check if client is subscribed to this execution
				client.mu.RLock()
				subscribed := len(client.sub) == 0 || client.sub[event.ExecutionID]
				client.mu.RUnlock()

				if subscribed {
					select {
					case client.send <- event:
					default:
						// Client buffer full, disconnect
						go func(c *WSClient) {
							h.unregister <- c
						}(client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Subscribe adds an execution ID to the client's subscription
func (c *WSClient) Subscribe(executionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sub == nil {
		c.sub = make(map[string]bool)
	}
	c.sub[executionID] = true
}

// Unsubscribe removes an execution ID from the client's subscription
func (c *WSClient) Unsubscribe(executionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sub, executionID)
}

// WritePump pumps messages from the hub to the WebSocket connection
func (c *WSClient) WritePump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *WSClient) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(65536)

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// Log error if needed
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		// Handle client messages
		switch msg.Type {
		case "subscribe":
			if executionID, ok := msg.Data["executionId"].(string); ok {
				c.Subscribe(executionID)
			}
		case "unsubscribe":
			if executionID, ok := msg.Data["executionId"].(string); ok {
				c.Unsubscribe(executionID)
			}
		}
	}
}

// Broadcast sends an event to all connected clients
func (h *WSHub) Broadcast(event WSProgressEvent) {
	select {
	case h.broadcast <- event:
	default:
		// Broadcast channel full, skip
	}
}
