package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// Server represents an embedded ISO8583 Mock Server
type Server struct {
	mu         sync.Mutex
	listener   net.Listener
	spec       *iso8583.MessageSpec
	routes     []config.MockRouteConfig
	matcher    *Matcher
	headerType string
	running    bool
	port       string
	stopChan   chan struct{}
	conns      map[net.Conn]struct{}
	connsMu    sync.Mutex
	stats      *Stats
	tlsConfig  *tls.Config
}

// NewServer creates a new Server instance
func NewServer(spec *iso8583.MessageSpec, routes []config.MockRouteConfig, headerType string) *Server {
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}
	if headerType == "" {
		headerType = "binary2"
	}
	return &Server{
		spec:       spec,
		routes:     routes,
		matcher:    NewMatcher(routes),
		headerType: headerType,
		conns:      make(map[net.Conn]struct{}),
		stopChan:   make(chan struct{}),
		stats:      NewStats(),
	}
}

// SetHeaderType updates the TCP header format type for the server
func (s *Server) SetHeaderType(headerType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if headerType != "" {
		s.headerType = headerType
	}
}

// GetHeaderType returns the active TCP header format type
func (s *Server) GetHeaderType() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headerType
}

// GetStats returns the server statistics tracker
func (s *Server) GetStats() *Stats {
	return s.stats
}

// SetTLSConfig configures the *tls.Config for mTLS mock server operation
func (s *Server) SetTLSConfig(cfg *tls.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsConfig = cfg
}

// GetTLSConfig returns the active *tls.Config for the mock server
func (s *Server) GetTLSConfig() *tls.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tlsConfig
}

// Start launches the TCP listener (or TLS listener if configured) on the specified port
func (s *Server) Start(port string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running on port %s", s.port)
	}

	addr := fmt.Sprintf(":%s", port)

	// The address is ":port" -- a numeric wildcard bind, so there is no name to
	// resolve and no peer to wait for, which is what a context on Listen would
	// cancel. Every caller (the CLI command, the TUI, the app layer) is itself
	// context-free at this point, so threading one would mean changing a public
	// signature to carry something with nothing in it.
	//nolint:noctx // nothing between here and the bind can block on resolution
	l, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if s.tlsConfig != nil {
		tlsCfg := s.tlsConfig.Clone()
		if tlsCfg.ClientCAs != nil && tlsCfg.ClientAuth == tls.NoClientCert {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
		l = tls.NewListener(l, tlsCfg)
	}

	// Record the port actually bound rather than the one requested: "0" means "any
	// free port", and a caller that asked for 0 cannot connect without learning
	// which one it got. For every explicit port the two are identical, so nothing
	// that passes a real port number can see the difference.
	if port == "0" {
		if ta, ok := l.Addr().(*net.TCPAddr); ok {
			port = strconv.Itoa(ta.Port)
		}
	}

	s.listener = l
	s.port = port
	s.running = true
	s.stopChan = make(chan struct{})
	s.stats.Reset()
	s.mu.Unlock()

	go s.acceptLoop()
	return nil
}

// Stop terminates the TCP listener and closes all active client connections
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stopChan)
	var listenerErr error
	if s.listener != nil {
		listenerErr = s.listener.Close()
	}
	s.mu.Unlock()

	// Close all active connections. Errors are ignored on purpose: these
	// conns are concurrently owned by handleConn, whose own defer closes
	// them too, so "use of closed network connection" here is expected
	// and not actionable.
	s.connsMu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.connsMu.Unlock()

	return listenerErr
}

// IsRunning returns whether the mock server is active
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetPort returns the port on which the server is listening
func (s *Server) GetPort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// BoundPort returns the TCP port the listener actually bound to, resolving
// an ephemeral "0" start for in-process callers (PAR-301 golden harness).
func (s *Server) BoundPort() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.listener == nil {
		return "", fmt.Errorf("server is not running")
	}

	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return "", fmt.Errorf("resolving listener port: %w", err)
	}

	return port, nil
}

// ActiveConnections returns the number of connected TCP clients
func (s *Server) ActiveConnections() int {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	return len(s.conns)
}

