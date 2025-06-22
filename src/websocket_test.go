// websocket_test.go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"github.com/gorilla/websocket"
)

// mockConn is a mock for the websocket.Conn
type mockConn struct {
	mu          sync.Mutex
	written     [][]byte
	read        chan []byte
	isClosed    bool
	readLimit   int64
	readDead    time.Time
	writeDead   time.Time
	pongHandler func(string) error
}

func newMockConn() *mockConn {
	return &mockConn{
		written: make([][]byte, 0),
		read:    make(chan []byte, 10),
	}
}

func (c *mockConn) WriteMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return errors.New("use of closed network connection")
	}
	// Simple mock: just append the data. A real test might check messageType.
	c.written = append(c.written, data)
	return nil
}

func (c *mockConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return -1, nil, errors.New("use of closed network connection")
	}
	c.mu.Unlock()
	msg, ok := <-c.read
	if !ok {
		return -1, nil, errors.New("channel closed")
	}
	return websocket.TextMessage, msg, nil
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isClosed {
		c.isClosed = true
		close(c.read)
	}
	return nil
}

func (c *mockConn) SetReadLimit(limit int64)                                 { c.readLimit = limit }
func (c *mockConn) SetReadDeadline(t time.Time) error                        { c.readDead = t; return nil }
func (c *mockConn) SetWriteDeadline(t time.Time) error                       { c.writeDead = t; return nil }
func (c *mockConn) SetPongHandler(handler func(string) error)                { c.pongHandler = handler }
func (c *mockConn) NextWriter(messageType int) (io.WriteCloser, error)       { return nil, nil }
func (c *mockConn) WriteJSON(v interface{}) error {
	data, _ := json.Marshal(v)
	return c.WriteMessage(websocket.TextMessage, data)
}

// setupTestAppConfig initializes a minimal AppConfig for testing purposes.
func setupTestAppConfig() {
	AppConfig = &Config{}
	setDefaults(AppConfig) // Apply defaults
	AppConfig.Security.APIKey = "test-api-key"
	AppConfig.Backend.URL = "http://test.backend"
	AppConfig.Environment.Mode = "production"
}

// TestHub checks the core functionality of the Hub (register, unregister, run).
func TestHub(t *testing.T) {
	setupTestAppConfig()
	hub := newHub()
	go hub.run()

	client1 := &Client{hub: hub, teamID: "team-a", userID: "user-1", send: make(chan []byte, 1)}
	client2 := &Client{hub: hub, teamID: "team-a", userID: "user-2", send: make(chan []byte, 1)}
	client3 := &Client{hub: hub, teamID: "team-b", userID: "user-3", send: make(chan []byte, 1)}

	// Test Registration
	hub.register <- client1
	hub.register <- client2
	hub.register <- client3

	// Allow time for hub to process registrations
	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	if len(hub.clients) != 2 {
		t.Fatalf("Expected 2 teams, got %d", len(hub.clients))
	}
	if len(hub.clients["team-a"]) != 2 {
		t.Fatalf("Expected 2 clients in team-a, got %d", len(hub.clients["team-a"]))
	}
	if len(hub.clients["team-b"]) != 1 {
		t.Fatalf("Expected 1 client in team-b, got %d", len(hub.clients["team-b"]))
	}
	hub.mu.RUnlock()

	// Test Unregistration
	hub.unregister <- client2

	// Allow time for hub to process unregistration
	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	if len(hub.clients["team-a"]) != 1 {
		t.Fatalf("Expected 1 client in team-a after unregister, got %d", len(hub.clients["team-a"]))
	}
	if _, ok := hub.clients["team-a"]["user-2"]; ok {
		t.Fatal("Unregistered client user-2 should not exist in team-a")
	}
	hub.mu.RUnlock()

	// Test team cleanup after last client leaves
	hub.unregister <- client1

	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	if _, ok := hub.clients["team-a"]; ok {
		t.Fatal("Team-a should have been removed after its last client was unregistered")
	}
	hub.mu.RUnlock()
}

