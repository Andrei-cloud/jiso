package service

import (
	"common/utils"
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
	fmt.Printf("Spec file loaded successfully, current spec: %s", spec.Name)

	// // Connect to server
	// conn, err := connection.New(host+":"+port, spec, utils.ReadMessageLength, utils.WriteMessageLength)
	// if err != nil {
	// 	return nil, err
	// }
	// err = conn.Connect()
	// if err != nil {
	// 	return nil, err
	// }

	return &Service{
		MessageSpec:  spec,
		Transactions: make([]Transaction, 0),
	}, nil
}

func (s *Service) Close() error {
	if s.Connection == nil {
		return nil
	}
	fmt.Println("Closing connection")
	return s.Connection.Close()
}
