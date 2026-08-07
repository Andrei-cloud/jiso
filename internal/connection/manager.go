package connection

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"jiso/internal/metrics"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	iso8583errors "github.com/moov-io/iso8583/errors"
	"github.com/moov-io/iso8583/network"
	isoutl "github.com/moov-io/iso8583/utils"
)

type Manager struct {
	Connection          *moovconnection.Connection // Expose Connection as public for backward compatibility
	address             string
	spec                *iso8583.MessageSpec
	debugMode           bool
	reconnectAttempts   int
	connectTimeout      time.Duration
	totalConnectTimeout time.Duration
	reconnecting        bool
	reconnectMu         sync.Mutex
	networkStats        *metrics.NetworkingStats
	statusMu            sync.RWMutex // Protects connection status updates
	mockMatcher         RouteMatcher

	// Connection parameters for reconnection
	naps   bool
	header network.Header

	// Async processing fields
	pendingRequests    map[string]*pendingRequest
	pendingMu          sync.RWMutex
	maxPendingRequests int // Maximum number of pending requests
	responseTimeout    time.Duration
}

func NewManager(
	host, port string,
	spec *iso8583.MessageSpec,
	debugMode bool,
	reconnectAttempts int,
	connectTimeout, totalConnectTimeout time.Duration,
	networkStats *metrics.NetworkingStats,
) *Manager {
	return &Manager{
		address:             fmt.Sprintf("%s:%s", host, port),
		spec:                spec,
		debugMode:           debugMode,
		reconnectAttempts:   reconnectAttempts,
		connectTimeout:      connectTimeout,
		totalConnectTimeout: totalConnectTimeout,
		networkStats:        networkStats,
		pendingRequests:     make(map[string]*pendingRequest),
		responseTimeout:     5 * time.Second, // Default 5s timeout
		maxPendingRequests:  100,             // Default max 100 pending requests
	}
}

// Connect establishes a connection with the ISO8583 server
func (m *Manager) Connect(naps bool, header network.Header) error {
	// Store connection parameters for potential reconnection
	m.naps = naps
	m.header = header

	// Always clean up any existing connection before attempting a new one
	// This prevents issues with stale connections that may appear online but are actually closed
	if m.Connection != nil {
		if m.debugMode {
			fmt.Printf("Cleaning up existing connection to %s\n", m.address)
		}
		m.Close()
		m.Connection = nil
	}

	var err error
	// Clone headers for reading and writing to avoid race conditions
	// Reader and Writer run in separate goroutines
	readHeader := cloneHeader(header)
	writeHeader := cloneHeader(header)

	readFunc := utils.ReadMessageLengthWrapper(readHeader)
	writeFunc := utils.WriteMessageLengthWrapper(writeHeader)
	if naps {
		readFunc = utils.NapsReadLengthWrapper(readFunc)
		writeFunc = utils.NapsWriteLengthWrapper(writeFunc)
	}

	// Add connection options with proper reconnection settings
	options := []moovconnection.Option{
		moovconnection.ConnectTimeout(m.connectTimeout),
		moovconnection.ErrorHandler(func(err error) {
			if m.debugMode {
				fmt.Printf("Error encountered: %s\n", err)
			}

			var unpackErr *iso8583errors.UnpackError
			if errors.As(err, &unpackErr) {
				fmt.Printf("Unpack error: %s\n", unpackErr)
				fmt.Printf("\n%v\n", hex.Dump(unpackErr.RawMessage))
				return
			}

			var safeErr *isoutl.SafeError
			if errors.As(err, &safeErr) {
				fmt.Printf("Unsafe error: %s\n", safeErr.UnsafeError())
			}

			if errors.Is(err, io.EOF) || errors.Is(err, moovconnection.ErrConnectionClosed) {
				fmt.Println("Connection closed")
				// Attempt to reconnect
				if m.reconnectAttempts > 0 {
					go m.attemptReconnect()
				}
			}
		}),

		moovconnection.InboundMessageHandler(
			func(c *moovconnection.Connection, message *iso8583.Message) {
				// Handle incoming messages asynchronously
				m.handleInboundMessage(message)
			},
		),
		moovconnection.OnConnect(func(c *moovconnection.Connection) error {
			if m.debugMode {
				fmt.Printf("Connection established to %s\n", m.address)
			}
			return nil
		}),
		moovconnection.ConnectionClosedHandler(func(c *moovconnection.Connection) {
			if m.debugMode {
				fmt.Printf("Connection closed to %s\n", m.address)
			}
		}),
	}

	// Attempt to connect with retries and exponential backoff
	maxBackoff := 30 * time.Second
	baseDelay := 1 * time.Second

	for attempt := 0; attempt <= m.reconnectAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * baseDelay
			if delay > maxBackoff {
				delay = maxBackoff
			}
			if m.networkStats != nil {
				m.networkStats.RecordBackoff(delay)
			}
			if m.debugMode {
				fmt.Printf(
					"Retrying connection attempt %d/%d to %s after %v\n",
					attempt,
					m.reconnectAttempts,
					m.address,
					delay,
				)
			}
			time.Sleep(delay)
		}

		if m.networkStats != nil {
			m.networkStats.RecordReconnectAttempt()
		}

		startTime := time.Now()
		m.statusMu.Lock()
		m.Connection, err = moovconnection.New(
			m.address,
			m.spec,
			readFunc,
			writeFunc,
			options...,
		)
		if err != nil {
			m.statusMu.Unlock()
			if m.networkStats != nil {
				m.networkStats.RecordReconnectFailure()
			}
			if attempt == m.reconnectAttempts {
				return fmt.Errorf(
					"failed to create connection after %d attempts: %w",
					m.reconnectAttempts+1,
					err,
				)
			}
			continue
		}

		// Connect with timeout context to prevent hanging indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), m.totalConnectTimeout)
		err = m.Connection.ConnectCtx(ctx)
		cancel()
		if err != nil {
			m.Connection = nil // Clear failed connection
			m.statusMu.Unlock()
			if m.networkStats != nil {
				m.networkStats.RecordReconnectFailure()
			}
			if attempt == m.reconnectAttempts {
				return fmt.Errorf(
					"failed to establish connection after %d attempts: %w",
					m.reconnectAttempts+1,
					err,
				)
			}
			continue
		}

		// Success
		if m.networkStats != nil {
			duration := time.Since(startTime)
			m.networkStats.RecordReconnectSuccess(duration)
		}

		// Store connection parameters for reconnection
		m.naps = naps
		m.header = header

		// Set connection status to online
		m.Connection.SetStatus(moovconnection.StatusOnline)

		// Wait a short time to ensure the connection stays open
		// This prevents considering reconnection successful if the server immediately closes
		time.Sleep(200 * time.Millisecond)
		if m.Connection.Status() != moovconnection.StatusOnline {
			m.Connection = nil // Clear failed connection
			m.statusMu.Unlock()
			if m.networkStats != nil {
				m.networkStats.RecordReconnectFailure()
			}
			if attempt == m.reconnectAttempts {
				return fmt.Errorf("connection closed immediately after establishment")
			}
			continue
		}

		m.statusMu.Unlock()
		break
	}

	return nil
}

