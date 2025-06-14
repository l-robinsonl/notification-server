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
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// handleWebSocket handles WebSocket connections
func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade connection:", err)
		return
	}

	// Create a new client
	client := &Client{
		// teamID: set during authentication
		// userID: set during authentication
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		isActive: true,
	}

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// First message MUST be authentication
	_, message, err := conn.ReadMessage()
	if err != nil {
			log.Printf("❌ Failed to read auth message: %v", err)
			conn.Close()
			return
	}

	var authMsg AuthMessage
	if err := json.Unmarshal(message, &authMsg); err != nil {
			log.Printf("❌ Failed to unmarshal: %v", err)
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
			conn.WriteJSON(map[string]interface{}{
					"type": "auth_error",
					"message": err.Error(),
			})
			conn.Close()
			return
	}
	
	// Success! Register client and start pumps
	hub.register <- client
	conn.WriteJSON(map[string]interface{}{
			"type": "auth_success",
			"message": "Successfully authenticated",
	})
	
	// Clear read deadline and start normal operation
	conn.SetReadDeadline(time.Time{})

	// Start the client's read and write pumps
	go client.writePump()
	go client.readPump()

	log.Printf("New WebSocket connection: team=%s, user=%s", client.teamID, client.userID)
}

// handleSendMessage handles the REST endpoint for sending messages
func handleSendMessage(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	// Parse the message request
	var req MessageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.TeamID == "" || req.UserID == "" || req.MessageType == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Create a new message
	message := NewMessage(req.TeamID, req.UserID, req.MessageType, req.Content)
	messageJSON, err := message.ToJSON()
	if err != nil {
		http.Error(w, "Error encoding message", http.StatusInternalServerError)
		return
	}

	var delivered int
	var success bool

	// Determine if we're broadcasting or sending to a specific user
	if req.Broadcast {
		// Broadcast to the entire team
		delivered = hub.broadcastToTeam(req.TeamID, messageJSON)
		success = delivered > 0
		log.Printf("Broadcast message to team %s: %d recipients", req.TeamID, delivered)
	} else if req.TargetUserID != "" {
		// Send to a specific user
		success = hub.sendToUser(req.TeamID, req.TargetUserID, messageJSON)
		if success {
			delivered = 1
		}
		log.Printf("Sent message to user %s in team %s: %v", req.TargetUserID, req.TeamID, success)
	} else {
		http.Error(w, "Must specify either broadcast=true or target_user_id", http.StatusBadRequest)
		return
	}

	// Return the result
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success":   success,
		"delivered": delivered,
	}
	json.NewEncoder(w).Encode(response)
}
