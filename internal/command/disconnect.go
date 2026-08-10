package command

import (
	"errors"
	"fmt"

	"jiso/internal/service"
	"jiso/internal/transactions"
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

// Execute closes active connection to server.
func (c *DisconnectCommand) Execute() error {
	fmt.Println("Disconnecting...")
	if c.Svc.Connection == nil {
		return errors.New("no active connection")
	}

	// Allow disconnecting even if connection is in a non-online state
	// This helps clean up stale connection objects
	err := c.Svc.Disconnect()
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	fmt.Println("Disconnected from server")

	return nil
}
