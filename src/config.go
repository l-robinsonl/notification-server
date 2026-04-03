// config.go
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Server struct {
		Port           string        `yaml:"port"`
		ReadTimeout    time.Duration `yaml:"read_timeout"`
		WriteTimeout   time.Duration `yaml:"write_timeout"`
		IdleTimeout    time.Duration `yaml:"idle_timeout"`
		AllowedOrigins []string      `yaml:"allowed_origins"`
	} `yaml:"server"`

	WebSocket struct {
		WriteWait          time.Duration `yaml:"write_wait"`
		PongWait           time.Duration `yaml:"pong_wait"`
		PingPeriod         time.Duration `yaml:"ping_period"`
		MaxMessageSize     int64         `yaml:"max_message_size"`
		AuthMaxMessageSize int64         `yaml:"auth_max_message_size"`
		ReadDeadline       time.Duration `yaml:"read_deadline"`
		BufferSize         struct {
			Read  int `yaml:"read"`
			Write int `yaml:"write"`
		} `yaml:"buffer_size"`
	} `yaml:"websocket"`

	Security struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"security"`

	Backend struct {
		URL     string        `yaml:"url"`
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"backend"`

	Limits struct {
		MaxClientsPerTeam int `yaml:"max_clients_per_team"`
		SendChannelBuffer int `yaml:"send_channel_buffer"`
	} `yaml:"limits"`

	CircuitBreaker struct {
		Threshold int           `yaml:"threshold"`
		Timeout   time.Duration `yaml:"timeout"`
	} `yaml:"circuit_breaker"`

	RateLimit struct {
		RequestsPerSecond float64       `yaml:"requests_per_second"`
		Burst             int           `yaml:"burst"`
		EntryTTL          time.Duration `yaml:"entry_ttl"`
		CleanupInterval   time.Duration `yaml:"cleanup_interval"`
	} `yaml:"rate_limit"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`

	// Environment settings
	Environment struct {
		Mode            string `yaml:"mode"`              // "development" or "production"
		AllowAllOrigins bool   `yaml:"allow_all_origins"` // Override for dev
		EnableFakeAuth  bool   `yaml:"enable_fake_auth"`  // For testing
	} `yaml:"environment"`
}

var AppConfig *Config

func LoadConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	// Set defaults for any missing values
	setDefaults(config)

	// Validate required fields
	if err := validateConfig(config); err != nil {
		return fmt.Errorf("config validation failed: %v", err)
	}

	AppConfig = config
	log.Printf("Configuration loaded successfully from %s", configPath)
	return nil
}

func setDefaults(config *Config) {
	if config.Server.Port == "" {
		config.Server.Port = "8081"
	}
	if config.Server.ReadTimeout == 0 {
		config.Server.ReadTimeout = 10 * time.Second
	}
	if config.Server.WriteTimeout == 0 {
		config.Server.WriteTimeout = 10 * time.Second
	}
	if config.Server.IdleTimeout == 0 {
		config.Server.IdleTimeout = 120 * time.Second
	}
	if len(config.Server.AllowedOrigins) == 0 {
		config.Server.AllowedOrigins = []string{}
	}

	if config.WebSocket.WriteWait == 0 {
		config.WebSocket.WriteWait = 10 * time.Second
	}
	if config.WebSocket.PongWait == 0 {
		config.WebSocket.PongWait = 60 * time.Second
	}
	if config.WebSocket.PingPeriod == 0 {
		config.WebSocket.PingPeriod = (config.WebSocket.PongWait * 9) / 10
	}
	if config.WebSocket.MaxMessageSize == 0 {
		config.WebSocket.MaxMessageSize = 512 * 1024 // 512KB
	}
	if config.WebSocket.AuthMaxMessageSize == 0 {
		config.WebSocket.AuthMaxMessageSize = 16 * 1024 // 16KB
	}
	if config.WebSocket.ReadDeadline == 0 {
		config.WebSocket.ReadDeadline = 30 * time.Second
	}
	if config.WebSocket.BufferSize.Read == 0 {
		config.WebSocket.BufferSize.Read = 1024
	}
	if config.WebSocket.BufferSize.Write == 0 {
		config.WebSocket.BufferSize.Write = 1024
	}

	if config.Backend.URL == "" {
		config.Backend.URL = "http://localhost:8000"
	}
	if config.Backend.Timeout == 0 {
		config.Backend.Timeout = 10 * time.Second
	}

	if config.Limits.MaxClientsPerTeam == 0 {
		config.Limits.MaxClientsPerTeam = 1000
	}
	if config.Limits.SendChannelBuffer == 0 {
		config.Limits.SendChannelBuffer = 256
	}

	if config.CircuitBreaker.Threshold == 0 {
		config.CircuitBreaker.Threshold = 5
	}
	if config.CircuitBreaker.Timeout == 0 {
		config.CircuitBreaker.Timeout = 60 * time.Second
	}

	if config.RateLimit.RequestsPerSecond == 0 {
		config.RateLimit.RequestsPerSecond = 20
	}
	if config.RateLimit.Burst == 0 {
		config.RateLimit.Burst = 60
	}
	if config.RateLimit.EntryTTL == 0 {
		config.RateLimit.EntryTTL = 5 * time.Minute
	}
	if config.RateLimit.CleanupInterval == 0 {
		config.RateLimit.CleanupInterval = time.Minute
	}

	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "text"
	}

	// Environment defaults
	if config.Environment.Mode == "" {
		config.Environment.Mode = "production" // Default to production for safety
	}
}

