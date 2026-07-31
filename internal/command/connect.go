package command

import (
	"errors"
	"fmt"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/core"
	"github.com/moov-io/iso8583"
)

type ConnectCommand struct {
	Tc   transactions.Repository
	Svc  *service.Service
	Ctrl CLIController
}

func (c *ConnectCommand) Name() string {
	return "connect"
}

func (c *ConnectCommand) Synopsis() string {
	return "Establishes connection to server."
}

func (c *ConnectCommand) Execute() error {
	if err := VerifyTarget(); err != nil {
		return err
	}

	if c.Svc != nil {
		host := config.GetConfig().GetHost()
		port := config.GetConfig().GetPort()
		if host != "" && port != "" {
			c.Svc.SetTarget(host, port)
		}
	}

	qs := []*survey.Question{
		{
			Name: "length",
			Prompt: &survey.Select{
				Message: "Select length type:",
				Options: []string{"ascii4", "binary2", "binary4", "bcd2", "NAPS", "visa"},
			},
			Validate: func(ans interface{}) error {
				validTypes := map[string]bool{
					"ascii4":  true,
					"binary2": true,
					"binary4": true,
					"bcd2":    true,
					"NAPS":    true,
					"visa":    true,
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

	if answers.Length == "visa" {
		stationID := config.GetConfig().GetVisaStationId()
		if stationID != "" {
			if _, err := utils.ParseStationID(stationID); err == nil {
				fmt.Printf("Using Local Station ID from command argument: %s\n", stationID)
			} else {
				fmt.Printf("Invalid Station ID from command argument '%s': %v. Please enter a valid ID.\n", stationID, err)
				stationID = ""
			}
		}

		if stationID == "" {
			stationPrompt := &survey.Input{
				Message: "Enter Local Station ID (6-digit numeric):",
			}
			err = survey.AskOne(stationPrompt, &stationID, survey.WithValidator(func(val interface{}) error {
				str, ok := val.(string)
				if !ok {
					return errors.New("invalid input type")
				}
				_, err := utils.ParseStationID(str)
				if err != nil {
					return err
				}
				return nil
			}))
			if err != nil {
				return err
			}
			config.GetConfig().SetVisaStationId(stationID)
		}
	}

	// Ask if unsolicited incoming messages should be parsed and processed via mock_routes
	var processUnsolicited bool
	unsolicitedPrompt := &survey.Confirm{
		Message: "Parse and process unsolicited incoming messages via mock_routes?",
		Default: false,
	}
	if err := survey.AskOne(unsolicitedPrompt, &processUnsolicited); err != nil {
		return err
	}

	if processUnsolicited {
		txPath := config.GetConfig().GetFile()
		if txPath == "" {
			fmt.Println("No transaction file currently selected.")
			selector := NewFileSelector("transaction")
			selected, err := selector.SelectFile()
			if err != nil {
				return fmt.Errorf("transaction file selection failed: %w", err)
			}
			txPath = selected
			config.GetConfig().SetFile(txPath)

			if c.Ctrl != nil {
				if _, err := c.Ctrl.ReloadTransactions(txPath); err != nil {
					return fmt.Errorf("failed to reload transaction repository: %w", err)
				}
			} else {
				var spec *iso8583.MessageSpec
				if c.Svc != nil {
					spec = c.Svc.GetSpec()
				}
				tc, err := transactions.NewTransactionCollection(txPath, spec)
				if err != nil {
					return fmt.Errorf("failed to load transaction file '%s': %w", txPath, err)
				}
				c.Tc = tc
			}
		}

		var routes []config.MockRouteConfig
		if c.Tc != nil {
			routes = c.Tc.GetMockRoutes()
		}

		if len(routes) == 0 {
			fmt.Printf("Warning: Selected transaction file '%s' contains no mock_routes.\n", txPath)
			if c.Svc != nil {
				c.Svc.SetMockMatcher(nil)
			}
		} else {
			fmt.Printf("Loaded %d mock route(s) from '%s' for unsolicited incoming message handling.\n", len(routes), txPath)
			if c.Svc != nil {
				c.Svc.SetMockMatcher(server.NewMatcher(routes))
			}
		}
	} else {
		if c.Svc != nil {
			c.Svc.SetMockMatcher(nil)
		}
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
