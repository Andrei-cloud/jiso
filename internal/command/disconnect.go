package command

import (
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

func (c *DisconnectCommand) Execute() error {
	fmt.Println("Disconnecting...")
	err := c.Svc.Disconnect()
	if err != nil {
		return err
	}
	fmt.Println("Disconnected from server")
	return nil
}
