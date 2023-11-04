package command

import (
	"fmt"

	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
)

type InfoCommand struct {
	Tc **transactions.TransactionCollection
}

func (c *InfoCommand) Name() string {
	return "info"
}

func (c *InfoCommand) Synopsis() string {
	return "Command prints details of selected transcation."
}

func (c *InfoCommand) Execute() error {
	qs := []*survey.Question{
		{
			Name: "info",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: (**c.Tc).ListNames(),
			},
		},
	}

	var trxnName string
	err := survey.Ask(qs, &trxnName)
	if err != nil {
		return err
	}

	name, desc, body, err := (**c.Tc).Info(trxnName)
	if err != nil {
		return err
	}

	fmt.Printf("Name: %s\n", name)
	fmt.Printf("Description: %s\n", desc)
	fmt.Printf("Body: \n%s\n", body)

	return nil
}
