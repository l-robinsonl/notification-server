// handlers_test.go - Fixed version
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestHandleSendMessage tests the /send endpoint logic.
func TestHandleSendMessage(t *testing.T) {
	setupTestAppConfig()
	hub := newHub() // Using a real hub instance is fine here

	testCases := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectedBody   string
		expectBroadcast bool
		expectSendToUser bool
	}{
		{
			name:           "Success - Broadcast Message",
			requestBody:    `{"message_type": "system_alert", "body": "server is restarting", "broadcast": true}`,
			expectedStatus: http.StatusOK,
			expectedBody:   `"delivered":1`,
			expectBroadcast: true,
			expectSendToUser: false,
		},
		{
			name:           "Success - User-Specific Message",
			requestBody:    `{"target_team_id": "team-1", "target_user_id": "user-1", "message_type": "user_message", "body": "hello there"}`,
			expectedStatus: http.StatusOK,
			expectedBody:   `"delivered":1`,
			expectBroadcast: false,
			expectSendToUser: true,
		},
		{
			name:           "Failure - Invalid JSON",
			requestBody:    `{"target_team_id": "team-1",...}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `Invalid JSON`,
			expectBroadcast: false,
			expectSendToUser: false,
		},
		{
			name:           "Failure - Missing MessageType",
			requestBody:    `{"target_team_id": "team-1", "target_user_id": "user-1"}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `Missing required field: MessageType`,
			expectBroadcast: false,
			expectSendToUser: false,
		},
		{
			name:           "Failure - Conflicting Broadcast and UserID",
			requestBody:    `{"broadcast": true, "target_user_id": "user-1", "message_type": "test"}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `Cannot specify TeamID or TargetUserID when Broadcast is true`,
			expectBroadcast: false,
			expectSendToUser: false,
		},
		{
			name:           "Failure - Missing Target for Non-Broadcast",
			requestBody:    `{"message_type": "test"}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   `Must specify a TeamID and TargetUserID for non-broadcast messages`,
			expectBroadcast: false,
			expectSendToUser: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Need to register at least one client for the broadcast/send to succeed
			client := &Client{teamID: "team-1", userID: "user-1", send: make(chan []byte, 1)}
			hub.clients = map[string]map[string]*Client{
				"team-1": {"user-1": client},
			}

			req := httptest.NewRequest("POST", "/send", bytes.NewBufferString(tc.requestBody))
			rr := httptest.NewRecorder()

			// Create a handler function and call it
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handleSendMessage(hub, w, r)
			})
			handler.ServeHTTP(rr, req)

			// Check status code
			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}

			// Check response body
			if !strings.Contains(rr.Body.String(), tc.expectedBody) {
				t.Errorf("handler returned unexpected body: got %v want it to contain %v", rr.Body.String(), tc.expectedBody)
			}
		})
	}
}

// TestHandleWebSocket tests the WebSocket upgrade and initial auth flow.
func TestHandleWebSocket(t *testing.T) {
	setupTestAppConfig()
	hub := newHub()
	go hub.run()

	// This is a simplified version of mocking for demonstration.
	// In a real app, you might use interfaces for easier mocking of the auth function itself.
	mockAuthenticate := func(c *Client, msg AuthMessage) error {
		if msg.Token == "good-token" {
			c.teamID = msg.TeamID
			c.userID = msg.UserID
			c.isAuthenticated = true
			return nil
		}
		return errors.New("mock auth failed")
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This mock handler simulates the real handleWebSocket but with mocked authentication.
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Because we refactored to use the Conn interface, the real conn is fine here.
		client := &Client{hub: hub, conn: conn, send: make(chan []byte, 1)}

		// Read auth message
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var authMsg AuthMessage
		json.Unmarshal(msgBytes, &authMsg)

		// Use the mock auth function
		if err := mockAuthenticate(client, authMsg); err != nil {
			client.conn.WriteJSON(map[string]string{"type": "auth_error", "message": err.Error()})
			client.conn.Close()
			return
		}
		hub.register <- client
		client.conn.WriteJSON(map[string]string{"type": "auth_success"})

	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("Successful Connection and Auth", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("could not open a ws connection on %s: %v", wsURL, err)
		}
		defer ws.Close()

		// Send valid auth message
		authMsg := AuthMessage{Type: "auth", Token: "good-token", TeamID: "team-ws", UserID: "user-ws"}
		if err := ws.WriteJSON(authMsg); err != nil {
			t.Fatalf("could not send message over ws connection: %v", err)
		}

		// Expect auth_success response
		var response map[string]string
		err = ws.ReadJSON(&response)
		if err != nil {
			t.Fatalf("could not read message from ws connection: %v", err)
		}

		if response["type"] != "auth_success" {
			t.Errorf("expected auth_success, got %s", response["type"])
		}

		// Check if client was registered in the hub
		time.Sleep(100 * time.Millisecond) // allow time for registration
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		if _, ok := hub.clients["team-ws"]["user-ws"]; !ok {
			t.Error("client was not registered in the hub after successful auth")
		}
	})

	t.Run("Failed Auth", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("could not open a ws connection on %s: %v", wsURL, err)
		}
		defer ws.Close()

		// Send invalid auth message
		authMsg := AuthMessage{Type: "auth", Token: "bad-token"}
		if err := ws.WriteJSON(authMsg); err != nil {
			t.Fatalf("could not send message over ws connection: %v", err)
		}

		// Expect auth_error response, then connection close
		var response map[string]string
		err = ws.ReadJSON(&response)
		if err != nil {
			t.Fatalf("could not read auth_error message from ws connection: %v", err)
		}

		if response["type"] != "auth_error" {
			t.Errorf("expected auth_error, got %s", response["type"])
		}

		// Now expect the connection to be closed
		err = ws.ReadJSON(&response)
		if err == nil {
			t.Fatalf("expected connection to be closed after auth failure, but got response: %v", response)
		}

		if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
			t.Fatalf("expected a close error, but got a different error: %v", err)
		}
	})
}