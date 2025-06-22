// config_test.go
package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Helper function to create a temporary config file for testing
func createTempConfigFile(t *testing.T, content string) (string, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configFile := filepath.Join(dir, "config.yaml")
	if err := ioutil.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write to temp config file: %v", err)
	}

	// Return the path and a cleanup function
	return configFile, func() {
		os.RemoveAll(dir)
		AppConfig = nil // Reset global AppConfig after each test
	}
}

// TestLoadConfig_Success tests the successful loading of a complete and valid config file.
func TestLoadConfig_Success(t *testing.T) {
	yamlContent := `
server:
  port: 9090
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 150s
  allowed_origins:
    - http://localhost:3000
    - https://myapp.com
websocket:
  write_wait: 15s
  pong_wait: 70s
  ping_period: 63s
  max_message_size: 1048576 # 1MB
  read_deadline: 45s
  buffer_size:
    read: 2048
    write: 2048
security:
  api_key: "my-secret-api-key"
backend:
  url: "http://backend-service:8000"
  timeout: 5s
limits:
  max_clients_per_team: 500
  send_channel_buffer: 512
circuit_breaker:
  threshold: 10
  timeout: 90s
logging:
  level: "debug"
  format: "json"
environment:
  mode: "development"
  allow_all_origins: true
  enable_fake_auth: true
`
	configFile, cleanup := createTempConfigFile(t, yamlContent)
	defer cleanup()

	err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() returned an unexpected error: %v", err)
	}

	if AppConfig == nil {
		t.Fatal("AppConfig should not be nil after successful loading")
	}

	// Assert a few key values to ensure parsing was correct
	if AppConfig.Server.Port != "9090" {
		t.Errorf("Expected Server.Port to be '9090', got '%s'", AppConfig.Server.Port)
	}
	if AppConfig.Security.APIKey != "my-secret-api-key" {
		t.Errorf("Expected Security.APIKey to be 'my-secret-api-key', got '%s'", AppConfig.Security.APIKey)
	}
	if AppConfig.Environment.Mode != "development" {
		t.Errorf("Expected Environment.Mode to be 'development', got '%s'", AppConfig.Environment.Mode)
	}
	expectedOrigins := []string{"http://localhost:3000", "https://myapp.com"}
	if !reflect.DeepEqual(AppConfig.Server.AllowedOrigins, expectedOrigins) {
		t.Errorf("Expected AllowedOrigins to be %v, got %v", expectedOrigins, AppConfig.Server.AllowedOrigins)
	}
}

// TestLoadConfig_FileNotExist tests loading a config from a non-existent path.
func TestLoadConfig_FileNotExist(t *testing.T) {
	err := LoadConfig("non_existent_config.yaml")
	if err == nil {
		t.Fatal("LoadConfig() should have returned an error for a non-existent file, but it didn't")
	}

	expectedError := "failed to read config file"
	if !reflect.DeepEqual(err.Error()[:len(expectedError)], expectedError) {
		t.Errorf("Expected error message to start with '%s', got '%v'", expectedError, err)
	}
}

// TestLoadConfig_InvalidYAML tests loading a config file with malformed YAML.
func TestLoadConfig_InvalidYAML(t *testing.T) {
	invalidYAML := `
server:
  port: 8080
  security: // Invalid indentation
    api_key: "bad-key"
`
	configFile, cleanup := createTempConfigFile(t, invalidYAML)
	defer cleanup()

	err := LoadConfig(configFile)
	if err == nil {
		t.Fatal("LoadConfig() should have returned an error for invalid YAML, but it didn't")
	}

	expectedError := "failed to parse config file"
	if !reflect.DeepEqual(err.Error()[:len(expectedError)], expectedError) {
		t.Errorf("Expected error message to start with '%s', got '%v'", expectedError, err)
	}
}

// TestLoadConfig_ValidationFailure tests config validation for missing required fields.
func TestLoadConfig_ValidationFailure(t *testing.T) {
	// Missing security.api_key
	yamlWithoutAPIKey := `
backend:
  url: "http://localhost:8000"
`
	configFile, cleanup := createTempConfigFile(t, yamlWithoutAPIKey)
	defer cleanup()

	err := LoadConfig(configFile)
	if err == nil {
		t.Fatal("LoadConfig() should have returned a validation error, but it didn't")
	}

	expectedError := "config validation failed: security.api_key is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%v'", expectedError, err)
	}
}

