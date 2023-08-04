package service

import (
	"common/utils"
	"encoding/hex"
	"fmt"

	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
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
		return nil, err
	}
	fmt.Printf("Spec file loaded successfully, current spec: %s\n", spec.Name)

	return &Service{
		MessageSpec:  spec,
		Address:      host + ":" + port,
		Transactions: make([]Transaction, 0),
	}, nil
}

// Function to establish connection
func (s *Service) Connect() error {
	if s.Connection == nil {
		var err error
		s.Connection, err = connection.New(s.Address, s.MessageSpec, utils.ReadMessageLength, utils.WriteMessageLength)
		if err != nil {
			return err
		}
	}
	if s.Connection.Status() == connection.StatusOnline {
		return nil
	}

	err := s.Connection.Connect()
	if err != nil {
		return err
	}
	fmt.Printf("Connected to %s\n", s.Connection.Addr())

	s.Connection.SetStatus(connection.StatusOnline)
	s.Address = s.Connection.Addr()
	return nil
}

// Function to disconnect
func (s *Service) Disconnect() error {
	if s.Connection == nil {
		return nil
	}
	err := s.Connection.Close()
	if err != nil {
		return err
	}
	s.Connection.SetStatus(connection.StatusOffline)
	s.Connection = nil
	return nil
}

// Connection status
func (s *Service) IsConnected() bool {
	return s.Connection.Status() == connection.StatusOnline
}

// Function to return current specification
func (s *Service) GetSpec() *iso8583.MessageSpec {
	return s.MessageSpec
}

// Function to Send iso8583 message
func (s *Service) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	if s.Connection == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	if s.Connection.Status() == connection.StatusOffline {
		return nil, fmt.Errorf("connection is offline")
	}

	// // Send message
	b, err := msg.Pack()

	fmt.Printf("\n%v\n", hex.Dump(b))
	if err != nil {
		return nil, err
	}

	response, err := s.Connection.Send(msg)
	if err != nil {
		return nil, err
	}

	b, err = response.Pack()
	fmt.Printf("\n%v\n", hex.Dump(b))
	if err != nil {
		return nil, err
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
