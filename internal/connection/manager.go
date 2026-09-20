package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"

	"jiso/internal/metrics"
	"jiso/internal/utils"
)

// Manager owns one connection to a peer and the reconnect sequence behind it.
// It wraps moov-io's Connection and adds what jiso needs on top: reconnect
// attempts bounded by a total budget, per-message response and listen timeouts,
// optional TLS, and the inbound path that answers a request from a mock route.
// The exported Connection field is a seam callers read directly, so the fields
// beside it change only under statusMu.
type Manager struct {
	Connection          *moovconnection.Connection // Expose Connection as public for backward compatibility
	address             string
	spec                *iso8583.MessageSpec
	debugMode           atomic.Bool
	reconnectAttempts   int
	connectTimeout      time.Duration
	totalConnectTimeout time.Duration
	reconnecting        bool
	reconnectMu         sync.Mutex
	networkStats        *metrics.NetworkingStats
	statusMu            sync.RWMutex // Protects connection status updates
	mockMatcher         RouteMatcher
	closeGen            atomic.Uint64 // Bumped by closeUnlocked; cancels in-flight Connect publishes

	// Connection parameters for reconnection
	paramsMu  sync.RWMutex // Protects naps/header against concurrent send/reconnect reads
	naps      bool
	header    network.Header
	tlsConfig *tls.Config

	// Callback for connection changes
	cbMu               sync.RWMutex
	onConnectionChange func(conn *moovconnection.Connection)

	// Listener mode parameters
	listenMode    bool
	listenPort    string
	listener      net.Listener
	listenTimeout time.Duration

	// Visa SMC Heartbeat Daemon (active ONLY when header is VisaHeader)
	smcDaemon *SMCHeartbeatDaemon

	// Async processing fields
	pendingRequests    map[string]*pendingRequest
	pendingMu          sync.RWMutex
	maxPendingRequests atomic.Int64 // Maximum number of pending requests
	responseTimeout    atomic.Int64 // time.Duration as nanoseconds
}

// NewManager builds a manager for host:port. It does not connect: Connect and
// Listen are what touch the network, so constructing one cannot fail and callers
// can install the optional pieces -- TLS, mock routes, the debug switch -- first.
func NewManager(
	host, port string,
	spec *iso8583.MessageSpec,
	debugMode bool,
	reconnectAttempts int,
	connectTimeout, totalConnectTimeout time.Duration,
	networkStats *metrics.NetworkingStats,
) *Manager {
	m := &Manager{
		address:             fmt.Sprintf("%s:%s", host, port),
		spec:                spec,
		reconnectAttempts:   reconnectAttempts,
		connectTimeout:      connectTimeout,
		totalConnectTimeout: totalConnectTimeout,
		networkStats:        networkStats,
		pendingRequests:     make(map[string]*pendingRequest),
	}
	m.debugMode.Store(debugMode)
	m.responseTimeout.Store(int64(5 * time.Second)) // Default 5s timeout
	m.maxPendingRequests.Store(100)                 // Default max 100 pending requests
	return m
}

// Connect establishes a connection with the ISO8583 server.
// The connection attempt loop runs without holding statusMu; the resulting
// connection is published under a short lock so GetStatus/IsConnected never
// block behind connect backoffs or the stabilization sleep. A concurrent
// Close bumps closeGen and cancels the pending publish.
func (m *Manager) Connect(naps bool, header network.Header) error {
	// Store connection parameters for potential reconnection
	m.setConnParams(naps, header)

	// Always clean up any existing connection before attempting a new one
	// This prevents issues with stale connections that may appear online but are actually closed
	if m.GetConnection() != nil {
		if m.debugMode.Load() {
			outputf("Cleaning up existing connection to %s\n", m.GetAddress())
		}
		_ = m.Close()
	}
	gen := m.closeGen.Load()

	// Clone headers for reading and writing to avoid race conditions
	// Reader and Writer run in separate goroutines
	readFunc, writeFunc := m.lengthFuncs(naps, header)

	// Add connection options with proper reconnection settings
	options := m.buildConnectionOptions()

	// Attempt to connect with retries and exponential backoff
	maxBackoff := 30 * time.Second
	baseDelay := 1 * time.Second

	for attempt := 0; attempt <= m.reconnectAttempts; attempt++ {
		m.backoffBefore(attempt, baseDelay, maxBackoff)

		if m.networkStats != nil {
			m.networkStats.RecordReconnectAttempt()
		}

		startTime := time.Now()
		conn, err := moovconnection.New(
			m.GetAddress(),
			m.GetSpec(),
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
		err = conn.ConnectCtx(ctx)
		cancel()
		if err != nil {
			m.abandonFailedConnection(conn)
			if attempt == m.reconnectAttempts {
				return fmt.Errorf(
					"failed to establish connection after %d attempts: %w",
					m.reconnectAttempts+1,
					err,
				)
			}

			continue
		}

		adopted, err := m.adoptConnection(conn, naps, header, gen, startTime, attempt)
		if err != nil {
			return err
		}
		if adopted {
			break
		}
	}

	if !m.IsConnected() {
		m.notifyConnectionChange(nil)
	}

	// Enable Visa SMC Heartbeat keep-alive ONLY if Visa header format is selected
	m.applyVisaSMC(header)

	return nil
}

// SetConnectionChangeHandler registers a callback invoked on connection updates
func (m *Manager) SetConnectionChangeHandler(fn func(*moovconnection.Connection)) {
	m.cbMu.Lock()
	defer m.cbMu.Unlock()
	m.onConnectionChange = fn
}

func (m *Manager) notifyConnectionChange(conn *moovconnection.Connection) {
	m.cbMu.RLock()
	fn := m.onConnectionChange
	m.cbMu.RUnlock()
	if fn != nil {
		fn(conn)
	}
}

// GetConnection returns the active underlying moovconnection.Connection safely
func (m *Manager) GetConnection() *moovconnection.Connection {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.Connection
}

// SetTLSConfig configures the *tls.Config for secure connections
func (m *Manager) SetTLSConfig(cfg *tls.Config) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.tlsConfig = cfg
}