func validateConfig(config *Config) error {
	config.Security.APIKey = strings.TrimSpace(config.Security.APIKey)
	config.Backend.URL = strings.TrimSpace(config.Backend.URL)
	config.Environment.Mode = strings.ToLower(strings.TrimSpace(config.Environment.Mode))

	if config.Security.APIKey == "" {
		return fmt.Errorf("security.api_key is required")
	}
	if config.Backend.URL == "" {
		return fmt.Errorf("backend.url is required")
	}
	if config.Environment.Mode != "development" && config.Environment.Mode != "production" {
		return fmt.Errorf("environment.mode must be either development or production")
	}
	if config.Environment.Mode == "production" && config.Environment.EnableFakeAuth {
		return fmt.Errorf("environment.enable_fake_auth cannot be true in production")
	}
	if config.WebSocket.PingPeriod >= config.WebSocket.PongWait {
		return fmt.Errorf("websocket.ping_period must be less than websocket.pong_wait")
	}
	if config.WebSocket.AuthMaxMessageSize < 1 {
		return fmt.Errorf("websocket.auth_max_message_size must be greater than 0")
	}
	if config.WebSocket.AuthMaxMessageSize > config.WebSocket.MaxMessageSize {
		return fmt.Errorf("websocket.auth_max_message_size must not exceed websocket.max_message_size")
	}
	if config.Limits.MaxClientsPerTeam < 1 {
		return fmt.Errorf("limits.max_clients_per_team must be greater than 0")
	}
	if config.Limits.SendChannelBuffer < 1 {
		return fmt.Errorf("limits.send_channel_buffer must be greater than 0")
	}
	if config.RateLimit.RequestsPerSecond <= 0 {
		return fmt.Errorf("rate_limit.requests_per_second must be greater than 0")
	}
	if config.RateLimit.Burst < 1 {
		return fmt.Errorf("rate_limit.burst must be greater than 0")
	}
	if config.RateLimit.EntryTTL <= 0 {
		return fmt.Errorf("rate_limit.entry_ttl must be greater than 0")
	}
	if config.RateLimit.CleanupInterval <= 0 {
		return fmt.Errorf("rate_limit.cleanup_interval must be greater than 0")
	}
	return nil
}

// Environment helper functions
func IsDevelopment() bool {
	if AppConfig == nil {
		return false
	}
	return AppConfig.Environment.Mode == "development"
}

func IsProduction() bool {
	if AppConfig == nil {
		return true // Default to production for safety
	}
	return AppConfig.Environment.Mode == "production"
}

func ShouldAllowAllOrigins() bool {
	if AppConfig == nil {
		return false
	}
	// Allow all origins if explicitly set OR if in development mode
	return AppConfig.Environment.AllowAllOrigins || IsDevelopment()
}

func IsFakeAuthEnabled() bool {
	if AppConfig == nil {
		return false
	}
	// Only allow fake auth in development
	return AppConfig.Environment.EnableFakeAuth && IsDevelopment()
}

// Enhanced IsOriginAllowed function
func IsOriginAllowed(origin string) bool {
	if AppConfig == nil {
		return false
	}

	// In development, allow all origins if configured
	if ShouldAllowAllOrigins() {
		log.Printf("🧪 DEV: Allowing origin %s (development mode)", origin)
		return true
	}

	// In production, check against allowed origins list
	for _, allowed := range AppConfig.Server.AllowedOrigins {
		if allowed == "*" {
			log.Printf("⚠️  WARNING: Wildcard origin allowed in production!")
			return true
		}
		if allowed == origin {
			return true
		}
	}

	log.Printf("❌ Origin rejected: %s", origin)
	return false
}