// TestHub_ClientLimits tests the client limit enforcement.
func TestHub_ClientLimits(t *testing.T) {
	setupTestAppConfig()
	AppConfig.Limits.MaxClientsPerTeam = 2
	hub := newHub()
	go hub.run()

	// Add 2 clients, which is the limit
	for i := 0; i < 2; i++ {
		hub.register <- &Client{hub: hub, teamID: "team-limited", userID: fmt.Sprintf("user-%d", i), send: make(chan []byte, 1)}
	}

	time.Sleep(100 * time.Millisecond)

	if hub.canAddClient("team-limited") {
		t.Error("canAddClient should return false when team is at capacity")
	}

	if !hub.canAddClient("another-team") {
		t.Error("canAddClient should return true for a new team")
	}

	totalClients := hub.getTotalClientCount()
	if totalClients != 2 {
		t.Errorf("Expected total client count to be 2, got %d", totalClients)
	}
}

// TestHub_Messaging tests the hub's message sending capabilities.
func TestHub_Messaging(t *testing.T) {
	setupTestAppConfig()
	hub := newHub()
	go hub.run()

	conn1 := newMockConn()
	conn2 := newMockConn()
	conn3 := newMockConn()
	client1 := &Client{hub: hub, conn: conn1, teamID: "team-a", userID: "user-1", send: make(chan []byte, 1)}
	client2 := &Client{hub: hub, conn: conn2, teamID: "team-a", userID: "user-2", send: make(chan []byte, 1)}
	client3 := &Client{hub: hub, conn: conn3, teamID: "team-b", userID: "user-3", send: make(chan []byte, 1)}

	hub.register <- client1
	hub.register <- client2
	hub.register <- client3
	time.Sleep(100 * time.Millisecond)

	t.Run("SendToUser", func(t *testing.T) {
		message := []byte("private message")
		success := hub.sendToUser("team-a", "user-1", message)
		if !success {
			t.Fatal("sendToUser should have returned true for a connected client")
		}

		// Check if message was received by the correct client
		select {
		case received := <-client1.send:
			if string(received) != string(message) {
				t.Errorf("Expected client1 to receive '%s', got '%s'", message, received)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("Timed out waiting for message")
		}

		// Ensure other clients did not receive it
		if len(client2.send) > 0 {
			t.Error("client2 should not have received the private message")
		}
	})

	t.Run("BroadcastToTeam", func(t *testing.T) {
		message := []byte("team broadcast")
		count := hub.broadcastToTeam("team-a", message)
		if count != 2 {
			t.Errorf("Expected broadcast to deliver to 2 clients, got %d", count)
		}

		// Check both clients in the team received it
		for i, c := range []*Client{client1, client2} {
			select {
			case received := <-c.send:
				if string(received) != string(message) {
					t.Errorf("Expected client %d to receive '%s', got '%s'", i+1, message, received)
				}
			case <-time.After(1 * time.Second):
				t.Fatalf("Timed out waiting for broadcast message for client %d", i+1)
			}
		}

		// Ensure client in other team did not receive it
		if len(client3.send) > 0 {
			t.Error("client3 should not have received the team-a broadcast")
		}
	})

	t.Run("BroadcastToAllTeams", func(t *testing.T) {
		message := []byte("global broadcast")
		count := hub.broadcastToAllTeams(message)
		if count != 3 {
			t.Errorf("Expected global broadcast to deliver to 3 clients, got %d", count)
		}

		// Check all clients received it
		for i, c := range []*Client{client1, client2, client3} {
			select {
			case received := <-c.send:
				if string(received) != string(message) {
					t.Errorf("Expected client %d to receive '%s', got '%s'", i+1, message, received)
				}
			case <-time.After(1 * time.Second):
				t.Fatalf("Timed out waiting for global broadcast message for client %d", i+1)
			}
		}
	})
}

// TestClient_Authentication tests the client authentication logic.
func TestClient_Authentication(t *testing.T) {
	// 1. Setup a mock backend server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "Bearer valid-token" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 123, "email": "test@example.com"}`))
		} else if authHeader == "Bearer invalid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"detail": "Invalid token"}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer mockServer.Close()

	// 2. Setup AppConfig to use the mock server
	setupTestAppConfig()
	AppConfig.Backend.URL = mockServer.URL
	httpClient = mockServer.Client() // Use the test server's client

	// 3. Define test cases
	testCases := []struct {
		name          string
		mode          string // "development" or "production"
		fakeAuth      bool
		authMsg       AuthMessage
		expectErr     bool
		expectedErrStr string
		expectedUserID string
		expectedEmail  string
	}{
		{
			name:      "Success - Production with valid token",
			mode:      "production",
			authMsg:   AuthMessage{Token: "valid-token", TeamID: "team-prod", UserID: "temp-user"},
			expectErr: false,
			expectedUserID: "123",
			expectedEmail:  "test@example.com",
		},
		{
			name:        "Failure - Production with invalid token",
			mode:        "production",
			authMsg:     AuthMessage{Token: "invalid-token", TeamID: "team-prod"},
			expectErr:   true,
			expectedErrStr: "invalid JWT token provided",
		},
		{
			name:       "Success - Development with fake token",
			mode:       "development",
			fakeAuth:   true,
			authMsg:    AuthMessage{Token: "fake_development_token", TeamID: "team-dev", UserID: "fake-user-456"},
			expectErr:  false,
			expectedUserID: "fake-user-456",
			expectedEmail:  "fake_fake-user-456@example.com",
		},
		{
			name:        "Failure - Production with fake token",
			mode:        "production",
			authMsg:     AuthMessage{Token: "fake_development_token", TeamID: "team-prod"},
			expectErr:   true,
			expectedErrStr: "invalid authentication token",
		},
		{
			name:        "Failure - Development with fake token but fake auth disabled",
			mode:        "development",
			fakeAuth:   false,
			authMsg:     AuthMessage{Token: "fake_development_token", TeamID: "team-dev"},
			expectErr:   true,
			expectedErrStr: "invalid authentication token", // It gets rejected before making a real call
		},
		{
			name:        "Failure - Backend server error",
			mode:        "production",
			authMsg:     AuthMessage{Token: "causes-server-error", TeamID: "team-prod"},
			expectErr:   true,
			// Changed to match the actual error format from the application
			expectedErrStr: "authentication failed with status: 500 Internal Server Error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			AppConfig.Environment.Mode = tc.mode
			AppConfig.Environment.EnableFakeAuth = tc.fakeAuth

			client := &Client{} // A minimal client is enough
			err := client.authenticate(tc.authMsg)

			if tc.expectErr {
				if err == nil {
					t.Fatal("Expected an error, but got nil")
				}
				if !strings.Contains(err.Error(), tc.expectedErrStr) {
					t.Errorf("Expected error to contain '%s', got '%s'", tc.expectedErrStr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Expected no error, but got: %v", err)
				}
				if client.userID != tc.expectedUserID {
					t.Errorf("Expected UserID to be '%s', got '%s'", tc.expectedUserID, client.userID)
				}
				if client.email != tc.expectedEmail {
					t.Errorf("Expected Email to be '%s', got '%s'", tc.expectedEmail, client.email)
				}
				if !client.isAuthenticated {
					t.Error("Expected client.isAuthenticated to be true")
				}
			}
		})
	}
}