func (s *Server) acceptLoop() {
	const (
		baseAcceptBackoff = 1 * time.Millisecond
		maxAcceptBackoff  = 100 * time.Millisecond
	)
	backoff := baseAcceptBackoff

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				// Listener was closed by a path other than Stop; Accept can
				// never succeed again, so exit instead of spinning.
				return
			}
			// Transient accept failure (e.g. EMFILE/ECONNABORTED): back off
			// rather than busy-spinning at full CPU.
			time.Sleep(backoff)
			if backoff < maxAcceptBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = baseAcceptBackoff

		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		_ = conn.Close() // cleanup path: Stop or a prior handler may already have closed it
		s.connsMu.Lock()
		delete(s.conns, conn)
		s.connsMu.Unlock()
	}()

	s.mu.Lock()
	hType := s.headerType
	spec := s.spec
	s.mu.Unlock()

	header, err := utils.SelectServerHeader(hType)
	if err != nil {
		return
	}

	var writeMu sync.Mutex

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		// Read TCP header length
		_, err := header.ReadFrom(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "closed") {
				return
			}
			return
		}

		payloadLen := header.Length()
		if payloadLen <= 0 || payloadLen > 65535 {
			return
		}

		payload := make([]byte, payloadLen)
		_, err = io.ReadFull(conn, payload)
		if err != nil {
			return
		}

		// Unpack request message
		req := iso8583.NewMessage(spec)
		if err := req.Unpack(payload); err != nil {
			s.stats.RecordRequestError()
			outputf("\n[SERVER] ❌ Error unpacking request payload: %v\n", err)
			continue
		}

		go s.serveRequest(conn, req, spec, hType, &writeMu)
	}
}

// serveRequest matches one request to a mock route, composes and writes the
// response, and records the served-message statistics. writeMu serializes writes
// to the connection across the concurrent per-request goroutines.
func (s *Server) serveRequest(conn net.Conn, req *iso8583.Message, spec *iso8583.MessageSpec, hType string, writeMu *sync.Mutex) {
	mti, _ := req.GetMTI()

	// Match and compose response (simulated latency/jitter sleep happens asynchronously)
	matchedRoute, resp, err := s.matcher.MatchAndCompose(req, spec)
	if err != nil || resp == nil {
		outputf("\n[SERVER] ❌ Error matching/composing response for MTI %s: %v\n", mti, err)
		return
	}

	routeName := FallbackRouteName
	if matchedRoute != nil {
		routeName = matchedRoute.Name
		if matchedRoute.DropConnection {
			s.stats.RecordDrop()
			outputf("\n[SERVER] 🔴 Matched Route '%s' for MTI %s -> Dropping connection\n", routeName, mti)
			// Dropping is the user-configured action: if the close
			// itself fails the drop did not happen, so surface it.
			if err := conn.Close(); err != nil {
				outputf("\n[SERVER] ⚠️ Error dropping connection for MTI %s: %v\n", mti, err)
			}

			return
		}
	}

	respCode := ""
	if f39 := resp.GetField(39); f39 != nil {
		respCode, _ = f39.String()
	}
	respMTI, _ := resp.GetMTI()

	if matchedRoute != nil {
		outputf("\n[SERVER] 🟢 Matched Route '%s' for MTI %s -> Responding %s (RC: %s)\n", routeName, mti, respMTI, respCode)
	} else {
		outputf("\n[SERVER] ⚠️ Fallback (No Route Match) for MTI %s -> Responding %s (RC: 12)\n", mti, respMTI)
	}

	// Record served message statistics
	s.stats.RecordMessage(mti, routeName, respCode)

	// Pack response
	respPacked, err := resp.Pack()
	if err != nil {
		outputf("[SERVER] ❌ Error packing response for route '%s': %v\n", routeName, err)
		return
	}

	// Send response with TCP header
	respHeader, err := utils.SelectServerHeader(hType)
	if err != nil {
		return
	}
	respHeader.SetLength(len(respPacked))

	writeMu.Lock()
	defer writeMu.Unlock()
	if _, err := respHeader.WriteTo(conn); err != nil {
		return
	}
	if _, err := conn.Write(respPacked); err != nil {
		return
	}
}
