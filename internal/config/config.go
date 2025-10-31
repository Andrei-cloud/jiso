package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	file                string
	host                string
	port                string
	specFileName        string
	reconnectAttempts   int
	connectTimeout      time.Duration
	totalConnectTimeout time.Duration
	responseTimeout     time.Duration
	hex                 bool
	dbPath              string
	sessionId           string
	mu                  sync.RWMutex
}

var (
	config     *Config
	configOnce sync.Once
)

func GetConfig() *Config {
	configOnce.Do(func() {
		config = &Config{}
	})
	return config
}

func (c *Config) Parse() error {
	host := flag.String("host", "", "Hostname to connect to")
	port := flag.String("port", "", "Port to connect to")
	specFileName := flag.String(
		"spec-file",
		"",
		"path to customized specification file in JSON format",
	)
	file := flag.String("file", "", "path to transaction file in JSON format")
	reconnectAttempts := flag.Int(
		"reconnect-attempts",
		3,
		"number of reconnection attempts on connection failure",
	)
	connectTimeout := flag.Duration(
		"connect-timeout",
		5*time.Second,
		"timeout for individual connection attempts",
	)
	totalConnectTimeout := flag.Duration(
		"total-connect-timeout",
		10*time.Second,
		"total timeout for connection establishment",
	)
	responseTimeout := flag.Duration(
		"response-timeout",
		5*time.Second,
		"timeout for waiting responses to async messages",
	)
	hex := flag.Bool("hex", false, "enable hex dump output for messages")
	dbPath := flag.String("db-path", "", "path to SQLite database file for storing sessions")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: jiso [OPTIONS]\n")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	c.host = *host
	c.port = *port
	c.specFileName = *specFileName
	c.reconnectAttempts = *reconnectAttempts
	c.connectTimeout = *connectTimeout
	c.totalConnectTimeout = *totalConnectTimeout
	c.responseTimeout = *responseTimeout
	c.file = *file
	c.hex = *hex
	c.dbPath = *dbPath
	c.sessionId = generateSessionId()

	return nil
}

func (c *Config) SetHost(host string) {
	if host == "" {
		return
	}
	c.host = host
}

func (c *Config) SetPort(port string) {
	if port == "" {
		return
	}
	c.port = port
}

func (c *Config) SetSpec(specFileName string) {
	if specFileName == "" {
		return
	}
	c.specFileName = specFileName
}

func (c *Config) SetFile(file string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file = file
}

func (c *Config) SetReconnectAttempts(attempts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnectAttempts = attempts
}

func (c *Config) SetConnectTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectTimeout = timeout
}

func (c *Config) SetTotalConnectTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalConnectTimeout = timeout
}

func (c *Config) SetResponseTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseTimeout = timeout
}

func (c *Config) GetHost() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.host
}

func (c *Config) GetPort() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.port
}

func (c *Config) GetSpec() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.specFileName
}

func (c *Config) GetFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.file
}

func (c *Config) GetReconnectAttempts() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reconnectAttempts
}

func (c *Config) GetConnectTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connectTimeout
}

func (c *Config) GetTotalConnectTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalConnectTimeout
}

func (c *Config) GetResponseTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.responseTimeout
}

func (c *Config) GetHex() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hex
}

func (c *Config) GetDbPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dbPath
}

func (c *Config) GetSessionId() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionId
}

func (c *Config) Validate() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Validate host
	if c.host == "" {
		return fmt.Errorf("host is required")
	}

	// Validate port
	if c.port == "" {
		return fmt.Errorf("port is required")
	}

	// Validate spec file
	if c.specFileName == "" {
		return fmt.Errorf("spec file is required")
	}
	if _, err := os.Stat(c.specFileName); os.IsNotExist(err) {
		return fmt.Errorf("spec file does not exist: %s", c.specFileName)
	}

	// Validate transaction file
	if c.file == "" {
		return fmt.Errorf("transaction file is required")
	}
	if _, err := os.Stat(c.file); os.IsNotExist(err) {
		return fmt.Errorf("transaction file does not exist: %s", c.file)
	}

	// Validate reconnect attempts
	if c.reconnectAttempts < 0 {
		return fmt.Errorf("reconnect attempts must be non-negative, got %d", c.reconnectAttempts)
	}
	if c.reconnectAttempts > 100 {
		return fmt.Errorf("reconnect attempts too high, got %d (max 100)", c.reconnectAttempts)
	}

	// Validate timeouts
	if c.connectTimeout <= 0 {
		return fmt.Errorf("connect timeout must be positive, got %v", c.connectTimeout)
	}
	if c.connectTimeout > 5*time.Minute {
		return fmt.Errorf("connect timeout too high, got %v (max 5m)", c.connectTimeout)
	}

	if c.totalConnectTimeout <= 0 {
		return fmt.Errorf("total connect timeout must be positive, got %v", c.totalConnectTimeout)
	}
	if c.totalConnectTimeout > 10*time.Minute {
		return fmt.Errorf("total connect timeout too high, got %v (max 10m)", c.totalConnectTimeout)
	}

	if c.responseTimeout <= 0 {
		return fmt.Errorf("response timeout must be positive, got %v", c.responseTimeout)
	}
	if c.responseTimeout > 10*time.Minute {
		return fmt.Errorf("response timeout too high, got %v (max 10m)", c.responseTimeout)
	}

	// Validate total timeout is greater than connect timeout
	if c.totalConnectTimeout < c.connectTimeout {
		return fmt.Errorf(
			"total connect timeout (%v) must be greater than or equal to connect timeout (%v)",
			c.totalConnectTimeout,
			c.connectTimeout,
		)
	}

	// Validate database path if provided
	if c.dbPath != "" {
		if _, err := os.Stat(c.dbPath); os.IsNotExist(err) {
			// Check if parent directory exists
			parentDir := filepath.Dir(c.dbPath)
			if _, err := os.Stat(parentDir); os.IsNotExist(err) {
				return fmt.Errorf("database parent directory does not exist: %s", parentDir)
			}
		}
	}

	return nil
}

func generateSessionId() string {
	return uuid.New().String()
}
