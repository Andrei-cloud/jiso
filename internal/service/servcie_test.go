package service

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/network"
)

func createTempSpecFile(t *testing.T) string {
	spec := `{
		"name": "Test Spec",
		"fields": {
			"0": {
				"type": "String",
				"length": 4,
				"description": "Message Type Indicator",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"1": {
				"type": "Bitmap",
				"length": 8,
				"description": "Bitmap",
				"enc": "Binary",
				"prefix": "Hex.Fixed"
			},
			"11": {
				"type": "String",
				"length": 6,
				"description": "Systems Trace Audit Number (STAN)",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"39": {
				"type": "String",
				"length": 2,
				"description": "Response Code",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"70": {
				"type": "Numeric",
				"length": 3,
				"description": "Network Management Information Code",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed",
				"padding": {
					"type": "Left",
					"pad": "0"
				}
			}
		}
	}`

	file, err := os.CreateTemp("", "spec.json")
	if err != nil {
		t.Fatalf("Failed to create temp spec file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(spec)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	return file.Name()
}

func TestNewService(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.Address != "localhost:8080" {
		t.Errorf("Expected address 'localhost:8080', got '%s'", service.Address)
	}

	if service.MessageSpec == nil {
		t.Error("MessageSpec is nil")
	}

	if service.connManager == nil {
		t.Error("connManager is nil")
	}

	if service.networkStats == nil {
		t.Error("networkStats is nil")
	}

	if service.debugMode != false {
		t.Error("debugMode should be false")
	}
}

func TestServiceGetters(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		true,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if !service.debugMode {
		t.Error("debugMode should be true")
	}

	if service.GetSpec() != service.MessageSpec {
		t.Error("GetSpec should return MessageSpec")
	}

	if service.GetNetworkingStats() != service.networkStats {
		t.Error("GetNetworkingStats should return networkStats")
	}
}

func TestServiceIsConnected(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should not be connected initially
	if service.IsConnected() {
		t.Error("Service should not be connected initially")
	}
}

func TestServiceClose(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Close should not error even if not connected
	err = service.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestServiceDisconnect(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Disconnect should not error even if not connected
	err = service.Disconnect()
	if err != nil {
		t.Errorf("Disconnect failed: %v", err)
	}
}

type testServer struct {
	listener net.Listener
	spec     *iso8583.MessageSpec
	header   network.Header
	respond  bool
	done     chan struct{}
}

func startTestServer(spec *iso8583.MessageSpec, respond bool) (*testServer, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	server := &testServer{
		listener: listener,
		spec:     spec,
		header:   &Binary2BytesAdapter{network.NewBinary2BytesHeader()},
		respond:  respond,
		done:     make(chan struct{}),
	}

	go server.run()

	return server, nil
}

func (s *testServer) run() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}

		go s.handle(conn)
	}
}

func (s *testServer) handle(conn net.Conn) {
	defer conn.Close()

	// Read length
	_, err := s.header.ReadFrom(conn)
	if err != nil {
		return
	}

	messageLength := s.header.Length()

	// Read message
	buf := make([]byte, messageLength)
	_, err = conn.Read(buf)
	if err != nil {
		return
	}

	if !s.respond {
		// For timeout test, don't respond
		return
	}

	// Unpack
	msg := iso8583.NewMessage(s.spec)
	err = msg.Unpack(buf)
	if err != nil {
		return
	}

	// Create response
	resp := iso8583.NewMessage(s.spec)
	resp.MTI("0810")
	if stan, err := msg.GetString(11); err == nil {
		resp.Field(11, stan)
	}
	resp.Field(39, "00")

	// Pack response
	respPacked, err := resp.Pack()
	if err != nil {
		return
	}

	// Write length
	s.header.SetLength(len(respPacked))
	_, err = s.header.WriteTo(conn)
	if err != nil {
		return
	}

	// Write response
	_, err = conn.Write(respPacked)
	if err != nil {
		return
	}
}

func (s *testServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *testServer) Close() {
	close(s.done)
	s.listener.Close()
}

type Binary2BytesAdapter struct {
	binary2Bytes *network.Binary2Bytes
}

func (a *Binary2BytesAdapter) SetLength(length int) {
	a.binary2Bytes.SetLength(length)
}

func (a *Binary2BytesAdapter) Length() int {
	return a.binary2Bytes.Length()
}

func (a *Binary2BytesAdapter) WriteTo(w io.Writer) (int, error) {
	n, err := a.binary2Bytes.WriteTo(w)
	return n, err
}

func (a *Binary2BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	n, err := a.binary2Bytes.ReadFrom(r)
	if err != nil {
		return 0, fmt.Errorf("reading from reader: %w", err)
	}

	return n, nil
}
