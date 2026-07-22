package server

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
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
	stats      *ServerStats
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
		stats:      NewServerStats(),
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
func (s *Server) GetStats() *ServerStats {
	return s.stats
}

// Start launches the TCP listener on the specified port
func (s *Server) Start(port string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running on port %s", s.port)
	}

	addr := fmt.Sprintf(":%s", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
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
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Unlock()

	// Close all active connections
	s.connsMu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.connsMu.Unlock()

	return nil
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

// ActiveConnections returns the number of connected TCP clients
func (s *Server) ActiveConnections() int {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	return len(s.conns)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				continue
			}
		}

		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		conn.Close()
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

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		// Read TCP header length
		_, err := header.ReadFrom(conn)
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "closed") {
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
			continue
		}

		mti, _ := req.GetMTI()

		// Match and compose response
		matchedRoute, resp, err := s.matcher.MatchAndCompose(req, spec)
		if err != nil || resp == nil {
			continue
		}

		routeName := "Catch-all Fallback"
		if matchedRoute != nil {
			routeName = matchedRoute.Name
			if matchedRoute.DropConnection {
				return
			}
		}

		respCode := ""
		if f39 := resp.GetField(39); f39 != nil {
			respCode, _ = f39.String()
		}

		// Record served message statistics
		s.stats.RecordMessage(mti, routeName, respCode)

		// Pack response
		respPacked, err := resp.Pack()
		if err != nil {
			continue
		}

		// Send response with TCP header
		respHeader, err := utils.SelectServerHeader(hType)
		if err != nil {
			continue
		}
		respHeader.SetLength(len(respPacked))

		if _, err := respHeader.WriteTo(conn); err != nil {
			return
		}
		if _, err := conn.Write(respPacked); err != nil {
			return
		}
	}
}
