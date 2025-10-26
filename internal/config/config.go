package config

import (
	"flag"
	"fmt"
	"os"
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

func generateSessionId() string {
	return uuid.New().String()
}