// TestSetDefaults checks if default values are correctly applied for an empty config.
func TestSetDefaults(t *testing.T) {
	// We need some required fields for validation to pass
	minimalYAML := `
security:
  api_key: "a-required-key"
backend:
  url: "http://a-required-url"
`
	configFile, cleanup := createTempConfigFile(t, minimalYAML)
	defer cleanup()

	err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() returned an unexpected error: %v", err)
	}

	if AppConfig == nil {
		t.Fatal("AppConfig should not be nil")
	}

	// Check a representative sample of default values
	if AppConfig.Server.Port != "8081" {
		t.Errorf("Expected default Server.Port to be '8081', got '%s'", AppConfig.Server.Port)
	}
	if AppConfig.Server.ReadTimeout != 10*time.Second {
		t.Errorf("Expected default Server.ReadTimeout to be 10s, got %v", AppConfig.Server.ReadTimeout)
	}
	if AppConfig.WebSocket.PongWait != 60*time.Second {
		t.Errorf("Expected default WebSocket.PongWait to be 60s, got %v", AppConfig.WebSocket.PongWait)
	}
	// PingPeriod is derived from PongWait
	expectedPingPeriod := (60 * time.Second * 9) / 10
	if AppConfig.WebSocket.PingPeriod != expectedPingPeriod {
		t.Errorf("Expected default WebSocket.PingPeriod to be %v, got %v", expectedPingPeriod, AppConfig.WebSocket.PingPeriod)
	}
	if AppConfig.Limits.MaxClientsPerTeam != 1000 {
		t.Errorf("Expected default Limits.MaxClientsPerTeam to be 1000, got %d", AppConfig.Limits.MaxClientsPerTeam)
	}
	if AppConfig.Environment.Mode != "production" {
		t.Errorf("Expected default Environment.Mode to be 'production', got '%s'", AppConfig.Environment.Mode)
	}
	expectedOrigins := []string{"*"}
	if !reflect.DeepEqual(AppConfig.Server.AllowedOrigins, expectedOrigins) {
		t.Errorf("Expected default AllowedOrigins to be %v, got %v", expectedOrigins, AppConfig.Server.AllowedOrigins)
	}
}

// TestEnvironmentHelpers tests the various boolean helper functions.
func TestEnvironmentHelpers(t *testing.T) {
	// Defer cleanup to reset AppConfig after the test
	defer func() { AppConfig = nil }()

	testCases := []struct {
		name                   string
		config                 *Config
		expectedIsDevelopment  bool
		expectedIsProduction   bool
		expectedAllowAllOrigins bool
		expectedFakeAuthEnabled bool
	}{
		{
			name:                   "AppConfig is nil",
			config:                 nil,
			expectedIsDevelopment:  false,
			expectedIsProduction:   true,  // Safety default
			expectedAllowAllOrigins: false,
			expectedFakeAuthEnabled: false,
		},
		{
			name: "Production Mode",
			config: &Config{
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production"},
			},
			expectedIsDevelopment:  false,
			expectedIsProduction:   true,
			expectedAllowAllOrigins: false,
			expectedFakeAuthEnabled: false,
		},
		{
			name: "Development Mode",
			config: &Config{
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "development"},
			},
			expectedIsDevelopment:  true,
			expectedIsProduction:   false,
			expectedAllowAllOrigins: true, // Should be true because dev mode implies it
			expectedFakeAuthEnabled: false, // FakeAuth is still false
		},
		{
			name: "Development Mode with Fake Auth",
			config: &Config{
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "development", EnableFakeAuth: true},
			},
			expectedIsDevelopment:  true,
			expectedIsProduction:   false,
			expectedAllowAllOrigins: true,
			expectedFakeAuthEnabled: true,
		},
		{
			name: "Production Mode with AllowAllOrigins override",
			config: &Config{
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production", AllowAllOrigins: true},
			},
			expectedIsDevelopment:  false,
			expectedIsProduction:   true,
			expectedAllowAllOrigins: true, // Overridden
			expectedFakeAuthEnabled: false,
		},
		{
			name: "Production Mode with Fake Auth (should be disabled)",
			config: &Config{
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production", EnableFakeAuth: true},
			},
			expectedIsDevelopment:  false,
			expectedIsProduction:   true,
			expectedAllowAllOrigins: false,
			expectedFakeAuthEnabled: false, // Should be false because not in dev mode
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			AppConfig = tc.config

			if got := IsDevelopment(); got != tc.expectedIsDevelopment {
				t.Errorf("IsDevelopment() = %v, want %v", got, tc.expectedIsDevelopment)
			}
			if got := IsProduction(); got != tc.expectedIsProduction {
				t.Errorf("IsProduction() = %v, want %v", got, tc.expectedIsProduction)
			}
			if got := ShouldAllowAllOrigins(); got != tc.expectedAllowAllOrigins {
				t.Errorf("ShouldAllowAllOrigins() = %v, want %v", got, tc.expectedAllowAllOrigins)
			}
			if got := IsFakeAuthEnabled(); got != tc.expectedFakeAuthEnabled {
				t.Errorf("IsFakeAuthEnabled() = %v, want %v", got, tc.expectedFakeAuthEnabled)
			}
		})
	}
}

