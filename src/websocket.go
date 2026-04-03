package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CircuitBreaker prevents repeated backend calls while auth is known to be failing.
type CircuitBreaker struct {
	failures    int
	lastFailure time.Time
	mu          sync.Mutex
}

var backendCircuitBreaker = &CircuitBreaker{}

type circuitBreakerFailure struct {
	err error
}

func (e *circuitBreakerFailure) Error() string {
	return e.err.Error()
}

func (e *circuitBreakerFailure) Unwrap() error {
	return e.err
}

func markCircuitBreakerFailure(err error) error {
	if err == nil {
		return nil
	}
	return &circuitBreakerFailure{err: err}
}

func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	if cb.failures >= AppConfig.CircuitBreaker.Threshold {
		if time.Since(cb.lastFailure) < AppConfig.CircuitBreaker.Timeout {
			cb.mu.Unlock()
			return errors.New("circuit breaker open - backend unavailable")
		}
		cb.failures = 0
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		var counted *circuitBreakerFailure
		if errors.As(err, &counted) {
			cb.failures++
			cb.lastFailure = time.Now()
		} else {
			cb.failures = 0
		}
		return err
	}

	cb.failures = 0
	return nil
}

type Client struct {
	hub             *Hub
	conn            Conn
	send            chan []byte
	teamID          string
	userID          string
	isAuthenticated bool
}

type verifiedUser struct {
	ID             string
	SelectedTeamID string
}

func scalarToString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed, trimmed != ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		return "", false
	}
}

func extractSelectedTeamID(raw map[string]any) string {
	if settings, ok := raw["settings"].(map[string]any); ok {
		if teamID, ok := scalarToString(settings["selectedTeam"]); ok {
			return teamID
		}
	}
	if teamID, ok := scalarToString(raw["selectedTeam"]); ok {
		return teamID
	}
	return ""
}

func parseVerifiedUser(body []byte) (*verifiedUser, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	userID, ok := scalarToString(raw["id"])
	if !ok {
		return nil, errors.New("authentication response missing user id")
	}

	return &verifiedUser{
		ID:             userID,
		SelectedTeamID: extractSelectedTeamID(raw),
	}, nil
}

func (c *Client) readPump() {
	defer func() {
		log.Printf("🔌 [%s:%s] ReadPump closing - unregistering client", c.teamID, c.userID)
		c.hub.unregister <- c
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	log.Printf("🔌 [%s:%s] ReadPump started for client", c.teamID, c.userID)

	c.conn.SetReadLimit(AppConfig.WebSocket.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(AppConfig.WebSocket.PongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ [%s:%s] WebSocket unexpected close error: %v", c.teamID, c.userID, err)
			} else {
				log.Printf("🔌 [%s:%s] WebSocket connection closed: %v", c.teamID, c.userID, err)
			}
			return
		}

		// This server is delivery-only. Clients authenticate and then only receive messages.
		return
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(AppConfig.WebSocket.PingPeriod)
	defer func() {
		log.Printf("🔌 [%s:%s] WritePump closing", c.teamID, c.userID)
		ticker.Stop()
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("❌ [%s:%s] Failed to write message: %v", c.teamID, c.userID, err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(AppConfig.WebSocket.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("❌ [%s:%s] Failed to send ping: %v", c.teamID, c.userID, err)
				return
			}
		}
	}
}

func (c *Client) authenticate(authMsg AuthMessage) error {
	teamID := strings.TrimSpace(authMsg.TeamID)
	token := strings.TrimSpace(authMsg.Token)

	if teamID == "" {
		return errors.New("teamId is required")
	}
	if token == "" {
		return errors.New("token is required")
	}

	if IsFakeAuthEnabled() && token == "fake_development_token" {
		userID := strings.TrimSpace(authMsg.UserID)
		if userID == "" {
			return errors.New("userId is required for fake authentication")
		}

		c.userID = userID
		c.teamID = teamID
		c.isAuthenticated = true

		log.Printf("✅ FAKE Client authenticated: user=%s, team=%s", userID, teamID)
		return nil
	}

	if token == "fake_development_token" {
		log.Printf("❌ SECURITY: Fake token rejected in production mode")
		return errors.New("invalid authentication token")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: AppConfig.Backend.Timeout}
	}

	return backendCircuitBreaker.Call(func() error {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(AppConfig.Backend.URL, "/")+"/rest-auth/user/", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		res, err := httpClient.Do(req)
		if err != nil {
			return markCircuitBreakerFailure(err)
		}
		defer res.Body.Close()

		switch res.StatusCode {
		case http.StatusUnauthorized:
			return errors.New("invalid JWT token provided")
		case http.StatusOK:
			bodyBytes, err := io.ReadAll(res.Body)
			if err != nil {
				return markCircuitBreakerFailure(err)
			}

			userData, err := parseVerifiedUser(bodyBytes)
			if err != nil {
				return markCircuitBreakerFailure(err)
			}
			if userData.SelectedTeamID == "" {
				return errors.New("authentication response missing selectedTeam")
			}
			if userData.SelectedTeamID != teamID {
				return fmt.Errorf("requested team %q does not match selectedTeam %q", teamID, userData.SelectedTeamID)
			}

			c.userID = userData.ID
			c.teamID = teamID
			c.isAuthenticated = true

			log.Printf("✅ Client authenticated: user=%s, team=%s", userData.ID, teamID)
			return nil
		default:
			err := errors.New("authentication failed with status: " + res.Status)
			if res.StatusCode >= http.StatusInternalServerError || res.StatusCode == http.StatusTooManyRequests {
				return markCircuitBreakerFailure(err)
			}
			return err
		}
	})
}

