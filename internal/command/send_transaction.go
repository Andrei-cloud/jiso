package command

import (
	"jiso/internal/service"
	"jiso/internal/transactions"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
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

	response, err := c.Svc.Send(msg)
	if err != nil {
		return err
	}

	// Print response
	iso8583.Describe(response, os.Stdout, iso8583.DoNotFilterFields()...)

	return nil
}
