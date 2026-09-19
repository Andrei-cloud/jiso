package transactions

import (
	"github.com/moov-io/iso8583"

	cfg "jiso/internal/config"
)

// Repository defines the interface for transaction storage and retrieval
type Repository interface {
	// ListNames returns all available transaction names
	ListNames() []string

	// Info returns transaction details by name: description, fields JSON,
	// the declared spec path, and the resolved dataset with its row count
	// (-1 when a named dataset is missing)
	Info(name string) (TransactionInfo, error)

	// Compose creates a new ISO8583 message from transaction template
	Compose(name string) (*iso8583.Message, error)

	// LogTransaction logs transaction execution results
	LogTransaction(name string, success bool)

	// GetMockRoutes returns configured mock routes
	GetMockRoutes() []cfg.MockRouteConfig

	// SetSpec updates the default fallback spec for transaction composition
	SetSpec(spec *iso8583.MessageSpec)

	// SetTransactionSpec rebinds one transaction to a spec file (empty
	// restores the fallback); the path must parse or it is an error
	SetTransactionSpec(name, specPath string) error
}

// Ensure TransactionCollection implements Repository interface
var _ Repository = (*TransactionCollection)(nil)
