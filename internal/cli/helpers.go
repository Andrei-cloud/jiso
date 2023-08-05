package cli

import (
	"fmt"
	cmd "jiso/internal/command"
	"jiso/internal/service"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

func (cli *CLI) setService(svc *service.Service) {
	cli.svc = svc
}

func (cli *CLI) getSpec() *iso8583.MessageSpec {
	if cli.svc == nil {
		return nil
	}
	return cli.svc.GetSpec()
}

func (cli *CLI) StartWorker(name string, command cmd.BgCommand, interval time.Duration) {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	if _, ok := cli.workers[name]; ok {
		fmt.Printf("Worker with name '%s' already exists\n", name)
		return
	}

	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				//fmt.Printf("Executing background command '%s'\n", name)
				err := command.ExecuteBackground(name)
				if err != nil {
					fmt.Printf("Error executing background command '%s': %s\n", name, err)
				}
			case <-done:
				//fmt.Printf("Stopping background command '%s'\n", name)
				return
			}
		}
	}()

	cli.workers[name] = &workerState{
		command:  command,
		interval: interval,
		ticker:   ticker,
		done:     done,
	}

	fmt.Printf("Started background command '%s' with interval %s\n", name, interval)
}

func (cli *CLI) stopWorker() error {
	// Get the list of worker names
	var workerNames []string
	for name := range cli.workers {
		workerNames = append(workerNames, name)
	}

	// Prompt the user to select a worker
	var selectedWorker string
	err := cli.prompt([]*survey.Question{
		{
			Name: "worker",
			Prompt: &survey.Select{
				Message: "Select a worker:",
				Options: workerNames,
			},
		},
	}, &selectedWorker)
	if err != nil {
		return err
	}

	cli.mu.Lock()
	defer cli.mu.Unlock()

	if worker, ok := cli.workers[selectedWorker]; ok {
		close(worker.done)
		delete(cli.workers, selectedWorker)
		fmt.Printf("Stopped background command '%s'\n", selectedWorker)
	} else {
		fmt.Printf("No worker with name '%s' found\n", selectedWorker)
	}
	return nil
}

func (cli *CLI) stopAllWorkers() {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	for name, worker := range cli.workers {
		close(worker.done)
		delete(cli.workers, name)
		fmt.Printf("Stopped background command '%s'\n", name)
	}
}

func (cli *CLI) printWorkerStatus() {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	if len(cli.workers) == 0 {
		fmt.Println("No background workers running")
		return
	}

	fmt.Println("Running background workers:")
	for name, worker := range cli.workers {
		fmt.Printf("\t%s (interval: %s, running: %v)\n", name, worker.interval, worker.command.Duration())
	}
}
