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

	"jiso/internal/utils"
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
	m.statusMu.Lock()
	// Store connection parameters for potential reconnection/re-listening
	m.naps = naps
	m.header = header
	m.listenMode = true
	m.listenPort = port
	m.address = fmt.Sprintf("0.0.0.0:%s", port)

	// Clean up any existing connection or listener
	if m.Connection != nil || m.listener != nil {
		if m.debugMode {
			fmt.Printf("Cleaning up existing connection/listener on port %s\n", port)
		}
		m.closeUnlocked()
	}
	m.listenMode = true
	m.listenPort = port

	addr := fmt.Sprintf(":%s", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		m.statusMu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
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

	if m.debugMode {
		fmt.Printf("Listener active on port %s. Waiting up to %v for incoming connection...\n", port, timeout)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan acceptResult, 1)

	go func() {
		conn, err := l.Accept()
		ch <- acceptResult{conn: conn, err: err}
	}()

	var rawConn net.Conn
	select {
	case res := <-ch:
		if res.err != nil {
			m.statusMu.Lock()
			if m.listener != nil {
				_ = m.listener.Close()
				m.listener = nil
			}
			m.statusMu.Unlock()
			if strings.Contains(res.err.Error(), "closed") {
				return fmt.Errorf("listener closed")
			}
			return fmt.Errorf("error accepting connection on port %s: %w", port, res.err)
		}
		rawConn = res.conn
	case <-time.After(timeout):
		m.statusMu.Lock()
		if m.listener != nil {
			_ = m.listener.Close()
			m.listener = nil
		}
		m.statusMu.Unlock()
		return fmt.Errorf("listener timed out after %v waiting for incoming connection on port %s", timeout, port)
	}

	// Single connection model: close the TCP listener now that we accepted our 1 connection
	m.statusMu.Lock()
	if m.listener != nil {
		_ = m.listener.Close()
		m.listener = nil
	}
	m.statusMu.Unlock()

	if m.debugMode {
		fmt.Printf("Accepted incoming connection from %s on port %s\n", rawConn.RemoteAddr(), port)
	}

	readHeader := cloneHeader(header)
	writeHeader := cloneHeader(header)

	readFunc := utils.ReadMessageLengthWrapper(readHeader)
	writeFunc := utils.WriteMessageLengthWrapper(writeHeader)
	if naps {
		readFunc = utils.NapsReadLengthWrapper(readFunc)
		writeFunc = utils.NapsWriteLengthWrapper(writeFunc)
	}

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
		rawConn.Close()
		return fmt.Errorf("failed to wrap accepted connection: %w", err)
	}

	m.Connection = conn
	m.Connection.SetStatus(moovconnection.StatusOnline)

	// Enable Visa SMC Heartbeat keep-alive if Visa header format is selected
	if IsVisaHeader(header) {
		if m.smcDaemon != nil {
			m.smcDaemon.Stop()
		}
		m.smcDaemon = NewSMCHeartbeatDaemon(m, 30*time.Second)
		m.smcDaemon.Start()
	} else if m.smcDaemon != nil {
		m.smcDaemon.Stop()
		m.smcDaemon = nil
	}

	m.statusMu.Unlock()
	return nil
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
	naps := m.naps
	header := m.header
	m.statusMu.RUnlock()

	if !listenMode || port == "" {
		return
	}

	if m.debugMode {
		fmt.Printf("Connection closed. Re-opening listener on port %s...\n", port)
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
			delay := time.Duration(1<<uint(attempt-1)) * baseDelay
			if delay > maxBackoff {
				delay = maxBackoff
			}
			time.Sleep(delay)
		}

		err := m.Listen(port, naps, header)
		if err == nil {
			if m.debugMode {
				fmt.Printf("Re-listen successful on port %s\n", port)
			}
			return
		}

		if errors.Is(err, fmt.Errorf("listener closed")) {
			return
		}

		if m.debugMode {
			fmt.Printf("Re-listen attempt %d failed: %v\n", attempt, err)
		}

		if m.reconnectAttempts > 0 && attempt >= m.reconnectAttempts {
			break
		}
	}
}
