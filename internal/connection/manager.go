package connection

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	iso8583errors "github.com/moov-io/iso8583/errors"
	"github.com/moov-io/iso8583/network"
	isoutl "github.com/moov-io/iso8583/utils"
)

// Manager handles connections to ISO8583 servers
type Manager struct {
	Connection *moovconnection.Connection // Expose Connection as public for backward compatibility
	address    string
	spec       *iso8583.MessageSpec
	debugMode  bool
}

// NewManager creates a new connection manager
func NewManager(host, port string, spec *iso8583.MessageSpec, debugMode bool) *Manager {
	return &Manager{
		address:   fmt.Sprintf("%s:%s", host, port),
		spec:      spec,
		debugMode: debugMode,
	}
}

// Connect establishes a connection with the ISO8583 server
func (m *Manager) Connect(naps bool, header network.Header) error {
	if m.Connection != nil && m.Connection.Status() == moovconnection.StatusOnline {
		return fmt.Errorf("connection is already established")
	}

	var err error
	readFunc := utils.ReadMessageLengthWrapper(header)
	writeFunc := utils.WriteMessageLengthWrapper(header)
	if naps {
		readFunc = utils.NapsReadLengthWrapper(readFunc)
		writeFunc = utils.NapsWriteLengthWrapper(writeFunc)
	}

	m.Connection, err = moovconnection.New(
		m.address,
		m.spec,
		readFunc,
		writeFunc,
		moovconnection.ErrorHandler(func(err error) {
			fmt.Printf("Error encountered while processing transaction request: %s\n", err)
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
			if errors.Is(err, io.EOF) {
				fmt.Println("Connection closed")
				m.Close()
			}
		}),
		moovconnection.ConnectTimeout(4*time.Second),
		moovconnection.ConnectionEstablishedHandler(func(c *moovconnection.Connection) {
			c.SetStatus(moovconnection.StatusOnline)
			if m.debugMode {
				fmt.Printf("Connection established to %s\n", m.address)
			}
		}),
		moovconnection.ConnectionClosedHandler(func(c *moovconnection.Connection) {
			c.SetStatus(moovconnection.StatusOffline)
			if m.debugMode {
				fmt.Printf("Connection closed to %s\n", m.address)
			}
		}),
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
			if errors.Is(err, io.EOF) {
				fmt.Println("Connection closed")
				m.Close()
			}
			if errors.Is(err, moovconnection.ErrConnectionClosed) {
				fmt.Println("Connection closed")
				m.Close()
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", m.address, err)
	}

	m.Connection.ConnectCtx(context.Background())

	// Verify the connection
	if err := m.VerifyConnection(); err != nil {
		return fmt.Errorf("failed to verify connection: %w", err)
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

// VerifyConnection sends a test echo message to confirm connection is fully established
func (m *Manager) VerifyConnection() error {
	if m.Connection == nil {
		return fmt.Errorf("connection not initialized")
	}

	if m.Connection.Status() != moovconnection.StatusOnline {
		return fmt.Errorf("connection is offline")
	}

	// Create a simple ping message (can be network management message or echo)
	msg := iso8583.NewMessage(m.spec)

	// Network management echo message
	err := msg.Field(0, "0800")
	if err != nil {
		return fmt.Errorf("failed to create verification message: %w", err)
	}

	// Try to set common fields for a ping/echo message
	// Note: These might need adjustment based on your ISO8583 specification
	fields := map[int]string{
		7:  time.Now().UTC().Format("0102150405"),          // Date and time in MMDDhhmmss format
		11: fmt.Sprintf("%06d", time.Now().Unix()%1000000), // Trace number
	}

	for field, value := range fields {
		if err := msg.Field(field, value); err != nil {
			// Skip fields that might not be supported in this specification
			if m.debugMode {
				fmt.Printf("Warning: failed to set field %d: %v\n", field, err)
			}
		}
	}

	// Set network management code if field 70 is in the spec
	if m.spec.Fields[70] != nil {
		if err := msg.Field(70, "301"); err != nil && m.debugMode {
			fmt.Printf("Warning: failed to set field 70: %v\n", err)
		}
	}

	// Send the message and see if we get a response
	if m.debugMode {
		fmt.Println("Sending verification message...")
	}

	// Set a shorter timeout for verification
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	responseCh := make(chan *iso8583.Message, 1)
	errCh := make(chan error, 1)

	go func() {
		resp, err := m.Connection.Send(msg)
		if err != nil {
			errCh <- err
			return
		}
		responseCh <- resp
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("connection verification timed out")
	case err := <-errCh:
		return fmt.Errorf("connection verification failed: %w", err)
	case resp := <-responseCh:
		if m.debugMode {
			fmt.Println("Verification successful, received response:", resp)
		}
		return nil
	}
}
