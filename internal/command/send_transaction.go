package command

import (
	"fmt"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"os"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
)

type SendCommand struct {
	Tc  *transactions.TransactionCollection
	Svc *service.Service
}

func (c *SendCommand) Name() string {
	return "send"
}

func (c *SendCommand) Synopsis() string {
	return "Send selected transaction. (reqires connection to server)"
}

func (c *SendCommand) Execute() error {
	if c.Svc.Connection == nil {
		return fmt.Errorf("connection is nil")
	}
	if c.Svc.Connection.Status() != connection.StatusOnline {
		return fmt.Errorf("connection is offline")
	}

	qs := []*survey.Question{
		{
			Name: "send",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: c.Tc.ListNames(),
			},
		},
	}

	var trxnName string
	err := survey.Ask(qs, &trxnName)
	if err != nil {
		return err
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		return err
	}

	// Print ISO8583 message
	iso8583.Describe(msg, os.Stdout, iso8583.DoNotFilterFields()...)

	start := time.Now()
	response, err := c.Svc.Send(msg)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)

	// Print response
	iso8583.Describe(response, os.Stdout, iso8583.DoNotFilterFields()...)

	// Print elapsed time
	fmt.Printf("\nElapsed time: %s\n", elapsed.Round(time.Millisecond))
	return nil
}