// TestIsOriginAllowed tests the detailed logic for origin validation.
func TestIsOriginAllowed(t *testing.T) {
	defer func() { AppConfig = nil }()

	testCases := []struct {
		name          string
		config        *Config
		originToCheck string
		expected      bool
	}{
		{
			name:          "AppConfig is nil",
			config:        nil,
			originToCheck: "http://anywhere.com",
			expected:      false,
		},
		{
			name: "Production - Origin Allowed",
			config: &Config{
				Server: struct {
					Port           string        `yaml:"port"`
					ReadTimeout    time.Duration `yaml:"read_timeout"`
					WriteTimeout   time.Duration `yaml:"write_timeout"`
					IdleTimeout    time.Duration `yaml:"idle_timeout"`
					AllowedOrigins []string      `yaml:"allowed_origins"`
				}{AllowedOrigins: []string{"https://safe.com", "https://trusted.com"}},
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production"},
			},
			originToCheck: "https://safe.com",
			expected:      true,
		},
		{
			name: "Production - Origin Denied",
			config: &Config{
				Server: struct {
					Port           string        `yaml:"port"`
					ReadTimeout    time.Duration `yaml:"read_timeout"`
					WriteTimeout   time.Duration `yaml:"write_timeout"`
					IdleTimeout    time.Duration `yaml:"idle_timeout"`
					AllowedOrigins []string      `yaml:"allowed_origins"`
				}{AllowedOrigins: []string{"https://safe.com"}},
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production"},
			},
			originToCheck: "https://unsafe.com",
			expected:      false,
		},
		{
			name: "Production - Wildcard '*' Allows All",
			config: &Config{
				Server: struct {
					Port           string        `yaml:"port"`
					ReadTimeout    time.Duration `yaml:"read_timeout"`
					WriteTimeout   time.Duration `yaml:"write_timeout"`
					IdleTimeout    time.Duration `yaml:"idle_timeout"`
					AllowedOrigins []string      `yaml:"allowed_origins"`
				}{AllowedOrigins: []string{"*"}},
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production"},
			},
			originToCheck: "https://anything.goes",
			expected:      true,
		},
		{
			name: "Development Mode Allows Any Origin",
			config: &Config{
				Server: struct {
					Port           string        `yaml:"port"`
					ReadTimeout    time.Duration `yaml:"read_timeout"`
					WriteTimeout   time.Duration `yaml:"write_timeout"`
					IdleTimeout    time.Duration `yaml:"idle_timeout"`
					AllowedOrigins []string      `yaml:"allowed_origins"`
				}{AllowedOrigins: []string{"https://safe.com"}}, // This list should be ignored
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "development"},
			},
			originToCheck: "http://localhost:1234",
			expected:      true,
		},
		{
			name: "Production Mode with AllowAllOrigins Override Allows Any Origin",
			config: &Config{
				Server: struct {
					Port           string        `yaml:"port"`
					ReadTimeout    time.Duration `yaml:"read_timeout"`
					WriteTimeout   time.Duration `yaml:"write_timeout"`
					IdleTimeout    time.Duration `yaml:"idle_timeout"`
					AllowedOrigins []string      `yaml:"allowed_origins"`
				}{AllowedOrigins: []string{"https://safe.com"}}, // This list should be ignored
				Environment: struct {
					Mode            string `yaml:"mode"`
					AllowAllOrigins bool   `yaml:"allow_all_origins"`
					EnableFakeAuth  bool   `yaml:"enable_fake_auth"`
				}{Mode: "production", AllowAllOrigins: true},
			},
			originToCheck: "http://another-random-site.com",
			expected:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			AppConfig = tc.config
			if got := IsOriginAllowed(tc.originToCheck); got != tc.expected {
				t.Errorf("IsOriginAllowed('%s') = %v, want %v", tc.originToCheck, got, tc.expected)
			}
		})
	}
}
