package command

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
)

type StressTestCommand struct {
	Tc  transactions.Repository
	Svc *service.Service
	Wrk WorkerController
}

func (c *StressTestCommand) Name() string {
	return "stresstest"
}

func (c *StressTestCommand) Synopsis() string {
	return "Perform stress testing with gradual TPS ramp-up to target TPS. (requires connection to server)"
}

func (c *StressTestCommand) Execute() error {
	if !c.Svc.IsConnected() {
		return fmt.Errorf("connection is offline")
	}

	qs := []*survey.Question{
		{
			Name: "trxnname",
			Prompt: &survey.Select{
				Message: "Select transaction for stress testing:",
				Options: c.Tc.ListNames(),
			},
		},
		{
			Name: "targettps",
			Prompt: &survey.Input{
				Default: "10",
				Message: "Enter target TPS (transactions per second):",
			},
			Validate: func(ans interface{}) error {
				tps, err := strconv.Atoi(ans.(string))
				if err != nil {
					return errors.New("please enter a valid number")
				}
				if tps <= 0 {
					return errors.New("TPS must be greater than 0")
				}
				if tps > 1000 {
					return errors.New("TPS cannot exceed 1000")
				}
				return nil
			},
		},
		{
			Name: "rampupduration",
			Prompt: &survey.Input{
				Default: "30s",
				Message: "Enter ramp-up duration (e.g. '30s', '1m', '2m'):",
			},
			Validate: survey.Required,
		},
		{
			Name: "workers",
			Prompt: &survey.Input{
				Default: "1",
				Message: "Enter number of concurrent workers:",
			},
			Validate: func(ans interface{}) error {
				workers, err := strconv.Atoi(ans.(string))
				if err != nil {
					return errors.New("please enter a valid number")
				}
				if workers <= 0 {
					return errors.New("workers must be greater than 0")
				}
				if workers > 50 {
					return errors.New("workers cannot exceed 50")
				}
				return nil
			},
		},
	}

	answers := struct {
		TrxnName       string
		TargetTps      string
		RampUpDuration string
		Workers        string
	}{}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	targetTps, err := strconv.Atoi(answers.TargetTps)
	if err != nil {
		return err
	}

	rampUpDuration, err := time.ParseDuration(answers.RampUpDuration)
	if err != nil {
		return err
	}

	numWorkers, err := strconv.Atoi(answers.Workers)
	if err != nil {
		return err
	}

	// Start stress test worker
	workerId, err := c.Wrk.StartStressTestWorker(
		answers.TrxnName,
		targetTps,
		rampUpDuration,
		numWorkers,
	)
	if err != nil {
		return fmt.Errorf("failed to start stress test worker: %w", err)
	}

	fmt.Printf("Started stress test worker %s for transaction %s\n", workerId, answers.TrxnName)
	fmt.Printf(
		"Target TPS: %d, Ramp-up duration: %s, Workers: %d\n",
		targetTps,
		rampUpDuration,
		numWorkers,
	)

	return nil
}
