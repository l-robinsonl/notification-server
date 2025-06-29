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
// type Client struct {
// 	hub             *Hub
// 	conn            Conn 
// 	send            chan []byte
// 	teamID          string
// 	userID          string
// 	isActive        bool
// 	email           string
// 	isAuthenticated bool
// 	mu              sync.RWMutex
// }

type Client struct {
	hub       		*Hub
	conn      			Conn
	send      			chan []byte
	teamID    			string
	userID    			string
	isActive  			bool
	email     		  string
	displayName 		string 
	isAuthenticated bool
	mu							sync.RWMutex
}

func (c *Client) readPump() {
	defer func() {
		log.Printf("🔌 [%s:%s] ReadPump closing - unregistering client", c.teamID, c.userID)
		c.hub.unregister <- c
		c.conn.Close()
	}()

	log.Printf("🔌 [%s:%s] ReadPump started for client", c.teamID, c.userID)

	c.conn.SetReadLimit(AppConfig.WebSocket.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
	c.conn.SetPongHandler(func(string) error {
		log.Printf("🏓 [%s:%s] Received pong from client", c.teamID, c.userID)
		c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ [%s:%s] WebSocket unexpected close error: %v", c.teamID, c.userID, err)
			} else {
				log.Printf("🔌 [%s:%s] WebSocket connection closed: %v", c.teamID, c.userID, err)
			}
			break
		}

		log.Printf("📨 [%s:%s] Received raw message: %s", c.teamID, c.userID, string(message))

		var baseMsg struct {
			Type string `json:"type"`
		}
		
		if err := json.Unmarshal(message, &baseMsg); err != nil {
			log.Printf("❌ [%s:%s] Failed to parse base message: %v, raw: %s", c.teamID, c.userID, err, string(message))
			continue
		}

		log.Printf("🔍 [%s:%s] Parsed message type: %s", c.teamID, c.userID, baseMsg.Type)

		switch baseMsg.Type {
		case "userMessage":
			log.Printf("💬 [%s:%s] Processing user message", c.teamID, c.userID)
			var userMsg struct {
				Type       string `json:"type"`
				Content    string `json:"content"`
				SenderID   string `json:"senderId"`
				SenderName string `json:"senderName"`
				TeamID     string `json:"teamId"`
				Timestamp  string `json:"timestamp"`
			}
			if err := json.Unmarshal(message, &userMsg); err != nil {
				log.Printf("❌ [%s:%s] Failed to parse user message: %v", c.teamID, c.userID, err)
				continue
			}
			log.Printf("💬 [%s:%s] User message details - Content: '%s', Sender: %s (%s), Team: %s", 
				c.teamID, c.userID, userMsg.Content, userMsg.SenderID, userMsg.SenderName, userMsg.TeamID)
			
			// Broadcast to team members
			log.Printf("📡 [%s:%s] Broadcasting user message to team %s", c.teamID, c.userID, userMsg.TeamID)
			c.hub.broadcastToTeam(userMsg.TeamID, message)

		case "privateMessage":
			log.Printf("🔒 [%s:%s] Processing private message", c.teamID, c.userID)
			var privateMsg struct {
				Type        string `json:"type"`
				Content     string `json:"content"`
				SenderID    string `json:"senderId"`
				SenderName  string `json:"senderName"`
				RecipientID string `json:"recipientId"`
				TeamID      string `json:"teamId"`
				Timestamp   string `json:"timestamp"`
			}
			if err := json.Unmarshal(message, &privateMsg); err != nil {
				log.Printf("❌ [%s:%s] Failed to parse private message: %v", c.teamID, c.userID, err)
				continue
			}
			log.Printf("🔒 [%s:%s] Private message details - Content: '%s', Sender: %s (%s), Recipient: %s, Team: %s", 
				c.teamID, c.userID, privateMsg.Content, privateMsg.SenderID, privateMsg.SenderName, privateMsg.RecipientID, privateMsg.TeamID)
			
			// Send to specific recipient
			log.Printf("📤 [%s:%s] Sending private message to recipient %s in team %s", c.teamID, c.userID, privateMsg.RecipientID, privateMsg.TeamID)
			c.hub.sendToUser(privateMsg.TeamID, privateMsg.RecipientID, message)

		case "typingStart":
			log.Printf("⌨️ [%s:%s] Processing typing start", c.teamID, c.userID)
			var typingMsg struct {
				Type        string `json:"type"`
				UserID      string `json:"userId"`
				DisplayName    string `json:"displayName"`
				RecipientID string `json:"recipientId"`
				TeamID      string `json:"teamId"`
			}
			if err := json.Unmarshal(message, &typingMsg); err != nil {
				log.Printf("❌ [%s:%s] Failed to parse typing start message: %v", c.teamID, c.userID, err)
				continue
			}
			log.Printf("⌨️ [%s:%s] Typing start - User: %s (%s), Recipient: %s, Team: %s", 
				c.teamID, c.userID, typingMsg.UserID, typingMsg.DisplayName, typingMsg.RecipientID, typingMsg.TeamID)
			
			if typingMsg.RecipientID != "" {
				// Private typing indicator
				log.Printf("📤 [%s:%s] Sending private typing indicator to %s", c.teamID, c.userID, typingMsg.RecipientID)
				c.hub.sendToUser(typingMsg.TeamID, typingMsg.RecipientID, message)
			} else {
				// Public typing indicator
				log.Printf("📡 [%s:%s] Broadcasting public typing indicator to team %s", c.teamID, c.userID, typingMsg.TeamID)
				c.hub.broadcastToTeam(typingMsg.TeamID, message)
			}

		case "typingStop":
			log.Printf("⌨️ [%s:%s] Processing typing stop", c.teamID, c.userID)
			var typingMsg struct {
				Type        string `json:"type"`
				UserID      string `json:"userId"`
				RecipientID string `json:"recipientId"`
				TeamID      string `json:"teamId"`
			}
			if err := json.Unmarshal(message, &typingMsg); err != nil {
				log.Printf("❌ [%s:%s] Failed to parse typing stop message: %v", c.teamID, c.userID, err)
				continue
			}
			log.Printf("⌨️ [%s:%s] Typing stop - User: %s, Recipient: %s, Team: %s", 
				c.teamID, c.userID, typingMsg.UserID, typingMsg.RecipientID, typingMsg.TeamID)
			
			if typingMsg.RecipientID != "" {
				// Private typing stop
				log.Printf("📤 [%s:%s] Sending private typing stop to %s", c.teamID, c.userID, typingMsg.RecipientID)
				c.hub.sendToUser(typingMsg.TeamID, typingMsg.RecipientID, message)
			} else {
				// Public typing stop
				log.Printf("📡 [%s:%s] Broadcasting public typing stop to team %s", c.teamID, c.userID, typingMsg.TeamID)
				c.hub.broadcastToTeam(typingMsg.TeamID, message)
			}

		case "getOnlineUsers":
			log.Printf("👥 [%s:%s] Processing get online users request", c.teamID, c.userID)
			c.hub.handleGetOnlineUsers(c)

		case "updateDisplayName":
			log.Printf("👤 [%s:%s] Processing display name update", c.teamID, c.userID)
			var updateMsg struct {
				Type        string `json:"type"`
				DisplayName string `json:"displayName"`
			}
			if err := json.Unmarshal(message, &updateMsg); err != nil {
				log.Printf("❌ [%s:%s] Failed to parse display name update: %v", c.teamID, c.userID, err)
				continue
			}
			log.Printf("👤 [%s:%s] Updating display name from '%s' to '%s'", c.teamID, c.userID, c.displayName, updateMsg.DisplayName)
			c.displayName = updateMsg.DisplayName
			
			// Update in online users
			c.hub.mu.Lock()
			if users, ok := c.hub.onlineUsers[c.teamID]; ok {
				if userInfo, exists := users[c.userID]; exists {
					userInfo.DisplayName = c.displayName
					users[c.userID] = userInfo
				}
			}
			c.hub.mu.Unlock()
			
			// Broadcast updated online users to team
			log.Printf("📡 [%s:%s] Broadcasting updated online users to team", c.teamID, c.userID)
			c.hub.broadcastOnlineUsersToTeam(c.teamID)

		default:
			log.Printf("❓ [%s:%s] Unknown message type: %s, raw message: %s", c.teamID, c.userID, baseMsg.Type, string(message))
		}
	}

	log.Printf("🔌 [%s:%s] ReadPump finished", c.teamID, c.userID)
}
// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(AppConfig.WebSocket.PingPeriod)
	defer func() {
		log.Printf("🔌 [%s:%s] WritePump closing", c.teamID, c.userID)
		ticker.Stop()
		c.conn.Close()
	}()

	log.Printf("🔌 [%s:%s] WritePump started for client", c.teamID, c.userID)

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if !ok {
				// The hub closed the channel
				log.Printf("🔌 [%s:%s] Send channel closed by hub - sending close message", c.teamID, c.userID)
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			log.Printf("📤 [%s:%s] Sending message: %s", c.teamID, c.userID, string(message))

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("❌ [%s:%s] Failed to get next writer: %v", c.teamID, c.userID, err)
				return
			}

			if _, err := w.Write(message); err != nil {
				log.Printf("❌ [%s:%s] Failed to write primary message: %v", c.teamID, c.userID, err)
				return
			}

			// Add queued messages to the current websocket message
			n := len(c.send)
			if n > 0 {
				log.Printf("📦 [%s:%s] Adding %d queued messages to current write", c.teamID, c.userID, n)
			}
			for i := 0; i < n; i++ {
				queuedMsg := <-c.send
				log.Printf("📦 [%s:%s] Adding queued message %d/%d: %s", c.teamID, c.userID, i+1, n, string(queuedMsg))
				if _, err := w.Write(newline); err != nil {
					log.Printf("❌ [%s:%s] Failed to write newline for queued message %d: %v", c.teamID, c.userID, i+1, err)
					return
				}
				if _, err := w.Write(queuedMsg); err != nil {
					log.Printf("❌ [%s:%s] Failed to write queued message %d: %v", c.teamID, c.userID, i+1, err)
					return
				}
			}

			if err := w.Close(); err != nil {
				log.Printf("❌ [%s:%s] Failed to close writer: %v", c.teamID, c.userID, err)
				return
			}

			log.Printf("✅ [%s:%s] Successfully sent message with %d queued messages", c.teamID, c.userID, n)

		case <-ticker.C:
			log.Printf("🏓 [%s:%s] Sending ping to client", c.teamID, c.userID)
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("❌ [%s:%s] Failed to send ping: %v", c.teamID, c.userID, err)
				return
			}
			log.Printf("✅ [%s:%s] Ping sent successfully", c.teamID, c.userID)
		}
	}
}

