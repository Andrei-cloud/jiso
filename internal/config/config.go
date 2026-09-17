package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config is the settings one jiso run acts on: where to connect, which spec and
// transaction files to use, how long to wait, and the session id that ties the
// resulting exchanges together. The TUI edits these while it renders them, so
// every field is reached through the accessors in accessors.go under the
// RWMutex; nothing outside this package reads a field directly.
type Config struct {
	file                string
	host                string
	port                string
	specFileName        string
	reconnectAttempts   int
	connectTimeout      time.Duration
	totalConnectTimeout time.Duration
	responseTimeout     time.Duration
	listenTimeout       time.Duration
	hex                 bool
	dbPath              string
	sessionID           string
	visaStationID       string
	header              string
	tlsConfigPath       string
	tlsConfig           *TLSFileConfig
	mu                  sync.RWMutex
}

var (
	config     *Config
	configOnce sync.Once
)

// GetConfig returns the process-wide Config, seeded on first call with the
// defaults the help text advertises. One instance is the point: the flag layer,
// the user config file and the settings page all edit the same view, and a
// second copy would let one of them lose a write with nothing to show it.
func GetConfig() *Config {
	configOnce.Do(func() {
		config = &Config{
			reconnectAttempts:   3,
			connectTimeout:      5 * time.Second,
			totalConnectTimeout: 10 * time.Second,
			responseTimeout:     5 * time.Second,
			listenTimeout:       5 * time.Minute,
		}
	})
	return config
}

// EnsureDefaults fills the timeouts and the reconnect count that are still
// zero. It is deliberately not a Reset: values an operator set are left alone
// even when Validate will reject them, so a bad setting stays visible instead
// of being quietly replaced by one that happens to work.
func (c *Config) EnsureDefaults() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reconnectAttempts <= 0 {
		c.reconnectAttempts = 3
	}
	if c.connectTimeout <= 0 {
		c.connectTimeout = 5 * time.Second
	}
	if c.totalConnectTimeout <= 0 {
		c.totalConnectTimeout = 10 * time.Second
	}
	if c.responseTimeout <= 0 {
		c.responseTimeout = 5 * time.Second
	}
	if c.listenTimeout <= 0 {
		c.listenTimeout = 5 * time.Minute
	}
}

// Reset returns every field to its untouched state and re-seeds the timeout
// defaults. It exists for tests, which must not inherit another test's host,
// spec or TLS material; nothing in the CLI calls it, because a command that
// lost its configuration would be worse than one that kept it.
func (c *Config) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.host = ""
	c.port = ""
	c.specFileName = ""
	c.file = ""
	c.hex = false
	c.dbPath = ""
	c.visaStationID = ""
	c.header = ""
	c.tlsConfigPath = ""
	c.tlsConfig = nil
	c.sessionID = ""
	c.reconnectAttempts = 3
	c.connectTimeout = 5 * time.Second
	c.totalConnectTimeout = 10 * time.Second
	c.responseTimeout = 5 * time.Second
	c.listenTimeout = 5 * time.Minute
}

// EnsureSessionID mints a session id only when none exists, so the id that
// ties one run's logged exchanges together survives every later flag or
// config-layer write. A caller that wants a genuinely new run calls Rotate.
func (c *Config) EnsureSessionID() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID == "" {
		c.sessionID = generateSessionID()
	}
}

// SetSessionID adopts an id chosen elsewhere, including the empty string,
// which is how a command clears its session identity before re-deriving it.
func (c *Config) SetSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}

// RotateSessionID starts a new session and returns its id. The TUI's "new
// session" action uses it so the exchanges after it are logged under an id the
// operator can find in the session browser, apart from the ones before.
func (c *Config) RotateSessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = generateSessionID()
	return c.sessionID
}

// GetSessionID returns the current session id, empty until something mints
// one; the persistent pre-run ensures one exists before a command reads it.
func (c *Config) GetSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// GetVisaStationID returns the acquiring-station id that field 7 of a composed
// message carries, "" when the operator supplied none (the composer then omits
// the field rather than sending a blank).
func (c *Config) GetVisaStationID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.visaStationID
}

// SetVisaStationID records the acquiring-station id. Like the other setters it
// takes the operator's spelling verbatim; the spec, not the config, decides
// whether the value fits the field it lands in.
func (c *Config) SetVisaStationID(stationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.visaStationID = stationID
}

// MissingFileError names a referenced config file (spec, transaction,
// database parent directory, or TLS config) that failed an existence check.
// Frontends extract Path to name the offending file in exit-3 messages
// instead of guessing from unrelated config paths.
type MissingFileError struct {
	// Kind is the human descriptor prefix, e.g. "spec file".
	Kind string
	// Path is the offending file or directory.
	Path string
}

// Error renders what the operator reads: the kind of reference and the path
// that failed, because naming the offending file is the whole reason this type
// exists instead of a plain fmt.Errorf.
func (e *MissingFileError) Error() string {
	return fmt.Sprintf("%s does not exist: %s", e.Kind, e.Path)
}

// Validate checks config values and that referenced spec/tx/db/TLS files
// exist. Commands that consume the spec/tx files validate with this;
// commands that run independently of them use ValidateValues. File
// existence failures return a *MissingFileError naming the file.
func (c *Config) Validate() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Validate spec file if provided
	if c.specFileName != "" {
		if _, err := os.Stat(c.specFileName); os.IsNotExist(err) {
			return &MissingFileError{Kind: "spec file", Path: c.specFileName}
		}
	}

	// Validate transaction file if provided
	if c.file != "" {
		if _, err := os.Stat(c.file); os.IsNotExist(err) {
			return &MissingFileError{Kind: "transaction file", Path: c.file}
		}
	}

	if err := c.validateValues(); err != nil {
		return err
	}

	// Validate database path if provided
	if c.dbPath != "" {
		if _, err := os.Stat(c.dbPath); os.IsNotExist(err) {
			// Check if parent directory exists
			parentDir := filepath.Dir(c.dbPath)
			if _, err := os.Stat(parentDir); os.IsNotExist(err) {
				return &MissingFileError{Kind: "database parent directory", Path: parentDir}
			}
		}
	}

	// Validate TLS config path if provided
	if c.tlsConfigPath != "" {
		if _, err := os.Stat(c.tlsConfigPath); os.IsNotExist(err) {
			return &MissingFileError{Kind: "TLS config file", Path: c.tlsConfigPath}
		}
	}

	return nil
}

// ValidateValues checks numeric config values only, without touching the
// referenced spec/tx files. Commands that run independently of those files
// (repl, stubs, completion, help, version) use this so a bogus --spec/--file
// path cannot block them (CLI-105).
func (c *Config) ValidateValues() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.validateValues()
}

func (c *Config) validateValues() error {
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

	return nil
}

func generateSessionID() string {
	return uuid.New().String()
}