// GetTLSConfig returns the active *tls.Config
func (m *Manager) GetTLSConfig() *tls.Config {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.tlsConfig
}

// GetSpec returns the current ISO8583 message specification
func (m *Manager) GetSpec() *iso8583.MessageSpec {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.spec
}

// SetSpec updates the ISO8583 message specification. The live moov
// connection unpacks inbound with the spec it was BUILT with, so storing
// m.spec alone would leave that reader parsing the old wire format: every
// response would fail unpacking, and moov's readLoop skips (silently, as an
// UnpackError) messages it cannot unpack — the sender waits on its STAN and
// times out although the server matched and answered (UAT round-10 flake).
// So on an online connection the change is applied by rebuilding it, exactly
// what adoptSpecFor does when a composed message needs a different spec.
func (m *Manager) SetSpec(spec *iso8583.MessageSpec) {
	_ = m.setSpec(spec)
}

// setSpec is SetSpec with the rebuild outcome surfaced.
func (m *Manager) setSpec(spec *iso8583.MessageSpec) error {
	m.statusMu.Lock()
	m.spec = spec
	online := m.Connection != nil && m.Connection.Status() == moovconnection.StatusOnline
	m.statusMu.Unlock()

	if !online {
		return nil // next Connect builds with the new spec
	}

	naps, header := m.connParams()

	return m.Connect(naps, header)
}

// Send sends an ISO8583 message with optional debug logging

// IsConnected reports an online connection: not merely a non-nil Connection, but
// one whose status is online. A connection that has dropped keeps its object
// while it reconnects, and a caller asking this wants to know about that gap.
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

func (m *Manager) closeUnlocked() error {
	m.closeGen.Add(1) // Cancel any in-flight Connect publish
	m.listenMode = false
	if m.listener != nil {
		_ = m.listener.Close()
		m.listener = nil
	}

	if m.smcDaemon != nil {
		m.smcDaemon.Stop()
		m.smcDaemon = nil
	}

	m.pendingMu.Lock()
	for stan, req := range m.pendingRequests {
		close(req.responseChan)
		delete(m.pendingRequests, stan)
	}
	m.pendingMu.Unlock()

	var closeErr error
	if m.Connection != nil {
		m.Connection.SetStatus(moovconnection.StatusOffline)
		closeErr = m.Connection.Close()
		m.Connection = nil
	}
	m.notifyConnectionChange(nil)

	return closeErr
}

