// main.go
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

var httpClient *http.Client

// Middleware functions
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Check if origin is allowed
		if IsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func apiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for API key in header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != AppConfig.Security.APIKey {
			log.Printf("Invalid API key attempt: %s", apiKey)
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add rate limiting logic here
		// For now, just pass through
		next.ServeHTTP(w, r)
	})
}

func main() {
	// Load configuration
	configPath := "local_settings.yaml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	if err := LoadConfig(configPath); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize HTTP client with configured timeout
	httpClient = &http.Client{
		Timeout: AppConfig.Backend.Timeout,
	}

	// Initialize the hub
	hub := newHub()
	go hub.run()

	// Create router with middleware
	mux := http.NewServeMux()

	// Set up HTTP handlers with security middleware
	mux.HandleFunc("/ws", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(hub, w, r)
	}))

	mux.HandleFunc("/send", corsMiddleware(apiKeyMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleSendMessage(hub, w, r)
	})))

	// Enhanced health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		health := hub.healthCheck()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		
		// Simple JSON response
		response := `{
			"status": "healthy",
			"message": "WebSocket server is running",
			"total_teams": ` + string(rune(health["total_teams"].(int))) + `,
			"total_clients": ` + string(rune(health["total_clients"].(int))) + `
		}`
		w.Write([]byte(response))
	})

	// Configure the server with values from config
	server := &http.Server{
		Addr:         ":" + AppConfig.Server.Port,
		Handler:      rateLimitMiddleware(mux),
		ReadTimeout:  AppConfig.Server.ReadTimeout,
		WriteTimeout: AppConfig.Server.WriteTimeout,
		IdleTimeout:  AppConfig.Server.IdleTimeout,
	}

	// Log startup information
	log.Printf("=== WebSocket Notification Server Starting ===")
	log.Printf("Port: %s", AppConfig.Server.Port)
	log.Printf("Backend URL: %s", AppConfig.Backend.URL)
	if IsDevelopment() {
		log.Printf("🧪 DEVELOPMENT MODE ENABLED")
		log.Printf("🧪 CORS: %s", func() string {
			if ShouldAllowAllOrigins() {
				return "Allow all origins"
			}
			return "Restricted origins"
		}())
		log.Printf("🧪 Fake Auth: %v", IsFakeAuthEnabled())
	} else {
		log.Printf("🔒 PRODUCTION MODE")
		log.Printf("🔒 CORS: Restricted to allowed origins only")
		log.Printf("🔒 Fake Auth: Disabled")
	}
	log.Printf("Allowed Origins: %s", strings.Join(AppConfig.Server.AllowedOrigins, ", "))
	log.Printf("Max Clients Per Team: %d", AppConfig.Limits.MaxClientsPerTeam)
	log.Printf("===============================================")

	// Start the server
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}