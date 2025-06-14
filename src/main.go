package main

import (
	"log"
	"net/http"
	"os"
	"time"

)


var httpClient = &http.Client{
  Timeout: 10 * time.Second,
}

// Your backend URL - replace with your actual backend URL
const backendURL = "http://localhost:8000" 

// Middleware functions
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Team-ID, X-User-ID")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}



// func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
    
// 		// // In real implementation, you'd validate tokens and extract these values
// 		// if r.Header.Get("X-Team-ID") == "" {
// 		// 	r.Header.Set("X-Team-ID", "17") // Demo value
// 		// }
// 		// if r.Header.Get("X-User-ID") == "" {
// 		// 	r.Header.Set("X-User-ID", "1") // Demo value
// 		// }
		
// 		next(w, r)
// 	}
// }

func apiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for API key in header
		// apiKey := r.Header.Get("X-API-Key")
		// if apiKey == "" {
		// 	http.Error(w, "API key required", http.StatusUnauthorized)
		// 	return
		// }

		// Add your API key validation logic here
		// For now, just check if it exists
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

	// Add a health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Vibin'"))
	})

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	// Configure the server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      rateLimitMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start the server
	log.Printf("Secure WebSocket server starting on port %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}