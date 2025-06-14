package main

import (
	"bytes"
	"log"
	"time"
	"errors"
	"io"
	"net/http"
	"encoding/json"
	"github.com/gorilla/websocket"
	"strconv"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024 // 512KB
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client represents a connected websocket client
type Client struct {
  hub        *Hub
  conn       *websocket.Conn
  send       chan []byte
  teamID     string
  userID     string
  isActive   bool
  email        string
  isAuthenticated bool
}

// readPump pumps messages from the websocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		
		// Try to parse as auth message
		var authMsg AuthMessage
		if err := json.Unmarshal(message, &authMsg); err == nil && authMsg.Type == "auth" {
			log.Printf("📡 Received auth message from user %d for team %s", authMsg.UserID, authMsg.TeamID)
			
			if err := c.authenticate(authMsg); err != nil {
				log.Printf("❌ Authentication failed: %v", err)
				// Send error response
				c.conn.WriteJSON(map[string]interface{}{
					"type": "auth_error",
					"message": "Authentication failed: " + err.Error(),
				})
				return
			}
			
			// Send success response
			c.conn.WriteJSON(map[string]interface{}{
				"type": "auth_success",
				"message": "Successfully authenticated",
			})
			
		} else {
			// Handle other message types or log
			log.Printf("Received WebSocket message: %s", message)
		}
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
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
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) authenticate(authMsg AuthMessage) error {
  // Make request to main backend (same pattern as chat server)
  req, err := http.NewRequest("GET", backendURL+"/rest-auth/user/", nil)
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
    
    // Set client authentication data
    c.userID = strconv.Itoa(userData.ID) 
    c.email = userData.Email
    c.teamID = authMsg.TeamID
    c.isAuthenticated = true
    
    log.Printf("✅ Client authenticated: user=%d, email=%s, team=%s", 
               userData.ID, userData.Email, authMsg.TeamID)
    
    return nil
  default:
    return errors.New("authentication failed with status: " + res.Status)
  }
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients by team and user
	clients map[string]map[string]*Client
	register chan *Client
	unregister chan *Client
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
			// Initialize team map if it doesn't exist
			if _, ok := h.clients[client.teamID]; !ok {
				h.clients[client.teamID] = make(map[string]*Client)
			}
			h.clients[client.teamID][client.userID] = client
			log.Printf("Client registered: team=%s, user=%s", client.teamID, client.userID)

		case client := <-h.unregister:
			if _, ok := h.clients[client.teamID]; ok {
				if _, ok := h.clients[client.teamID][client.userID]; ok {
					delete(h.clients[client.teamID], client.userID)
					close(client.send)
					log.Printf("Client unregistered: team=%s, user=%s", client.teamID, client.userID)
					
					// Clean up empty team maps
					if len(h.clients[client.teamID]) == 0 {
						delete(h.clients, client.teamID)
					}
				}
			}
		}
	}
}

// sendToUser sends a message to a specific user in a team
func (h *Hub) sendToUser(teamID, userID string, message []byte) bool {
	if teamClients, ok := h.clients[teamID]; ok {
		if client, ok := teamClients[userID]; ok {
			select {
			case client.send <- message:
				return true
			default:
				// If the client's send buffer is full, assume they're gone
				close(client.send)
				delete(teamClients, userID)
				return false
			}
		}
	}
	return false
}

// broadcastToTeam sends a message to all users in a team
func (h *Hub) broadcastToTeam(teamID string, message []byte) int {
	count := 0
	if teamClients, ok := h.clients[teamID]; ok {
		for userID, client := range teamClients {
			select {
			case client.send <- message:
				count++
			default:
				// If the client's send buffer is full, assume they're gone
				close(client.send)
				delete(teamClients, userID)
			}
		}
	}
	return count
}
