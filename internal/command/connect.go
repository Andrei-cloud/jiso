package command

import (
	"fmt"
	"jiso/internal/utils"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
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
	qs := []*survey.Question{
		{
			Name: "length",
			Prompt: &survey.Select{
				Message: "Select length type:",
				Options: []string{"ascii4", "binary2", "bcd2", "NAPS"},
			},
		},
	}

	var lenType string
	err := survey.Ask(qs, &lenType)
	if err != nil {
		return err
	}

	utils.SelectLength(lenType)

	fmt.Println("Connecting to server...")
	naps := (lenType == "NAPS")
	err = c.Svc.Connect(naps)
	if err != nil {
		return err
	}
	fmt.Printf("Connected to server: %s\n", c.Svc.Address)
	return nil
}
