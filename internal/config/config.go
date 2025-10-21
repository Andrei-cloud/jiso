package config

import (
	"flag"
	"fmt"
	"os"
	"sync"
)

type Config struct {
	file              string
	host              string
	port              string
	specFileName      string
	reconnectAttempts int
	mu                sync.RWMutex
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
	c.file = *file

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
