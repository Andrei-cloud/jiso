package service

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
	iso8583errors "github.com/moov-io/iso8583/errors"
	"github.com/moov-io/iso8583/network"
	isoutl "github.com/moov-io/iso8583/utils"
)

type Service struct {
	Address string

	Connection   *connection.Connection
	MessageSpec  *iso8583.MessageSpec
	Transactions []Transaction
	debugMode    bool
}

type Transaction struct {
	// Define your transaction fields here
}

func NewService(host, port, specFileName string, debugMode bool) (*Service, error) {
	// Load message spec
	spec, err := utils.CreateSpecFromFile(specFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec file: %w", err)
	}
	fmt.Printf("Spec file loaded successfully, current spec: %s\n", spec.Name)

	return &Service{
		MessageSpec:  spec,
		Address:      host + ":" + port,
		Transactions: make([]Transaction, 0),
		debugMode:    debugMode,
	}, nil
}

// Function to establish connection
func (s *Service) Connect(naps bool, header network.Header) error {
	// Try to establish connection if needed
	if s.Connection == nil {
		err := s.establishConnection(naps, header)
		if err != nil {
			return fmt.Errorf("failed to establish connection: %w", err)
		}
	}

	// If already connected, return immediately
	if s.Connection.Status() == connection.StatusOnline {
		return nil
	}

	// Connection exists but is offline, try to reconnect with backoff
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := s.Connection.Connect()
		if err == nil {
			s.Connection.SetStatus(connection.StatusOnline)
			s.Address = s.Connection.Addr()
			return nil
		}

		if attempt < maxRetries {
			backoffTime := time.Duration(attempt) * time.Second
			fmt.Printf("Connection attempt %d failed, retrying in %v...\n",
				attempt, backoffTime)
			time.Sleep(backoffTime)
		} else {
			return fmt.Errorf("failed to connect after %d attempts: %w",
				maxRetries, err)
		}
	}

	return fmt.Errorf("unexpected error during connection")
}

func (s *Service) establishConnection(naps bool, header network.Header) error {
	var err error
	readFunc := utils.ReadMessageLengthWrapper(header)
	writeFunc := utils.WriteMessageLengthWrapper(header)
	if naps {
		readFunc = utils.NapsReadLengthWrapper(readFunc)
		writeFunc = utils.NapsWriteLengthWrapper(writeFunc)
	}

	s.Connection, err = connection.New(
		s.Address,
		s.MessageSpec,
		readFunc,
		writeFunc,
		connection.ErrorHandler(func(err error) {
			fmt.Printf("Error encountered wile processing transaction request: %s\n", err)
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
				s.Disconnect()
			}
		}),
		connection.ConnectTimeout(4*time.Second),
	)
	return err
}

// Function to disconnect
func (s *Service) Disconnect() error {
	if s.Connection == nil {
		return nil
	}
	err := s.Connection.Close()
	if err != nil {
		return fmt.Errorf("failed to close connection: %w", err)
	}

	s.Connection.SetStatus(connection.StatusOffline)
	s.Connection = nil
	return nil
}

// Connection status
func (s *Service) IsConnected() bool {
	if s.Connection == nil {
		return false
	}
	return s.Connection.Status() == connection.StatusOnline
}

// Function to return current specification
func (s *Service) GetSpec() *iso8583.MessageSpec {
	return s.MessageSpec
}

// Function to Send iso8583 message with improved debug handling
func (s *Service) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	if s.Connection == nil || s.Connection.Status() == connection.StatusOffline {
		return nil, connection.ErrConnectionClosed
	}

	// Debug logging only when debugMode is enabled
	if s.debugMode {
		// Cache the packed message for both logging and sending
		packedMsg, err := msg.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack message: %w", err)
		}
		fmt.Printf("\nSENDING MESSAGE:\n%v\n", hex.Dump(packedMsg))

		// Send message using the already packed data
		response, err := s.Connection.Send(msg)
		if err != nil {
			return nil, err
		}

		// Debug logging for response
		packedResponse, err := response.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack response: %w", err)
		}
		fmt.Printf("\nRECEIVED RESPONSE:\n%v\n", hex.Dump(packedResponse))

		return response, nil
	}

	// Normal operation without extra packing
	return s.Connection.Send(msg)
}

func (s *Service) BackgroundSend(msg *iso8583.Message) (*iso8583.Message, error) {
	if s.Connection == nil || s.Connection.Status() == connection.StatusOffline {
		return nil, connection.ErrConnectionClosed
	}

	response, err := s.Connection.Send(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	return response, nil
}

// Function to close connection
func (s *Service) Close() error {
	if s.Connection == nil {
		return nil
	}
	if s.Connection.Status() == connection.StatusOffline {
		return nil
	}
	fmt.Println("Closing connection")
	return s.Connection.Close()
}