func (h *Hub) broadcastOnlineUsersToTeam(teamID string) {
	h.mu.RLock()
	users := make([]UserInfo, 0)
	if teamUsers, ok := h.onlineUsers[teamID]; ok {
		for _, userInfo := range teamUsers {
			users = append(users, userInfo)
		}
	}
	
	clients := h.clients[teamID]
	h.mu.RUnlock()

	if len(users) == 0 {
		return
	}

	message := OnlineUsersMessage{
		Type:   "onlineUsers",
		Users:  users,
		TeamID: teamID,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling online users message: %v", err)
		return
	}

	for _, client := range clients {
		select {
		case client.send <- messageBytes:
		default:
			// Client's send channel is blocked, skip
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
		c.displayName = authMsg.DisplayName
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

type UserJoinedMessage struct {
	Type     string `json:"type"`
	UserID   string `json:"userId"`
	DisplayName string `json:"displayName,omitempty"`
	TeamID   string `json:"teamId"`
}

type UserLeftMessage struct {
	Type     string `json:"type"`
	UserID   string `json:"userId"`
	DisplayName string `json:"displayName,omitempty"`
	TeamID   string `json:"teamId"`
}

type OnlineUsersMessage struct {
	Type  string     `json:"type"`
	Users []UserInfo `json:"users"`
	TeamID string    `json:"teamId"`
}

type GetOnlineUsersMessage struct {
	Type string `json:"type"`
}

type UserInfo struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	JoinedAt    time.Time `json:"joinedAt"`
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients by team and user
	clients    map[string]map[string]*Client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	onlineUsers map[string]map[string]UserInfo 
}

func newHub() *Hub {
	return &Hub{
		broadcast:   make(chan []byte),
		clients:    make(map[string]map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		onlineUsers: make(map[string]map[string]UserInfo),
		
	}
}

// Helper function to get display name
func getDisplayName(client *Client) string {
	if client.displayName != "" {
		return client.displayName
	}
	if client.email != "" {
		return client.email
	}
	return client.userID
}

// Broadcast user joined to team members
func (h *Hub) broadcastUserJoined(joinedClient *Client) {
	message := UserJoinedMessage{
		Type:     "userJoined",
		UserID:   joinedClient.userID,
		DisplayName: getDisplayName(joinedClient),
		TeamID:   joinedClient.teamID,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling user joined message: %v", err)
		return
	}

	// Send to all clients in the same team
	h.mu.RLock()
	if clients, ok := h.clients[joinedClient.teamID]; ok {
		for userID, client := range clients {
			// Don't send to the user who just joined
			if userID != joinedClient.userID {
				select {
				case client.send <- messageBytes:
				default:
					// Client's send channel is blocked, skip
				}
			}
		}
	}
	h.mu.RUnlock()
}

// Broadcast user left to team members
func (h *Hub) broadcastUserLeft(leftClient *Client) {
	message := UserLeftMessage{
		Type:     "userLeft",
		UserID:   leftClient.userID,
		DisplayName: getDisplayName(leftClient),
		TeamID:   leftClient.teamID,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling user left message: %v", err)
		return
	}

	// Send to all remaining clients in the team
	h.mu.RLock()
	if clients, ok := h.clients[leftClient.teamID]; ok {
		for _, client := range clients {
			select {
			case client.send <- messageBytes:
			default:
				// Client's send channel is blocked, skip
			}
		}
	}
	h.mu.RUnlock()
}

// Send current online users to a specific client
func (h *Hub) sendOnlineUsersToClient(client *Client) {
	h.mu.RLock()
	users := make([]UserInfo, 0)
	if teamUsers, ok := h.onlineUsers[client.teamID]; ok {
		for _, userInfo := range teamUsers {
			users = append(users, userInfo)
		}
	}
	h.mu.RUnlock()

	message := OnlineUsersMessage{
		Type:   "onlineUsers",
		Users:  users,
		TeamID: client.teamID,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling online users message: %v", err)
		return
	}

	select {
	case client.send <- messageBytes:
	default:
		// Client's send channel is blocked
	}
}

// Handle request for online users
func (h *Hub) handleGetOnlineUsers(client *Client) {
	h.sendOnlineUsersToClient(client)
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
			if h.onlineUsers[client.teamID] == nil {
				h.onlineUsers[client.teamID] = make(map[string]UserInfo)
			}

			// Add client
			h.clients[client.teamID][client.userID] = client

			// Add to online users
			h.onlineUsers[client.teamID][client.userID] = UserInfo{
				UserID:      client.userID,
				DisplayName: getDisplayName(client), // Helper function
				Email:       client.email,
				JoinedAt:    time.Now(),
			}
			h.mu.Unlock()
			
			log.Printf("✅ Client registered: team=%s, user=%s", client.teamID, client.userID)
		// Broadcast user joined to team
			h.broadcastUserJoined(client)
			// Send current online users to the new client
			h.sendOnlineUsersToClient(client)
			case client := <-h.unregister:
				h.mu.Lock()
				if clients, ok := h.clients[client.teamID]; ok {
					if _, ok := clients[client.userID]; ok {
						delete(clients, client.userID)
						close(client.send)

						// Remove from online users
						if users, exists := h.onlineUsers[client.teamID]; exists {
							delete(users, client.userID)
						}

						// Clean up empty team
						if len(clients) == 0 {
							delete(h.clients, client.teamID)
							delete(h.onlineUsers, client.teamID)
						}

						h.mu.Unlock()

						// Broadcast user left to team
						h.broadcastUserLeft(client)
					} else {
						h.mu.Unlock()
					}
				} else {
					h.mu.Unlock()
				}
			case message := <-h.broadcast:
				h.mu.RLock()
				for teamID := range h.clients {
					for _, client := range h.clients[teamID] {
						select {
						case client.send <- message:
						default:
							close(client.send)
							delete(h.clients[teamID], client.userID)
							if users, exists := h.onlineUsers[teamID]; exists {
								delete(users, client.userID)
							}
						}
					}
				}
				h.mu.RUnlock()
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

func (h *Hub) broadcastToTeam(teamID string, message []byte) int {
	log.Printf("📡 Starting broadcast to team %s", teamID)
	log.Printf("📡 Message content: %s", string(message))
	
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	count := 0
	if teamClients, ok := h.clients[teamID]; ok {
		log.Printf("📡 Found %d clients in team %s", len(teamClients), teamID)
		
		for userID, client := range teamClients {
			log.Printf("📤 Attempting to send to client %s:%s", teamID, userID)
			
			select {
			case client.send <- message:
				count++
				log.Printf("✅ Message sent successfully to %s:%s", teamID, userID)
			case <-time.After(1 * time.Second):
				log.Printf("⏰ Client %s/%s broadcast timeout", teamID, userID)
			default:
				// If the client's send buffer is full, skip them
				log.Printf("📪 Client %s/%s send buffer full during broadcast", teamID, userID)
			}
		}
		
		log.Printf("📡 Broadcast completed - sent to %d/%d clients in team %s", count, len(teamClients), teamID)
	} else {
		log.Printf("❌ Team %s not found in clients map", teamID)
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