package connection

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"
)

// SetListenTimeout sets the timeout for waiting for an incoming connection in listener mode
func (m *Manager) SetListenTimeout(timeout time.Duration) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.listenTimeout = timeout
}

// GetListenTimeout returns the timeout for listener mode
func (m *Manager) GetListenTimeout() time.Duration {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	if m.listenTimeout <= 0 {
		return 5 * time.Minute
	}
	return m.listenTimeout
}

// IsListening returns whether the manager is currently configured in listener mode
func (m *Manager) IsListening() bool {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.listenMode
}

// Listen starts a TCP listener on the specified port and waits for an incoming remote connection.
// Once a remote client connects, it wraps the connection as an ISO8583 client connection.
func (m *Manager) Listen(port string, naps bool, header network.Header) error {
	// Store connection parameters for potential reconnection/re-listening
	m.setConnParams(naps, header)

	l, timeout, err := m.setupListener(port)
	if err != nil {
		return err
	}

	if m.debugMode.Load() {
		outputf("Listener active on port %s. Waiting up to %v for incoming connection...\n", port, timeout)
	}

	rawConn, err := m.acceptWithTimeout(l, port, timeout)
	if err != nil {
		return err
	}

	// Single connection model: close the TCP listener now that we accepted our 1 connection
	m.closeListener()

	if m.debugMode.Load() {
		outputf("Accepted incoming connection from %s on port %s\n", rawConn.RemoteAddr(), port)
	}

	readFunc, writeFunc := m.lengthFuncs(naps, header)
	options := m.buildConnectionOptions()

	m.statusMu.Lock()
	conn, err := moovconnection.NewFrom(
		rawConn,
		m.spec,
		readFunc,
		writeFunc,
		options...,
	)
	if err != nil {
		m.statusMu.Unlock()
		_ = rawConn.Close() // rollback path: the wrap error is the reportable one

		return fmt.Errorf("failed to wrap accepted connection: %w", err)
	}

	m.Connection = conn
	m.Connection.SetStatus(moovconnection.StatusOnline)
	m.statusMu.Unlock()

	// Enable Visa SMC Heartbeat keep-alive if Visa header format is selected
	m.applyVisaSMC(header)

	m.notifyConnectionChange(conn)
	return nil
}

// setupListener records listen state, tears down any existing connection or
// listener, binds the TCP (or TLS) listener, and returns it with the accept
// timeout to wait for the first incoming connection.
func (m *Manager) setupListener(port string) (net.Listener, time.Duration, error) {
	m.statusMu.Lock()
	m.listenMode = true
	m.listenPort = port
	m.address = fmt.Sprintf("0.0.0.0:%s", port)

	// Clean up any existing connection or listener
	if m.Connection != nil || m.listener != nil {
		if m.debugMode.Load() {
			outputf("Cleaning up existing connection/listener on port %s\n", port)
		}
		_ = m.closeUnlocked()
	}
	m.listenMode = true
	m.listenPort = port

	addr := fmt.Sprintf(":%s", port)

	// The address is ":port" -- a numeric wildcard bind, so there is no name to
	// resolve and no peer to wait for, which is what a context on Listen would
	// cancel. Every caller (the CLI command, the TUI, the app layer) is itself
	// context-free at this point, so threading one would mean changing a public
	// signature to carry something with nothing in it.
	//nolint:noctx // nothing between here and the bind can block on resolution
	l, err := net.Listen("tcp", addr)
	if err != nil {
		m.statusMu.Unlock()

		return nil, 0, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if m.tlsConfig != nil {
		tlsCfg := m.tlsConfig.Clone()
		if tlsCfg.ClientCAs != nil && tlsCfg.ClientAuth == tls.NoClientCert {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
		l = tls.NewListener(l, tlsCfg)
	}

	m.listener = l
	timeout := m.listenTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	m.statusMu.Unlock()

	return l, timeout, nil
}

// acceptWithTimeout accepts a single connection from l, closing the listener and
// returning an error when Accept fails or no connection arrives within timeout.
func (m *Manager) acceptWithTimeout(l net.Listener, port string, timeout time.Duration) (net.Conn, error) {
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan acceptResult, 1)

	go func() {
		conn, err := l.Accept()
		ch <- acceptResult{conn: conn, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			m.closeListener()
			if strings.Contains(res.err.Error(), "closed") {
				return nil, fmt.Errorf("listener closed")
			}

			return nil, fmt.Errorf("error accepting connection on port %s: %w", port, res.err)
		}

		return res.conn, nil
	case <-time.After(timeout):
		m.closeListener()

		return nil, fmt.Errorf("listener timed out after %v waiting for incoming connection on port %s", timeout, port)
	}
}

// closeListener closes and clears the TCP listener under the status lock.
func (m *Manager) closeListener() {
	m.statusMu.Lock()
	if m.listener != nil {
		_ = m.listener.Close()
		m.listener = nil
	}
	m.statusMu.Unlock()
}

// attemptReListen handles automatic re-listening when remote host disconnects in listener mode
func (m *Manager) attemptReListen() {
	m.reconnectMu.Lock()
	if m.reconnecting {
		m.reconnectMu.Unlock()
		return
	}
	m.reconnecting = true
	m.reconnectMu.Unlock()

	defer func() {
		m.reconnectMu.Lock()
		m.reconnecting = false
		m.reconnectMu.Unlock()
	}()

	m.statusMu.RLock()
	listenMode := m.listenMode
	port := m.listenPort
	m.statusMu.RUnlock()
	naps, header := m.connParams()

	if !listenMode || port == "" {
		return
	}

	if m.debugMode.Load() {
		outputf("Connection closed. Re-opening listener on port %s...\n", port)
	}

	// Backoff loop for re-listening
	maxBackoff := 30 * time.Second
	baseDelay := 1 * time.Second

	for attempt := 1; attempt <= m.reconnectAttempts || m.reconnectAttempts == 0; attempt++ {
		m.statusMu.RLock()
		currentMode := m.listenMode
		m.statusMu.RUnlock()
		if !currentMode {
			return // Listener was explicitly closed
		}

		if attempt > 1 {
			time.Sleep(reListenBackoff(attempt, baseDelay, maxBackoff))
		}

		err := m.Listen(port, naps, header)
		if err == nil {
			if m.debugMode.Load() {
				outputf("Re-listen successful on port %s\n", port)
			}
			return
		}

		if errors.Is(err, fmt.Errorf("listener closed")) {
			return
		}

		if m.debugMode.Load() {
			outputf("Re-listen attempt %d failed: %v\n", attempt, err)
		}

		if m.reconnectAttempts > 0 && attempt >= m.reconnectAttempts {
			break
		}
	}
}

// reListenBackoff is the exponential backoff delay before re-listen attempt
// (1-based), capped at maxBackoff.
func reListenBackoff(attempt int, baseDelay, maxBackoff time.Duration) time.Duration {
	delay := time.Duration(1<<uint(attempt-1)) * baseDelay
	if delay > maxBackoff {
		delay = maxBackoff
	}

	return delay
}
