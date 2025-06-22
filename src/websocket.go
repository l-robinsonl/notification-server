// websocket.go
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Circuit breaker for backend calls
type CircuitBreaker struct {
	failures    int
	lastFailure time.Time
	mu          sync.RWMutex
}

var backendCircuitBreaker = &CircuitBreaker{}

func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit is open
	if cb.failures >= AppConfig.CircuitBreaker.Threshold {
		if time.Since(cb.lastFailure) < AppConfig.CircuitBreaker.Timeout {
			return errors.New("circuit breaker open - backend unavailable")
		}
		// Reset after timeout
		cb.failures = 0
	}

	err := fn()
	if err != nil {
		cb.failures++
		cb.lastFailure = time.Now()
		return err
	}

	// Reset failures on success
	if cb.failures > 0 {
		cb.failures = 0
	}
	return nil
}

// Client represents a connected websocket client
type Client struct {
	hub             *Hub
	conn            Conn 
	send            chan []byte
	teamID          string
	userID          string
	isActive        bool
	email           string
	isAuthenticated bool
	mu              sync.RWMutex
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(AppConfig.WebSocket.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ WebSocket error: %v", err)
			}
			break
		}
		// Note: we're ignoring the message content since clients don't send anything meaningful
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(AppConfig.WebSocket.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if !ok {
				// The hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) authenticate(authMsg AuthMessage) error {
	// DEVELOPMENT ONLY: Check for fake authentication
	if IsFakeAuthEnabled() && authMsg.Token == "fake_development_token" {
		log.Printf("🧪 DEVELOPMENT: Using fake authentication for %s", authMsg.UserID)
		
		c.mu.Lock()
		c.userID = authMsg.UserID
		c.email = fmt.Sprintf("fake_%s@example.com", authMsg.UserID)
		c.teamID = authMsg.TeamID
		c.isAuthenticated = true
		c.mu.Unlock()
		
		log.Printf("✅ FAKE Client authenticated: user=%s, team=%s", c.userID, c.teamID)
		return nil
	}
	
	// Reject fake tokens in production
	if authMsg.Token == "fake_development_token" {
		log.Printf("❌ SECURITY: Fake token rejected in production mode")
		return errors.New("invalid authentication token")
	}
	
	return backendCircuitBreaker.Call(func() error {
		// Make request to main backend
		req, err := http.NewRequest("GET", AppConfig.Backend.URL+"/rest-auth/user/", nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+authMsg.Token)

		res, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		switch res.StatusCode {
		case 401:
			return errors.New("invalid JWT token provided")
		case 200:
			var userData UserData
			bodyBytes, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}

			err = json.Unmarshal(bodyBytes, &userData)
			if err != nil {
				return err
			}

			log.Printf("🔑 Authenticated team ID: %s", authMsg.TeamID)

			// Set client authentication data
			c.mu.Lock()
			c.userID = strconv.Itoa(userData.ID)
			c.email = userData.Email
			c.teamID = authMsg.TeamID
			c.isAuthenticated = true
			c.mu.Unlock()

			log.Printf("✅ Client authenticated: user=%d, email=%s, team=%s",
				userData.ID, userData.Email, authMsg.TeamID)

			return nil
		default:
			return errors.New("authentication failed with status: " + res.Status)
		}
	})
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients by team and user
	clients    map[string]map[string]*Client
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[string]map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// run processes client registrations and unregistrations
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// Initialize team map if it doesn't exist
			if _, ok := h.clients[client.teamID]; !ok {
				h.clients[client.teamID] = make(map[string]*Client)
			}
			h.clients[client.teamID][client.userID] = client
			h.mu.Unlock()

			log.Printf("✅ Client registered: team=%s, user=%s", client.teamID, client.userID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.teamID]; ok {
				if _, ok := h.clients[client.teamID][client.userID]; ok {
					delete(h.clients[client.teamID], client.userID)
					close(client.send)
					log.Printf("❌ Client unregistered: team=%s, user=%s", client.teamID, client.userID)

					// Clean up empty team maps
					if len(h.clients[client.teamID]) == 0 {
						delete(h.clients, client.teamID)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

// canAddClient checks if we can add another client to a team
func (h *Hub) canAddClient(teamID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if teamClients, ok := h.clients[teamID]; ok {
		return len(teamClients) < AppConfig.Limits.MaxClientsPerTeam
	}
	return true
}

// getTotalClientCount returns the total number of connected clients
func (h *Hub) getTotalClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := 0
	for _, teamClients := range h.clients {
		total += len(teamClients)
	}
	return total
}

// healthCheck returns health information about the hub
func (h *Hub) healthCheck() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	totalClients := 0
	for _, teamClients := range h.clients {
		totalClients += len(teamClients)
	}

	return map[string]interface{}{
		"total_teams":   len(h.clients),
		"total_clients": totalClients,
	}
}

// sendToUser sends a message to a specific user in a team with timeout
func (h *Hub) sendToUser(teamID, userID string, message []byte) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if teamClients, ok := h.clients[teamID]; ok {
		if client, ok := teamClients[userID]; ok {
			select {
			case client.send <- message:
				return true
			case <-time.After(5 * time.Second):
				log.Printf("⏰ Client %s/%s send timeout, will be removed", teamID, userID)
				// Note: We can't safely remove the client here due to the RLock
				// The client will be removed when the connection fails
				return false
			default:
				// If the client's send buffer is full, assume they're gone
				log.Printf("📪 Client %s/%s send buffer full, will be removed", teamID, userID)
				return false
			}
		}
	}
	return false
}

// broadcastToTeam sends a message to all users in a team
func (h *Hub) broadcastToTeam(teamID string, message []byte) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	if teamClients, ok := h.clients[teamID]; ok {
		for userID, client := range teamClients {
			select {
			case client.send <- message:
				count++
			case <-time.After(1 * time.Second):
				log.Printf("⏰ Client %s/%s broadcast timeout", teamID, userID)
			default:
				// If the client's send buffer is full, skip them
				log.Printf("📪 Client %s/%s send buffer full during broadcast", teamID, userID)
			}
		}
	}
	return count
}

// broadcastToAllTeams sends a message to all users across all teams
func (h *Hub) broadcastToAllTeams(message []byte) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for teamID, teamClients := range h.clients {
		for userID, client := range teamClients {
			select {
			case client.send <- message:
				count++
			case <-time.After(1 * time.Second):
				log.Printf("⏰ Client %s/%s global broadcast timeout", teamID, userID)
			default:
				// If the client's send buffer is full, skip them
				log.Printf("📪 Client %s/%s send buffer full during global broadcast", teamID, userID)
			}
		}
	}
	return count
}

// removeClient safely removes a client (called when connection issues occur)
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if teamClients, ok := h.clients[client.teamID]; ok {
		if _, ok := teamClients[client.userID]; ok {
			delete(teamClients, client.userID)
			close(client.send)
			log.Printf("🧹 Client removed due to connection issues: team=%s, user=%s", client.teamID, client.userID)

			// Clean up empty team maps
			if len(teamClients) == 0 {
				delete(h.clients, client.teamID)
			}
		}
	}
}