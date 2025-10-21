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
	}
}

// Connect establishes a connection with the ISO8583 server
func (m *Manager) Connect(naps bool, header network.Header) error {
	// If we already have a connection object, check its status.
	// If it's online, return "already established" error.
	// Otherwise, close any stale connection first
	if m.Connection != nil {
		if m.Connection.Status() == moovconnection.StatusOnline {
			return fmt.Errorf("connection is already established")
		} else {
			// We have a connection object but it's not online
			// Close it cleanly before attempting a new connection
			if m.debugMode {
				fmt.Printf("Cleaning up stale connection to %s\n", m.address)
			}
			m.Close()
			m.Connection = nil
		}
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
					go m.attemptReconnect(naps, header)
				}
			}
		}),
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
	if m.Connection == nil || m.Connection.Status() == moovconnection.StatusOffline {
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
	if m.Connection == nil || m.Connection.Status() == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	return m.Connection.Send(msg)
}

// IsConnected returns the connection status
func (m *Manager) IsConnected() bool {
	return m.Connection != nil && m.Connection.Status() == moovconnection.StatusOnline
}

// GetStatus returns the connection status as a string
func (m *Manager) GetStatus() string {
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
	if m.Connection != nil {
		return m.Connection.Close()
	}
	return nil
}

// SetNetworkingStats sets the networking stats instance
func (m *Manager) SetNetworkingStats(stats *metrics.NetworkingStats) {
	m.networkStats = stats
}

// attemptReconnect tries to reconnect in the background with exponential backoff
func (m *Manager) attemptReconnect(naps bool, header network.Header) {
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
		err := m.Connect(naps, header)
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
