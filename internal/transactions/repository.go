package transactions

import (
	"github.com/moov-io/iso8583"
)

// Repository defines the interface for transaction storage and retrieval
type Repository interface {
	// ListNames returns all available transaction names
	ListNames() []string

	// Info returns transaction details by name
	Info(name string) (string, string, string, error)

	// Compose creates a new ISO8583 message from transaction template
	Compose(name string) (*iso8583.Message, error)

	// LogTransaction logs transaction execution results
	LogTransaction(name string, success bool)
}

// Ensure TransactionCollection implements Repository interface
var _ Repository = (*TransactionCollection)(nil)
