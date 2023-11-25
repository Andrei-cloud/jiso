package command

import (
	"fmt"
	"jiso/internal/service"
	"jiso/internal/transactions"

	connection "github.com/moov-io/iso8583-connection"
)

type DisconnectCommand struct {
	Tc  *transactions.TransactionCollection
	Svc *service.Service
}

func (c *DisconnectCommand) Name() string {
	return "disconnect"
}

func (c *DisconnectCommand) Synopsis() string {
	return "Closes connection to server."
}

// disconnect.go

// Improved error handling and clarity in the Execute() method.
func (c *DisconnectCommand) Execute() error {
	fmt.Println("Disconnecting...")
	if c.Svc.Connection == nil {
		return fmt.Errorf("no active connection")
	}
	if c.Svc.Connection.Status() != connection.StatusOnline {
		return fmt.Errorf("connection is not online")
	}

	if err := c.Svc.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	fmt.Println("Disconnected from server")
	return nil
}
