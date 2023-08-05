package command

import (
	"fmt"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"os"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
)

type SendCommand struct {
	Tc     *transactions.TransactionCollection
	Svc    *service.Service
	start  time.Time
	counts int
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

	c.start = time.Now()
	response, err := c.Svc.Send(msg)
	if err != nil {
		return err
	}
	elapsed := time.Since(c.start)

	// Print response
	iso8583.Describe(response, os.Stdout, iso8583.DoNotFilterFields()...)

	// Print elapsed time
	fmt.Printf("\nElapsed time: %s\n", elapsed.Round(time.Millisecond))
	return nil
}

func (c *SendCommand) StartClock() {
	c.start = time.Now()
}

func (c *SendCommand) ExecuteBackground(trxnName string) error {
	if strings.Contains(trxnName, "#") {
		parts := strings.Split(trxnName, "#")
		trxnName = parts[0]
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		return err
	}

	_, err = c.Svc.BackgroundSend(msg)
	if err != nil {
		return err
	}

	c.counts++
	// check response

	return nil
}

func (c *SendCommand) Stats() int {
	return c.counts
}
func (c *SendCommand) Duration() time.Duration {
	return time.Since(c.start)
}
