// main.go
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient *http.Client
var requestRateLimiter *ipRateLimiter

type healthResponse struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	TotalTeams   int    `json:"total_teams"`
	TotalClients int    `json:"total_clients"`
}

// Middleware functions
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Check if origin is allowed
		if origin != "" && IsOriginAllowed(origin) {
			w.Header().Add("Vary", "Origin")
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
		expectedAPIKey := AppConfig.Security.APIKey
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedAPIKey)) != 1 {
			log.Printf("Invalid API key attempt from %s", r.RemoteAddr)
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestRateLimiter != nil && r.URL.Path != "/health" {
			clientIP := clientIPFromRequest(r)
			if !requestRateLimiter.Allow(clientIP) {
				log.Printf("rate limit exceeded for %s on %s", clientIP, r.URL.Path)
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
		}

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
	requestRateLimiter = newIPRateLimiter(
		AppConfig.RateLimit.RequestsPerSecond,
		AppConfig.RateLimit.Burst,
		AppConfig.RateLimit.EntryTTL,
		AppConfig.RateLimit.CleanupInterval,
	)

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

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		health := hub.healthCheck()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(healthResponse{
			Status:       "healthy",
			Message:      "WebSocket server is running",
			TotalTeams:   health.TotalTeams,
			TotalClients: health.TotalClients,
		}); err != nil {
			log.Printf("failed to encode health response: %v", err)
		}
	})

	// Configure the server with values from config
	server := &http.Server{
		Addr:              ":" + AppConfig.Server.Port,
		Handler:           rateLimitMiddleware(mux),
		ReadTimeout:       AppConfig.Server.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      AppConfig.Server.WriteTimeout,
		IdleTimeout:       AppConfig.Server.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
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
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed to start: %v", err)
	}
}