// GetSpec returns the current ISO8583 message specification
func (m *Manager) GetSpec() *iso8583.MessageSpec {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.spec
}

// SetSpec updates the ISO8583 message specification
func (m *Manager) SetSpec(spec *iso8583.MessageSpec) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.spec = spec
}

// Send sends an ISO8583 message with optional debug logging

func (m *Manager) IsConnected() bool {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.Connection != nil && m.Connection.Status() == moovconnection.StatusOnline
}

// GetStatus returns the connection status as a string
func (m *Manager) GetStatus() string {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	if m.Connection == nil {
		return "Not initialized"
	}
	return string(m.Connection.Status())
}

// GetAddress returns the connection address
func (m *Manager) GetAddress() string {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.address
}

// SetAddress updates the connection address
func (m *Manager) SetAddress(host, port string) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.address = fmt.Sprintf("%s:%s", host, port)
}

// Close closes the connection
func (m *Manager) Close() error {
	// First, acquire locks in consistent order to prevent deadlocks
	m.statusMu.Lock()
	m.pendingMu.Lock()

	// Clear pending requests
	for stan, req := range m.pendingRequests {
		close(req.responseChan)
		delete(m.pendingRequests, stan)
	}

	var closeErr error
	if m.Connection != nil {
		// Explicitly set status to offline before closing
		// This ensures status is updated even if ConnectionClosedHandler isn't called
		m.Connection.SetStatus(moovconnection.StatusOffline)
		closeErr = m.Connection.Close()
		m.Connection = nil
	}

	m.pendingMu.Unlock()
	m.statusMu.Unlock()

	return closeErr
}

// SetNetworkingStats sets the networking stats instance
func (m *Manager) SetNetworkingStats(stats *metrics.NetworkingStats) {
	m.networkStats = stats
}

// SetMockMatcher sets or clears the mock matcher for unsolicited incoming message processing
func (m *Manager) SetMockMatcher(matcher RouteMatcher) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.mockMatcher = matcher
}

// handleInboundMessage handles messages received from the server

func (m *Manager) SetResponseTimeout(timeout time.Duration) {
	m.responseTimeout = timeout
}

// GetResponseTimeout returns the response timeout
func (m *Manager) GetResponseTimeout() time.Duration {
	return m.responseTimeout
}

// SetMaxPendingRequests sets the maximum number of pending requests
func (m *Manager) SetMaxPendingRequests(max int) {
	if max < 1 {
		max = 1 // Minimum of 1
	}
	m.maxPendingRequests = max
}

// GetMaxPendingRequests returns the maximum number of pending requests
func (m *Manager) GetMaxPendingRequests() int {
	return m.maxPendingRequests
}

// attemptReconnect tries to reconnect in the background with exponential backoff
