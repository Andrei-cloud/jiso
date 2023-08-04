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

func (c *DisconnectCommand) Execute() error {
	fmt.Println("Disconnecting...")
	if c.Svc.Connection == nil {
		return fmt.Errorf("connection is nil")
	}
	if c.Svc.Connection.Status() != connection.StatusOnline {
		return fmt.Errorf("connection is offline")
	}
	err := c.Svc.Disconnect()
	if err != nil {
		return err
	}
	fmt.Println("Disconnected from server")
	return nil
}
