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

// Manager handles connections to ISO8583 servers
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

	// Connection parameters for reconnection
	naps   bool
	header network.Header

	// Async processing fields
	pendingRequests      map[string]*pendingRequest
	pendingMu            sync.RWMutex
	responseTimeout      time.Duration
	responseReaderCtx    context.Context
	responseReaderCancel context.CancelFunc
}

type pendingRequest struct {
	responseChan    chan *iso8583.Message
	timeout         time.Time
	transactionName string
}

// NewManager creates a new connection manager
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
	readFunc := utils.ReadMessageLengthWrapper(header)
	writeFunc := utils.WriteMessageLengthWrapper(header)
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
			c.SetStatus(moovconnection.StatusOnline)
			if m.debugMode {
				fmt.Printf("Connection established to %s\n", m.address)
			}
			return nil
		}),
		moovconnection.ConnectionClosedHandler(func(c *moovconnection.Connection) {
			c.SetStatus(moovconnection.StatusOffline)
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
		m.Connection, err = moovconnection.New(
			m.address,
			m.spec,
			readFunc,
			writeFunc,
			options...,
		)
		if err != nil {
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
		break
	}

	return nil
}

// Send sends an ISO8583 message with optional debug logging
func (m *Manager) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	// Connection validation and error handling
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	// Debug logging
	if m.debugMode {
		// Log the request
		packedMsg, err := msg.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack message: %w", err)
		}
		fmt.Printf("\nSENDING MESSAGE:\n%v\n", hex.Dump(packedMsg))

		// Send and get response
		response, err := m.Connection.Send(msg)
		if err != nil {
			return nil, err
		}

		// Log the response
		packedResponse, err := response.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack response: %w", err)
		}
		fmt.Printf("\nRECEIVED RESPONSE:\n%v\n", hex.Dump(packedResponse))

		return response, nil
	}

	// Regular operation without debug
	return m.Connection.Send(msg)
}

// BackgroundSend sends a message without debug logging (for background operations)
func (m *Manager) BackgroundSend(msg *iso8583.Message) (*iso8583.Message, error) {
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	return m.Connection.Send(msg)
}

// IsConnected returns the connection status
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
	return m.address
}

// Close closes the connection
func (m *Manager) Close() error {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()

	// Clear pending requests
	m.pendingMu.Lock()
	for stan, req := range m.pendingRequests {
		close(req.responseChan)
		delete(m.pendingRequests, stan)
	}
	m.pendingMu.Unlock()

	if m.Connection != nil {
		// Explicitly set status to offline before closing
		// This ensures status is updated even if ConnectionClosedHandler isn't called
		m.Connection.SetStatus(moovconnection.StatusOffline)
		return m.Connection.Close()
	}
	return nil
}

// SetNetworkingStats sets the networking stats instance
func (m *Manager) SetNetworkingStats(stats *metrics.NetworkingStats) {
	m.networkStats = stats
}

// handleInboundMessage handles messages received from the server
func (m *Manager) handleInboundMessage(message *iso8583.Message) {
	// Get STAN from response
	stanField := message.GetField(11)
	if stanField == nil {
		if m.debugMode {
			fmt.Printf("Inbound message missing STAN field\n")
		}
		return
	}
	stan, err := stanField.String()
	if err != nil {
		if m.debugMode {
			fmt.Printf("Error getting STAN from inbound message: %v\n", err)
		}
		return
	}

	// Find pending request
	m.pendingMu.Lock()
	pending, exists := m.pendingRequests[stan]
	if exists {
		delete(m.pendingRequests, stan)
	}
	m.pendingMu.Unlock()

	if exists {
		// Send response to waiting goroutine
		select {
		case pending.responseChan <- message:
		case <-time.After(100 * time.Millisecond):
			if m.debugMode {
				fmt.Printf("Timeout sending inbound message to channel for STAN %s\n", stan)
			}
		}
	} else {
		// Unmatched response
		if m.debugMode {
			fmt.Printf("Unmatched inbound message received for STAN %s\n", stan)
		}
		// Could log to metrics or file
	}
}

// SendAsync sends a message asynchronously and returns a channel for the response
func (m *Manager) SendAsync(
	msg *iso8583.Message,
	transactionName string,
) (<-chan *iso8583.Message, error) {
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	// Get STAN from request
	stanField := msg.GetField(11)
	if stanField == nil {
		return nil, fmt.Errorf("request missing STAN field")
	}
	stan, err := stanField.String()
	if err != nil {
		return nil, fmt.Errorf("failed to get STAN from request: %w", err)
	}

	// Send the message without waiting for response
	err = conn.Reply(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Create pending request
	responseChan := make(chan *iso8583.Message, 1)
	pending := &pendingRequest{
		responseChan:    responseChan,
		timeout:         time.Now().Add(m.responseTimeout),
		transactionName: transactionName,
	}

	// Add to pending
	m.pendingMu.Lock()
	m.pendingRequests[stan] = pending
	m.pendingMu.Unlock()

	// Start timeout handler
	go func() {
		time.Sleep(m.responseTimeout)
		m.pendingMu.Lock()
		if _, exists := m.pendingRequests[stan]; exists {
			delete(m.pendingRequests, stan)
			close(responseChan)
			if m.debugMode {
				fmt.Printf("Request timeout for STAN %s, transaction %s\n", stan, transactionName)
			}
		}
		m.pendingMu.Unlock()
	}()

	return responseChan, nil
}

// SetResponseTimeout sets the timeout for waiting responses
func (m *Manager) SetResponseTimeout(timeout time.Duration) {
	m.responseTimeout = timeout
}

// GetResponseTimeout returns the response timeout
func (m *Manager) GetResponseTimeout() time.Duration {
	return m.responseTimeout
}

// attemptReconnect tries to reconnect in the background with exponential backoff
func (m *Manager) attemptReconnect() {
	m.reconnectMu.Lock()
	if m.reconnecting {
		m.reconnectMu.Unlock()
		return // Already reconnecting
	}
	m.reconnecting = true
	m.reconnectMu.Unlock()

	defer func() {
		m.reconnectMu.Lock()
		m.reconnecting = false
		m.reconnectMu.Unlock()
	}()

	maxBackoff := 30 * time.Second
	baseDelay := 1 * time.Second

	for attempt := 1; attempt <= m.reconnectAttempts; attempt++ {
		delay := time.Duration(1<<uint(attempt-1)) * baseDelay
		if delay > maxBackoff {
			delay = maxBackoff
		}

		if m.networkStats != nil {
			m.networkStats.RecordBackoff(delay)
		}

		if m.debugMode {
			fmt.Printf(
				"Waiting %v before reconnection attempt %d/%d\n",
				delay,
				attempt,
				m.reconnectAttempts,
			)
		}
		time.Sleep(delay)

		if m.networkStats != nil {
			m.networkStats.RecordReconnectAttempt()
		}

		startTime := time.Now()
		err := m.Connect(m.naps, m.header)
		if err == nil {
			if m.networkStats != nil {
				duration := time.Since(startTime)
				m.networkStats.RecordReconnectSuccess(duration)
			}
			if m.debugMode {
				fmt.Printf("Reconnection successful on attempt %d\n", attempt)
			}
			return
		}

		if m.networkStats != nil {
			m.networkStats.RecordReconnectFailure()
		}

		if m.debugMode {
			fmt.Printf("Reconnection attempt %d failed: %s\n", attempt, err)
		}
	}

	if m.debugMode {
		fmt.Printf("All reconnection attempts failed\n")
	}
}