type HubHealth struct {
	TotalTeams   int
	TotalClients int
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	clients    map[string]map[string]map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[string]map[string]map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) snapshotTeamClients(teamID string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	teamClients := h.clients[teamID]
	clients := make([]*Client, 0, h.getTeamClientCountLocked(teamID))
	for _, userClients := range teamClients {
		for client := range userClients {
			clients = append(clients, client)
		}
	}
	return clients
}

func (h *Hub) snapshotAllClients() []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := h.getTotalClientCountLocked()
	clients := make([]*Client, 0, total)
	for _, teamClients := range h.clients {
		for _, userClients := range teamClients {
			for client := range userClients {
				clients = append(clients, client)
			}
		}
	}
	return clients
}

// run processes client registrations and unregistrations.
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if _, ok := h.clients[client.teamID]; !ok {
				h.clients[client.teamID] = make(map[string]map[*Client]struct{})
			}
			if _, ok := h.clients[client.teamID][client.userID]; !ok {
				h.clients[client.teamID][client.userID] = make(map[*Client]struct{})
			}
			h.clients[client.teamID][client.userID][client] = struct{}{}
			h.mu.Unlock()

			log.Printf("✅ Client registered: team=%s, user=%s", client.teamID, client.userID)

		case client := <-h.unregister:
			h.removeClient(client)
		}
	}
}

// canAddClient checks if we can add another client to a team.
func (h *Hub) canAddClient(teamID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if _, ok := h.clients[teamID]; ok {
		return h.getTeamClientCountLocked(teamID) < AppConfig.Limits.MaxClientsPerTeam
	}
	return true
}

func (h *Hub) getTeamClientCountLocked(teamID string) int {
	total := 0
	for _, userClients := range h.clients[teamID] {
		total += len(userClients)
	}
	return total
}

func (h *Hub) getTotalClientCountLocked() int {
	total := 0
	for _, teamClients := range h.clients {
		for _, userClients := range teamClients {
			total += len(userClients)
		}
	}
	return total
}

// getTotalClientCount returns the total number of connected clients.
func (h *Hub) getTotalClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.getTotalClientCountLocked()
}

// healthCheck returns health information about the hub.
func (h *Hub) healthCheck() HubHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return HubHealth{
		TotalTeams:   len(h.clients),
		TotalClients: h.getTotalClientCountLocked(),
	}
}

func (h *Hub) enqueueMessage(client *Client, message []byte) (sent bool) {
	if client == nil {
		return false
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("🧹 Recovered while enqueueing message for %s/%s", client.teamID, client.userID)
			sent = false
			h.disconnectClient(client, "send channel closed")
		}
	}()

	select {
	case client.send <- message:
		return true
	default:
		h.disconnectClient(client, "send buffer full")
		return false
	}
}

// sendToUser sends a message to a specific user.
// If teamID is empty, the message is delivered to every connected session for that user across all teams.
func (h *Hub) sendToUser(teamID, userID string, message []byte) int {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0
	}

	if teamID != "" {
		h.mu.RLock()
		userClients := h.clients[teamID][userID]
		clients := make([]*Client, 0, len(userClients))
		for client := range userClients {
			clients = append(clients, client)
		}
		h.mu.RUnlock()

		count := 0
		for _, client := range clients {
			if h.enqueueMessage(client, message) {
				count++
			}
		}
		return count
	}

	count := 0
	for _, client := range h.snapshotAllClients() {
		if client.userID == userID && h.enqueueMessage(client, message) {
			count++
		}
	}
	return count
}

func (h *Hub) broadcastToTeam(teamID string, message []byte) int {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return 0
	}

	count := 0
	for _, client := range h.snapshotTeamClients(teamID) {
		if h.enqueueMessage(client, message) {
			count++
		}
	}
	return count
}

// broadcastToAllTeams sends a message to all users across all teams.
func (h *Hub) broadcastToAllTeams(message []byte) int {
	count := 0
	for _, client := range h.snapshotAllClients() {
		if h.enqueueMessage(client, message) {
			count++
		}
	}
	return count
}

func (h *Hub) disconnectClient(client *Client, reason string) {
	if client == nil {
		return
	}

	log.Printf("🧹 Disconnecting client %s/%s: %s", client.teamID, client.userID, reason)
	if client.conn != nil {
		client.conn.Close()
	}

	go func() {
		h.unregister <- client
	}()
}

// removeClient safely removes a client if it is still the active connection for that user.
func (h *Hub) removeClient(client *Client) bool {
	if client == nil {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	teamClients, ok := h.clients[client.teamID]
	if !ok {
		return false
	}

	userClients, ok := teamClients[client.userID]
	if !ok {
		return false
	}

	if _, ok := userClients[client]; !ok {
		return false
	}

	delete(userClients, client)
	close(client.send)

	if len(userClients) == 0 {
		delete(teamClients, client.userID)
	}
	if len(teamClients) == 0 {
		delete(h.clients, client.teamID)
	}

	return true
}
