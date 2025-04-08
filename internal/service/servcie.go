package service

import (
	"fmt"

	"jiso/internal/connection"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"
)

type Service struct {
	Address     string
	Connection  *moovconnection.Connection
	MessageSpec *iso8583.MessageSpec
	connManager *connection.Manager
	debugMode   bool
}

func NewService(host, port, specFileName string, debugMode bool) (*Service, error) {
	// Load message spec
	spec, err := utils.CreateSpecFromFile(specFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec file: %w", err)
	}
	fmt.Printf("Spec file loaded successfully, current spec: %s\n", spec.Name)

	// Create a new connection manager
	connManager := connection.NewManager(host, port, spec, debugMode)

	return &Service{
		MessageSpec: spec,
		Address:     fmt.Sprintf("%s:%s", host, port),
		connManager: connManager,
		debugMode:   debugMode,
	}, nil
}

// Connect establishes a connection to the server
func (s *Service) Connect(naps bool, header network.Header) error {
	err := s.connManager.Connect(naps, header)
	if err != nil {
		return err
	}

	// Maintain backward compatibility with existing code
	// by exposing the Connection field
	s.Connection = s.connManager.Connection

	// Give the connection a moment to stabilize
	// This prevents false "connected" status before the connection is truly ready
	if s.debugMode {
		fmt.Println("Waiting for connection to stabilize...")
	}

	// The connection should be ready now, but let's explicitly verify
	if !s.IsConnected() {
		return fmt.Errorf("connection established but not ready")
	}

	return nil
}

// Disconnect closes the connection to the server
func (s *Service) Disconnect() error {
	err := s.connManager.Close()
	if err != nil {
		return fmt.Errorf("failed to close connection: %w", err)
	}

	s.Connection = nil
	return nil
}

// IsConnected returns whether the service is connected
func (s *Service) IsConnected() bool {
	return s.connManager.IsConnected()
}

// GetSpec returns the current ISO8583 message specification
func (s *Service) GetSpec() *iso8583.MessageSpec {
	return s.MessageSpec
}

// Send sends an ISO8583 message and returns the response
func (s *Service) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	return s.connManager.Send(msg)
}

// BackgroundSend sends an ISO8583 message without debug logging
func (s *Service) BackgroundSend(msg *iso8583.Message) (*iso8583.Message, error) {
	return s.connManager.BackgroundSend(msg)
}

// Close closes the connection when service is shut down
func (s *Service) Close() error {
	if s.connManager == nil {
		return nil
	}
	return s.connManager.Close()
}
