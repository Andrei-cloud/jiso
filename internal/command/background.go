package command

import (
	"strconv"
	"time"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	connection "github.com/moov-io/iso8583-connection"
)

type BackgroundCommand struct {
	Tc  **transactions.TransactionCollection
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
		return ErrConnectionOffline
	}

	qs := []*survey.Question{
		{
			Name: "trxnname",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: (**c.Tc).ListNames(),
			},
		},
		{
			Name: "workers",
			Prompt: &survey.Input{
				Default: "1",
				Message: "Enter number of workers:",
			},
			Validate: survey.Required,
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

	command := &SendCommand{Tc: c.Tc, Svc: c.Svc}
	command.StartClock()
	c.Wrk.StartWorker(answers.TrxnName, command, numWorkers, interval)

	return nil
}
