package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestGetConfig(t *testing.T) {
	config1 := GetConfig()
	if config1 == nil {
		t.Fatal("GetConfig returned nil")
	}

	config2 := GetConfig()
	if config1 != config2 {
		t.Error("GetConfig is not returning singleton instance")
	}
}

func TestConfigSettersAndGetters(t *testing.T) {
	config := GetConfig()

	// Test SetHost and GetHost
	config.SetHost("testhost")
	if config.GetHost() != "testhost" {
		t.Errorf("Expected host 'testhost', got '%s'", config.GetHost())
	}

	// Test SetPort and GetPort
	config.SetPort("9999")
	if config.GetPort() != "9999" {
		t.Errorf("Expected port '9999', got '%s'", config.GetPort())
	}

	// Test SetSpec and GetSpec
	config.SetSpec("testspec.json")
	if config.GetSpec() != "testspec.json" {
		t.Errorf("Expected spec 'testspec.json', got '%s'", config.GetSpec())
	}

	// Test SetFile and GetFile
	config.SetFile("testfile.json")
	if config.GetFile() != "testfile.json" {
		t.Errorf("Expected file 'testfile.json', got '%s'", config.GetFile())
	}

	// Test SetReconnectAttempts and GetReconnectAttempts
	config.SetReconnectAttempts(5)
	if config.GetReconnectAttempts() != 5 {
		t.Errorf("Expected reconnect attempts 5, got %d", config.GetReconnectAttempts())
	}

	// Test SetConnectTimeout and GetConnectTimeout
	timeout := 10 * time.Second
	config.SetConnectTimeout(timeout)
	if config.GetConnectTimeout() != timeout {
		t.Errorf("Expected connect timeout %v, got %v", timeout, config.GetConnectTimeout())
	}

	// Test SetTotalConnectTimeout and GetTotalConnectTimeout
	totalTimeout := 20 * time.Second
	config.SetTotalConnectTimeout(totalTimeout)
	if config.GetTotalConnectTimeout() != totalTimeout {
		t.Errorf(
			"Expected total connect timeout %v, got %v",
			totalTimeout,
			config.GetTotalConnectTimeout(),
		)
	}
}

func TestConfigSetHostEmpty(t *testing.T) {
	config := GetConfig()
	config.SetHost("initial")
	config.SetHost("") // Should not change
	if config.GetHost() != "initial" {
		t.Errorf("Expected host 'initial', got '%s'", config.GetHost())
	}
}

func TestConfigSetPortEmpty(t *testing.T) {
	config := GetConfig()
	config.SetPort("initial")
	config.SetPort("") // Should not change
	if config.GetPort() != "initial" {
		t.Errorf("Expected port 'initial', got '%s'", config.GetPort())
	}
}

func TestConfigSetSpecEmpty(t *testing.T) {
	config := GetConfig()
	config.SetSpec("initial")
	config.SetSpec("") // Should not change
	if config.GetSpec() != "initial" {
		t.Errorf("Expected spec 'initial', got '%s'", config.GetSpec())
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		setupConfig func(*Config)
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
				c.reconnectAttempts = 3
				c.connectTimeout = time.Second
				c.totalConnectTimeout = 2 * time.Second
				c.responseTimeout = time.Second
			},
			expectError: false,
		},
		{
			name: "missing host",
			setupConfig: func(c *Config) {
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
			},
			expectError: true,
			errorMsg:    "host is required",
		},
		{
			name: "missing port",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
			},
			expectError: true,
			errorMsg:    "port is required",
		},
		{
			name: "missing spec file",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.file = createTempFile(t, "transactions.json", "[]")
			},
			expectError: true,
			errorMsg:    "spec file is required",
		},
		{
			name: "missing transaction file",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
			},
			expectError: true,
			errorMsg:    "transaction file is required",
		},
		{
			name: "non-existent spec file",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = "nonexistent.json"
				c.file = createTempFile(t, "transactions.json", "[]")
			},
			expectError: true,
			errorMsg:    "spec file does not exist",
		},
		{
			name: "negative reconnect attempts",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
				c.reconnectAttempts = -1
			},
			expectError: true,
			errorMsg:    "reconnect attempts must be non-negative",
		},
		{
			name: "too many reconnect attempts",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
				c.reconnectAttempts = 101
			},
			expectError: true,
			errorMsg:    "reconnect attempts too high",
		},
		{
			name: "zero connect timeout",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
				c.connectTimeout = 0
				c.totalConnectTimeout = time.Second
				c.responseTimeout = time.Second
			},
			expectError: true,
			errorMsg:    "connect timeout must be positive",
		},
		{
			name: "total timeout less than connect timeout",
			setupConfig: func(c *Config) {
				c.host = "localhost"
				c.port = "9999"
				c.specFileName = createTempFile(t, "test.json", "{}")
				c.file = createTempFile(t, "transactions.json", "[]")
				c.connectTimeout = 2 * time.Second
				c.totalConnectTimeout = time.Second
				c.responseTimeout = time.Second
			},
			expectError: true,
			errorMsg:    "must be greater than or equal to connect timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh config for each test
			config := &Config{}

			// Setup the config as specified
			tt.setupConfig(config)

			// Run validation
			err := config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func createTempFile(t *testing.T, name, content string) string {
	file, err := os.CreateTemp("", name)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	return file.Name()
}
