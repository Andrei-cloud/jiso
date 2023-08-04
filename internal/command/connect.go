package command

import (
	"fmt"
	"jiso/internal/service"
	"jiso/internal/transactions"
)

type ConnectCommand struct {
	Tc  *transactions.TransactionCollection
	Svc *service.Service
}

func (c *ConnectCommand) Name() string {
	return "connect"
}

func (c *ConnectCommand) Synopsis() string {
	return "Establishes connection to server."
}

func (c *ConnectCommand) Execute() error {
	fmt.Println("Connecting to server...")
	err := c.Svc.Connect()
	if err != nil {
		return err
	}
	fmt.Printf("Connected to server: %s\n", c.Svc.Address)
	return nil
}
