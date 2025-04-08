package command

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	connection "github.com/moov-io/iso8583-connection"
)

type BackgroundCommand struct {
	Tc  transactions.Repository
	Svc *service.Service
	Wrk WorkerController
}

func (c *BackgroundCommand) Name() string {
	return "bgsend"
}

func (c *BackgroundCommand) Synopsis() string {
	return "Send selected transaction in a background process. (reqires connection to server)"
}

func (c *BackgroundCommand) Execute() error {
	if c.Svc.Connection == nil || c.Svc.Connection.Status() != connection.StatusOnline {
		return fmt.Errorf("connection is offline")
	}

	qs := []*survey.Question{
		{
			Name: "trxnname",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: c.Tc.ListNames(),
			},
		},
		{
			Name: "workers",
			Prompt: &survey.Input{
				Default: "1",
				Message: "Enter number of workers:",
			},
			Validate: func(ans interface{}) error {
				_, err := strconv.Atoi(ans.(string))
				if err != nil {
					return errors.New("please enter a valid number")
				}
				return nil
			},
		},
		{
			Name: "interval",
			Prompt: &survey.Input{
				Default: "1s",
				Message: "Enter execution interval (e.g. '1.5s', '500ms', '1m'):",
			},
			Validate: survey.Required,
		},
	}

	answers := struct {
		TrxnName string
		Interval string
		Workers  string
	}{}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	interval, err := time.ParseDuration(answers.Interval)
	if err != nil {
		return err
	}

	numWorkers, err := strconv.Atoi(answers.Workers)
	if err != nil {
		return err
	}

	// Start worker with transaction name and parameters
	workerId, err := c.Wrk.StartWorker(answers.TrxnName, numWorkers, interval)
	if err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	fmt.Printf("Started background worker %s for transaction %s with %d workers at %s interval\n",
		workerId, answers.TrxnName, numWorkers, interval)

	return nil
}