// Close closes the connection and listener if active
func (m *Manager) Close() error {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	return m.closeUnlocked()
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

// SetDebugMode switches verbose connection logging at runtime. The settings page
// edits it while a connection is live, so the value is stored atomically instead
// of behind statusMu, which the send path holds while it waits for a reply.
func (m *Manager) SetDebugMode(debug bool) {
	m.debugMode.Store(debug)
}

// SetResponseTimeout changes how long a sent message waits for its reply. Safe
// between sends: each send reads the value when it starts, so changing it does
// not disturb a message already in flight.
func (m *Manager) SetResponseTimeout(timeout time.Duration) {
	m.responseTimeout.Store(int64(timeout))
}

// GetResponseTimeout returns the response timeout
func (m *Manager) GetResponseTimeout() time.Duration {
	return m.responseTimeoutDur()
}

// SetMaxPendingRequests sets the maximum number of pending requests
func (m *Manager) SetMaxPendingRequests(maxPending int) {
	if maxPending < 1 {
		maxPending = 1 // one is the floor: zero would reject every request
	}
	m.maxPendingRequests.Store(int64(maxPending))
}

// GetMaxPendingRequests returns the maximum number of pending requests
func (m *Manager) GetMaxPendingRequests() int {
	return int(m.maxPendingRequests.Load())
}

// attemptReconnect tries to reconnect in the background with exponential backoff

// lengthFuncs builds the read/write message-length functions for a header: NAPS
// framing when requested, then the read/write counters. Reader and writer run in
// separate goroutines, so each gets its own header clone.
func (m *Manager) lengthFuncs(naps bool, header network.Header) (moovconnection.MessageLengthReader, moovconnection.MessageLengthWriter) {
	readHeader := cloneHeader(header)
	writeHeader := cloneHeader(header)

	readFunc := utils.ReadMessageLengthWrapper(readHeader)
	writeFunc := utils.WriteMessageLengthWrapper(writeHeader)
	if naps {
		readFunc = utils.NapsReadLengthWrapper(readFunc)
		writeFunc = utils.NapsWriteLengthWrapper(writeFunc)
	}
	readFunc = m.countReads(readFunc)
	writeFunc = m.countWrites(writeFunc)

	return readFunc, writeFunc
}

// backoffBefore sleeps the exponential-backoff delay before a retry attempt (a
// no-op on the first attempt) and records it in the network stats and debug log.
func (m *Manager) backoffBefore(attempt int, baseDelay, maxBackoff time.Duration) {
	if attempt == 0 {
		return
	}
	delay := time.Duration(1<<uint(attempt-1)) * baseDelay
	if delay > maxBackoff {
		delay = maxBackoff
	}
	if m.networkStats != nil {
		m.networkStats.RecordBackoff(delay)
	}
	if m.debugMode.Load() {
		_, _ = fmt.Fprintf(os.Stderr,
			"Retrying connection attempt %d/%d to %s after %v\n",
			attempt,
			m.reconnectAttempts,
			m.GetAddress(),
			delay,
		)
	}
	time.Sleep(delay)
}

// abandonFailedConnection marks a connection offline, releases its socket, and
// records the reconnect failure.
func (m *Manager) abandonFailedConnection(conn *moovconnection.Connection) {
	conn.SetStatus(moovconnection.StatusOffline)
	_ = conn.Close() // Release the socket from the failed attempt
	if m.networkStats != nil {
		m.networkStats.RecordReconnectFailure()
	}
}

// applyVisaSMC (re)starts the Visa SMC heartbeat daemon when a Visa header format
// is selected, and stops and clears it otherwise.
func (m *Manager) applyVisaSMC(header network.Header) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()

	if m.smcDaemon != nil {
		m.smcDaemon.Stop()
		m.smcDaemon = nil
	}
	if IsVisaHeader(header) {
		m.smcDaemon = NewSMCHeartbeatDaemon(m, 30*time.Second)
		m.smcDaemon.Start()
	}
}

// adoptConnection records the successful reconnect, marks the connection online,
// verifies it stayed open, and (unless a Close raced) stores it as live. It
// returns (adopted, retry, err): adopted when the connection is live, retry when
// the loop should try again, err when the loop should return.
func (m *Manager) adoptConnection(conn *moovconnection.Connection, naps bool, header network.Header, gen uint64, startTime time.Time, attempt int) (adopted bool, err error) {
	// Success
	if m.networkStats != nil {
		m.networkStats.RecordReconnectSuccess(time.Since(startTime))
	}

	// Store connection parameters for reconnection
	m.setConnParams(naps, header)

	// Set connection status to online
	conn.SetStatus(moovconnection.StatusOnline)

	// Wait a short time to ensure the connection stays open
	// This prevents considering reconnection successful if the server immediately closes
	time.Sleep(200 * time.Millisecond)
	if conn.Status() != moovconnection.StatusOnline {
		m.abandonFailedConnection(conn)
		if attempt == m.reconnectAttempts {
			return false, fmt.Errorf("connection closed immediately after establishment")
		}

		return false, nil
	}

	m.statusMu.Lock()
	if m.closeGen.Load() != gen {
		// A Close landed while we were connecting; do not resurrect it.
		m.statusMu.Unlock()
		conn.SetStatus(moovconnection.StatusOffline)
		_ = conn.Close()

		return false, fmt.Errorf("connection to %s cancelled: closed during connect", m.GetAddress())
	}
	m.Connection = conn
	m.statusMu.Unlock()
	m.notifyConnectionChange(conn)

	return true, nil
}