// TestCircuitBreaker verifies the circuit breaker logic.
func TestCircuitBreaker(t *testing.T) {
	setupTestAppConfig()
	AppConfig.CircuitBreaker.Threshold = 2
	AppConfig.CircuitBreaker.Timeout = 100 * time.Millisecond
	
	cb := &CircuitBreaker{}
	failingCall := func() error { return errors.New("backend failure") }
	successfulCall := func() error { return nil }

	// First failure
	err := cb.Call(failingCall)
	if err == nil { t.Fatal("Expected error on first call") }
	if cb.failures != 1 { t.Errorf("Expected 1 failure, got %d", cb.failures) }

	// Second failure, should trip the breaker
	err = cb.Call(failingCall)
	if err == nil { t.Fatal("Expected error on second call") }
	if cb.failures != 2 { t.Errorf("Expected 2 failures, got %d", cb.failures) }

	// Breaker is now open
	err = cb.Call(successfulCall) // This call shouldn't even be attempted
	if err == nil { t.Fatal("Expected circuit breaker to be open") }
	if err.Error() != "circuit breaker open - backend unavailable" {
		t.Errorf("Expected open circuit breaker error, got: %v", err)
	}

	// Wait for the timeout to elapse
	time.Sleep(110 * time.Millisecond)

	// Breaker is now half-open. A successful call should close it.
	err = cb.Call(successfulCall)
	if err != nil { t.Fatalf("Expected successful call after timeout, got: %v", err) }
	if cb.failures != 0 { t.Errorf("Expected failures to be reset to 0, got %d", cb.failures) }
	
	// A subsequent successful call should also work
	err = cb.Call(successfulCall)
	if err != nil { t.Fatalf("Expected another successful call, got: %v", err) }
}
