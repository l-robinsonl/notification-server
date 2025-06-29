// handlers.go
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  0, // Will be set from config
	WriteBufferSize: 0, // Will be set from config
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		
		// Environment-aware origin checking
		if ShouldAllowAllOrigins() {
			if IsDevelopment() {
				log.Printf("🧪 DEV: Allowing origin %s (development mode)", origin)
			} else {
				log.Printf("⚠️  WARNING: Allowing all origins in production!")
			}
			return true
		}
		
		// Production-safe origin checking
		return IsOriginAllowed(origin)
	},
}

func initUpgrader() {
	if AppConfig == nil {
		log.Fatal("Config must be loaded before initializing upgrader")
	}
	
	upgrader.ReadBufferSize = AppConfig.WebSocket.BufferSize.Read
	upgrader.WriteBufferSize = AppConfig.WebSocket.BufferSize.Write
	
	if IsDevelopment() {
		log.Printf("🧪 WebSocket upgrader initialized for DEVELOPMENT")
		log.Printf("🧪 CORS policy: %s", func() string {
			if ShouldAllowAllOrigins() {
				return "Allow all origins"
			}
			return "Restricted origins only"
		}())
	} else {
		log.Printf("🔒 WebSocket upgrader initialized for PRODUCTION")
		log.Printf("🔒 CORS policy: Restricted to allowed origins only")
	}
}

// handleWebSocket handles WebSocket connections
func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// Ensure upgrader is configured
	if upgrader.ReadBufferSize == 0 {
		initUpgrader()
	}

	// Check if we can accept more clients (optional global limit)
	totalClients := hub.getTotalClientCount()
	maxGlobalClients := AppConfig.Limits.MaxClientsPerTeam * 100 // Rough global limit
	if totalClients >= maxGlobalClients {
		log.Printf("❌ Global client limit reached: %d", totalClients)
		http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Failed to upgrade connection: %v", err)
		return
	}

	// Create a new client
	client := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, AppConfig.Limits.SendChannelBuffer),
		isActive: true,
	}

	// Set initial read deadline for authentication
	conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.ReadDeadline))

	// First message MUST be authentication
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Printf("❌ Failed to read auth message: %v", err)
		conn.Close()
		return
	}

	log.Printf("📨 Received auth message: %s", message)

	var authMsg AuthMessage
	if err := json.Unmarshal(message, &authMsg); err != nil {
		log.Printf("❌ Failed to unmarshal auth message: %v", err)
		log.Printf("❌ Raw bytes: %v", message)
		conn.Close()
		return
	}

	if authMsg.Type != "auth" {
		log.Printf("❌ Wrong message type: got '%s', expected 'auth'", authMsg.Type)
		conn.Close()
		return
	}

	// Authenticate the client
	if err := client.authenticate(authMsg); err != nil {
		log.Printf("❌ Authentication failed: %v", err)
		conn.WriteJSON(map[string]interface{}{
			"type":    "auth_error",
			"message": err.Error(),
		})
		conn.Close()
		return
	}

	// Check team-specific client limits
	if !hub.canAddClient(client.teamID) {
		log.Printf("❌ Team client limit reached for team %s", client.teamID)
		conn.WriteJSON(map[string]interface{}{
			"type":    "auth_error",
			"message": "Team client limit reached",
		})
		conn.Close()
		return
	}

	// Register client first
	hub.register <- client

	// Send success response
	conn.WriteJSON(map[string]interface{}{
		"type":    "authSuccess",
		"message": "Successfully authenticated",
	})

	// Clear read deadline and start normal operation
	conn.SetReadDeadline(time.Time{})

	// Start the client's read and write pumps
	go client.writePump()
	go client.readPump()

	log.Printf("✅ New WebSocket connection: team=%s, user=%s", client.teamID, client.userID)
}

// handleSendMessage handles the REST endpoint for sending messages
func handleSendMessage(hub *Hub, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	log.Printf("📨 Request body: %s", string(body))

	var req MessageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("❌ Invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.MessageType == "" {
		http.Error(w, "Missing required field: MessageType", http.StatusBadRequest)
		return
	}

	if req.Broadcast {
			// For broadcasts, TargetUserID is not allowed (broadcasts can't target individual users)
			if req.TargetUserID != "" {
					http.Error(w, "Cannot specify TargetUserID when Broadcast is true", http.StatusBadRequest)
					return
			}
			// TargetTeamID is optional for broadcasts:
			// - Empty TargetTeamID = Global broadcast (all teams)
			// - Specified TargetTeamID = Team broadcast (specific team only)
	} else {
			// If it's not a broadcast, a TeamID and UserID are required for direct messages
			if req.TargetTeamID == "" || req.TargetUserID == "" {
					http.Error(w, "Must specify a TeamID and TargetUserID for non-broadcast messages", http.StatusBadRequest)
					return
			}
	}

	// Create the message
	message := NewMessage(req.NotificationID, req.TargetTeamID, req.TargetUserID, req.SenderUserID, req.MessageType, req.Body)
	messageJSON, err := message.ToJSON()
	if err != nil {
		log.Printf("❌ Error encoding message: %v", err)
		http.Error(w, "Error encoding message", http.StatusInternalServerError)
		return
	}

	var delivered int
	var success bool

	// Determine delivery method based on request parameters
	if req.Broadcast {
		if req.TargetTeamID != "" {
			// Team-specific broadcast: send to all users in the specified team
			delivered = hub.broadcastToTeam(req.TargetTeamID, messageJSON)
			success = delivered > 0
			log.Printf("🎯 Team broadcast to %s: %d recipients", req.TargetTeamID, delivered)
		} else {
			// Global broadcast: send to all users in all teams
			delivered = hub.broadcastToAllTeams(messageJSON)
			success = delivered > 0
			log.Printf("🌍 Global broadcast message: %d recipients across all teams", delivered)
		}
	} else {
		// Send to specific user in specific team
		success = hub.sendToUser(req.TargetTeamID, req.TargetUserID, messageJSON)
		if success {
			delivered = 1
			log.Printf("📤 Message sent to user %s in team %s", req.TargetUserID, req.TargetTeamID)
		} else {
			log.Printf("❌ Failed to send message to user %s in team %s (user not connected)", req.TargetUserID, req.TargetTeamID)
		}
	}

	// Return the result
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success":   success,
		"delivered": delivered,
	}
	json.NewEncoder(w).Encode(response)
}