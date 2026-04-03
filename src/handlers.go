// handlers.go
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func newUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  AppConfig.WebSocket.BufferSize.Read,
		WriteBufferSize: AppConfig.WebSocket.BufferSize.Write,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			if ShouldAllowAllOrigins() {
				if IsDevelopment() {
					log.Printf("🧪 DEV: Allowing origin %s (development mode)", origin)
				} else {
					log.Printf("⚠️  WARNING: Allowing all origins in production!")
				}
				return true
			}

			return IsOriginAllowed(origin)
		},
	}
}

func writeWebSocketAuthError(conn Conn, message string) {
	_ = conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
	if err := conn.WriteJSON(map[string]string{
		"type":    "auth_error",
		"message": message,
	}); err != nil {
		log.Printf("failed to send websocket auth error: %v", err)
	}
}

func decodeMessageRequest(body []byte) (*MessageRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	var req MessageRequest
	if err := decoder.Decode(&req); err != nil {
		return nil, err
	}

	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("request body must contain a single JSON object")
	}

	req.Normalize()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	return &req, nil
}

func decodeAuthMessage(body []byte) (*AuthMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	var authMsg AuthMessage
	if err := decoder.Decode(&authMsg); err != nil {
		return nil, err
	}

	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("auth payload must contain a single JSON object")
	}

	authMsg.Normalize()
	return &authMsg, nil
}

// handleWebSocket handles WebSocket connections
func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
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
	upgrader := newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Failed to upgrade connection: %v", err)
		return
	}

	// Create a new client
	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, AppConfig.Limits.SendChannelBuffer),
	}

	// Set initial read deadline for authentication
	conn.SetReadLimit(AppConfig.WebSocket.AuthMaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.ReadDeadline))

	// First message MUST be authentication
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Printf("❌ Failed to read auth message: %v", err)
		conn.Close()
		return
	}

	authMsg, err := decodeAuthMessage(message)
	if err != nil {
		log.Printf("❌ Failed to unmarshal auth message: %v", err)
		writeWebSocketAuthError(conn, "Invalid auth payload")
		conn.Close()
		return
	}

	if authMsg.Type != "auth" {
		log.Printf("❌ Wrong message type: got '%s', expected 'auth'", authMsg.Type)
		writeWebSocketAuthError(conn, "First websocket message must be auth")
		conn.Close()
		return
	}

	// Authenticate the client
	if err := client.authenticate(*authMsg); err != nil {
		log.Printf("❌ Authentication failed: %v", err)
		writeWebSocketAuthError(conn, err.Error())
		conn.Close()
		return
	}

	// Check team-specific client limits
	if !hub.canAddClient(client.teamID) {
		log.Printf("❌ Team client limit reached for team %s", client.teamID)
		writeWebSocketAuthError(conn, "Team client limit reached")
		conn.Close()
		return
	}

	// Register client first
	hub.register <- client

	// Send success response
	_ = conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, AppConfig.WebSocket.MaxMessageSize)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	req, err := decodeMessageRequest(body)
	if err != nil {
		log.Printf("❌ Invalid JSON: %v", err)
		switch {
		case errors.Is(err, io.EOF):
			http.Error(w, "Request body is required", http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	log.Printf(
		"📨 send request: type=%s broadcast=%t team=%s target_user=%s body_bytes=%d",
		req.MessageType,
		req.Broadcast,
		req.TargetTeamID,
		req.TargetUserID,
		len(req.Body),
	)

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
		// Send to a specific user. If no team is provided, deliver to all connected sessions for that user.
		delivered = hub.sendToUser(req.TargetTeamID, req.TargetUserID, messageJSON)
		success = delivered > 0
		if success {
			log.Printf("📤 Message sent to user %s in team %s (%d recipients)", req.TargetUserID, req.TargetTeamID, delivered)
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
