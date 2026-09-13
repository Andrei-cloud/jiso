// accessors.go is the Config read/write surface: every field is reached here
// and nowhere else, under the RWMutex, because the settings page writes while
// the pages render reads. Two rules recur in the setters and are easy to break
// by "simplifying" them: an empty argument is ignored rather than stored (an
// unset flag arrives as "" and must not clear a value the config file set), and
// value checks live in Validate, not here.
package config

import (
	"strings"
	"time"
)

// SetHost records the peer to connect to. An empty argument is ignored rather
// than stored: an unset --host arrives as "" and would otherwise clear a host
// the config file or the connect form already set.
func (c *Config) SetHost(host string) {
	if host == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.host = host
}

// SetPort records the peer port as the operator typed it, since a service
// name like "iso8583" is as valid as a number; "" is ignored for the reason
// SetHost states.
func (c *Config) SetPort(port string) {
	if port == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.port = port
}

// SetSpec records the message spec that defines the MTIs and fields every
// later command reads. "" is ignored: a spec is set once and clearing it
// implicitly would turn the next command into "no spec loaded".
func (c *Config) SetSpec(specFileName string) {
	if specFileName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specFileName = specFileName
}

// SetHex chooses hex rendering for message bodies in CLI output and the
// message views. There is no validation to do, which is why this setter is
// bare.
func (c *Config) SetHex(hex bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hex = hex
}

// SetDbPath records the session database that exchanges are logged into.
// Empty means session logging is off, and the session and CTF panes say so
// with an empty state that names the missing flag.
func (c *Config) SetDbPath(dbPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dbPath = dbPath
}

// SetFile records the transaction file whose messages a command sends.
func (c *Config) SetFile(file string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file = file
}

// SetReconnectAttempts sets how many retries a dropped connection gets before
// the command gives up. The range is Validate's business, not this setter's,
// so a CLI flag can be inspected before it is judged.
func (c *Config) SetReconnectAttempts(attempts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnectAttempts = attempts
}

// SetConnectTimeout bounds a single dial attempt.
func (c *Config) SetConnectTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectTimeout = timeout
}

// SetTotalConnectTimeout bounds the whole reconnect sequence, all attempts
// together. Validate requires at least one connect timeout's worth, because a
// total shorter than one attempt would fail before the first dial finished.
func (c *Config) SetTotalConnectTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalConnectTimeout = timeout
}

// SetResponseTimeout bounds how long a sent message waits for its reply
// before the step is recorded as failed.
func (c *Config) SetResponseTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseTimeout = timeout
}

// SetListenTimeout bounds how long listen mode waits for an inbound message.
// Its default is minutes, not seconds, because listening waits for a human or
// an upstream, not for a round trip.
func (c *Config) SetListenTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listenTimeout = timeout
}

// GetHost returns the peer host, empty until a flag, the config file, or the
// connect form sets it. An empty host is what the connect form reports as not
// configured.
func (c *Config) GetHost() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.host
}

// GetPort returns the peer port in the operator's spelling, empty when unset.
func (c *Config) GetPort() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.port
}

// GetSpec returns the spec file path, "" when the operator supplied none.
func (c *Config) GetSpec() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.specFileName
}

// GetFile returns the transaction file path, "" when none was supplied.
func (c *Config) GetFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.file
}

// GetReconnectAttempts returns the retry count for a dropped connection.
func (c *Config) GetReconnectAttempts() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reconnectAttempts
}

// GetConnectTimeout returns the per-dial timeout.
func (c *Config) GetConnectTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connectTimeout
}

// GetTotalConnectTimeout returns the whole-reconnect-sequence timeout.
func (c *Config) GetTotalConnectTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalConnectTimeout
}

// GetResponseTimeout returns the wait-for-reply timeout.
func (c *Config) GetResponseTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.responseTimeout
}

// GetListenTimeout returns the listen-mode wait.
func (c *Config) GetListenTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.listenTimeout
}

// GetHex reports whether message bodies render as hex.
func (c *Config) GetHex() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hex
}

// GetDbPath returns the session database path, "" when session logging is
// off.
func (c *Config) GetDbPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dbPath
}

// GetHeader returns the selected network header, "" when none was chosen.
func (c *Config) GetHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.header
}

// SetHeader records which network header wraps messages on the wire. The
// value must match a header the connection layer knows; the spec and the peer
// decide, not this package.
func (c *Config) SetHeader(header string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.header = header
}

// SetTLSConfigPath loads path and records it only if it parses. A malformed
// TLS file must fail the command that named it, not surface later as an
// opaque handshake error; an empty path is not an error, since TLS is off
// unless a config is named.
func (c *Config) SetTLSConfigPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	tlsCfg, err := LoadTLSConfig(path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tlsConfigPath = path
	c.tlsConfig = tlsCfg
	return nil
}

// GetTLSConfigPath returns the TLS config file the operator named, "" when
// TLS is off.
func (c *Config) GetTLSConfigPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tlsConfigPath
}

// GetTLSConfig returns the parsed TLS config, nil when none was loaded.
// Callers must treat nil as "connect in the clear", not as an error, which is
// why the path getter exists separately.
func (c *Config) GetTLSConfig() *TLSFileConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tlsConfig
}

// SetTLSConfig adopts an already-loaded TLS config, the path callers take
// when they parsed the file themselves (the settings page reloads it to show
// validation errors before applying).
func (c *Config) SetTLSConfig(cfg *TLSFileConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tlsConfig = cfg
}
