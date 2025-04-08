package command

import (
	"errors"
	"fmt"

	"jiso/internal/utils"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/core"
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
			Validate: func(ans interface{}) error {
				validTypes := map[string]bool{
					"ascii4":  true,
					"binary2": true,
					"bcd2":    true,
					"NAPS":    true,
				}

				// Properly handle the response type
				option, ok := ans.(core.OptionAnswer)
				if !ok {
					// Try to convert directly to string as a fallback
					str, ok := ans.(string)
					if !ok {
						return errors.New("unexpected answer type")
					}
					if _, valid := validTypes[str]; !valid {
						return errors.New("invalid length type selected")
					}
					return nil
				}

				// Check if the value is valid
				if _, valid := validTypes[option.Value]; !valid {
					return errors.New("invalid length type selected")
				}
				return nil
			},
		},
	}

	// Answer will be stored here
	answers := struct {
		Length string `survey:"length"`
	}{}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	header, err := utils.SelectLength(answers.Length)
	if err != nil {
		return err
	}

	fmt.Println("Connecting to server...")
	naps := (answers.Length == "NAPS")
	err = c.Svc.Connect(naps, header)
	if err != nil {
		return fmt.Errorf("failed to connect to server at %s: %w", c.Svc.Address, err)
	}

	// Double-check connection status after connecting
	if c.Svc.Connection == nil {
		return fmt.Errorf("connection object is nil after connecting to %s", c.Svc.Address)
	}

	// Verify the connection status one more time
	if !c.Svc.IsConnected() {
		return fmt.Errorf("connection to %s is not online", c.Svc.Address)
	}

	fmt.Printf("Successfully connected to server: %s\n", c.Svc.Address)
	return nil
}
