package client

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ReconnectNotifier represents an interface to notify connection changes
type ReconnectNotifier interface {
	ReconnectWithTarget(host, port string) error
}

// ClientConfig holds thread-safe client connection target settings
type ClientConfig struct {
	mu          sync.RWMutex
	host        string
	port        string
	reconnector ReconnectNotifier
	lastSwapped time.Time
}

// NewClientConfig creates a thread-safe ClientConfig instance
func NewClientConfig(host, port string, reconnector ReconnectNotifier) *ClientConfig {
	return &ClientConfig{
		host:        host,
		port:        port,
		reconnector: reconnector,
		lastSwapped: time.Now(),
	}
}

// GetTarget returns the current host and port
func (c *ClientConfig) GetTarget() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.host, c.port
}

// GetAddress returns the formatted host:port string
func (c *ClientConfig) GetAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%s:%s", c.host, c.port)
}

// SetTarget updates the target endpoint and optionally triggers re-dialing
func (c *ClientConfig) SetTarget(target string) error {
	host, port, err := parseTargetAddress(target)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.host = host
	c.port = port
	c.lastSwapped = time.Now()
	reconnector := c.reconnector
	c.mu.Unlock()

	if reconnector != nil {
		if err := reconnector.ReconnectWithTarget(host, port); err != nil {
			return fmt.Errorf("target updated to %s:%s but reconnect failed: %w", host, port, err)
		}
	}
	return nil
}

// SetReconnector binds a ReconnectNotifier callback
func (c *ClientConfig) SetReconnector(r ReconnectNotifier) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnector = r
}

func parseTargetAddress(target string) (string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("empty target address")
	}

	if strings.Contains(target, ":") {
		host, portStr, err := net.SplitHostPort(target)
		if err != nil {
			// Try manual split if host missing
			parts := strings.Split(target, ":")
			if len(parts) == 2 {
				host = parts[0]
				portStr = parts[1]
			} else {
				return "", "", fmt.Errorf("invalid address format '%s': %w", target, err)
			}
		}

		if host == "" {
			host = "localhost"
		}

		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			return "", "", fmt.Errorf("invalid port number: %s", portStr)
		}
		return host, portStr, nil
	}

	// Just a port number provided
	if p, err := strconv.Atoi(target); err == nil && p > 0 && p <= 65535 {
		return "localhost", target, nil
	}

	// Hostname without port defaults to retaining port or default 9999
	return target, "9999", nil
}
