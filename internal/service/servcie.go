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
	"github.com/moov-io/iso8583/network"
	isoutl "github.com/moov-io/iso8583/utils"
)

type Service struct {
	Address string

	Connection   *connection.Connection
	MessageSpec  *iso8583.MessageSpec
	Transactions []Transaction
}

type Transaction struct {
	// Define your transaction fields here
}

func NewService(host, port, specFileName string) (*Service, error) {
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
	}, nil
}

// Function to establish connection
func (s *Service) Connect(naps bool, header network.Header) error {
	if s.Connection == nil {
		err := s.establishConnection(naps, header)
		if err != nil {
			return fmt.Errorf("failed to establish connection: %w", err)
		}
	}
	if s.Connection.Status() == connection.StatusOnline {
		return nil
	}

	err := s.Connection.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	s.Connection.SetStatus(connection.StatusOnline)
	s.Address = s.Connection.Addr()
	return nil
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
			var unpackErr *iso8583.UnpackError
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

// Function to Send iso8583 message
func (s *Service) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	if s.Connection == nil || s.Connection.Status() == connection.StatusOffline {
		return nil, connection.ErrConnectionClosed
	}

	// // Send message
	b, err := msg.Pack()

	fmt.Printf("\n%v\n", hex.Dump(b))
	if err != nil {
		return nil, fmt.Errorf("failed to pack message: %w", err)
	}

	response, err := s.Connection.Send(msg)
	if err != nil {
		return nil, err
	}

	b, err = response.Pack()
	fmt.Printf("\n%v\n", hex.Dump(b))
	if err != nil {
		return nil, fmt.Errorf("failed to pack response: %w", err)
	}

	return response, nil
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
