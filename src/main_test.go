// main_test.go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestApiKeyMiddleware validates the API key checking logic.
func TestApiKeyMiddleware(t *testing.T) {
	setupTestAppConfig() // Sets AppConfig.Security.APIKey = "test-api-key"

	// A dummy handler to pass to the middleware
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	testCases := []struct {
		name           string
		apiKeyHeader   string
		expectedStatus int
	}{
		{
			name:           "Valid API Key",
			apiKeyHeader:   "test-api-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid API Key",
			apiKeyHeader:   "wrong-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Missing API Key",
			apiKeyHeader:   "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing/send", nil)
			req.Header.Set("X-API-Key", tc.apiKeyHeader)
			rr := httptest.NewRecorder()

			// Create the handler with the middleware
			handlerToTest := apiKeyMiddleware(nextHandler)
			handlerToTest.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tc.expectedStatus)
			}
		})
	}
}

// TestCorsMiddleware validates the CORS header logic.
func TestCorsMiddleware(t *testing.T) {
	setupTestAppConfig()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name           string
		mode           string // "development" or "production"
		allowedOrigins []string
		requestOrigin  string
		expectedHeader string
	}{
		{
			name:           "Production - Origin Allowed",
			mode:           "production",
			allowedOrigins: []string{"http://safe.com"},
			requestOrigin:  "http://safe.com",
			expectedHeader: "http://safe.com",
		},
		{
			name:           "Production - Origin Denied",
			mode:           "production",
			allowedOrigins: []string{"http://safe.com"},
			requestOrigin:  "http://unsafe.com",
			expectedHeader: "", // No header should be set
		},
		{
			name:           "Development Mode - Any Origin Allowed",
			mode:           "development",
			allowedOrigins: []string{}, // Should be ignored
			requestOrigin:  "http://localhost:3000",
			expectedHeader: "http://localhost:3000",
		},
		{
			name:           "Production - Wildcard Origin",
			mode:           "production",
			allowedOrigins: []string{"*"},
			requestOrigin:  "http://any.com",
			expectedHeader: "http://any.com", // Should reflect the request origin
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			AppConfig.Environment.Mode = tc.mode
			AppConfig.Server.AllowedOrigins = tc.allowedOrigins

			req := httptest.NewRequest("GET", "http://testing/ws", nil)
			req.Header.Set("Origin", tc.requestOrigin)
			rr := httptest.NewRecorder()

			handlerToTest := corsMiddleware(nextHandler)
			handlerToTest.ServeHTTP(rr, req)

			header := rr.Header().Get("Access-Control-Allow-Origin")
			if header != tc.expectedHeader {
				t.Errorf("Expected CORS header to be '%s', got '%s'", tc.expectedHeader, header)
			}
		})
	}
}

// TestHealthCheckHandler tests the /health endpoint.
func TestHealthCheckHandler(t *testing.T) {
	hub := newHub()
	// Populate hub for a more realistic health check
	hub.clients = map[string]map[string]*Client{
		"team-1": {"user-1": nil, "user-2": nil},
		"team-2": {"user-3": nil},
	}
	
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This is a simplified version of the main.go handler
		health := hub.healthCheck()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "healthy",
			"total_teams":   health["total_teams"],
			"total_clients": health["total_clients"],
		})
	})
	
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("health handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	
	// Check the content of the response
	expectedBody := `"total_teams":2`
	if !strings.Contains(rr.Body.String(), expectedBody) {
		t.Errorf("health handler body missing or incorrect total_teams: got %s", rr.Body.String())
	}
	
	expectedBody = `"total_clients":3`
	if !strings.Contains(rr.Body.String(), expectedBody) {
		t.Errorf("health handler body missing or incorrect total_clients: got %s", rr.Body.String())
	}
}
