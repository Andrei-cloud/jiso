package cli

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	cmd "jiso/internal/command"
	"jiso/internal/service"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	"github.com/olekukonko/tablewriter"
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

func (cli *CLI) StartWorker(
	name string,
	command cmd.BgCommand,
	numWorkers int,
	interval time.Duration,
) {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	if _, ok := cli.workers[name]; ok {
		index := 1
		for {
			newName := fmt.Sprintf("%s#%d", name, index)
			if _, ok := cli.workers[newName]; !ok {
				name = newName
				break
			}
			index++
		}
		fmt.Printf(
			"Worker with name '%s' already exists, new instance will be named '%s'\n",
			name[:len(name)-2],
			name,
		)
	}

	done := make(chan struct{})
	for i := 0; i < numWorkers; i++ {
		go func() {
			jitter := time.Duration(rand.Int63n(int64(interval / 2)))
			ticker := time.NewTicker(interval + jitter)
			for {
				select {
				case <-ticker.C:
					err := command.ExecuteBackground(name)
					if err != nil {
						fmt.Printf("Error executing background command '%s': %s\n", name, err)
					}
				case <-done:
					ticker.Stop()
					return
				}
			}
		}()
	}

	cli.workers[name] = &workerState{
		command:  command,
		interval: interval,
		done:     done,
	}

	fmt.Printf(
		"Started background worker for command '%s' with interval %s\n",
		name,
		interval,
	)
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

func (cli *CLI) printWorkerStats() {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	if len(cli.workers) == 0 {
		fmt.Println("No background workers running")
		return
	}

	// Define the table headers
	headers := []string{"Name", "Runs", "Interval", "Duration", "Mean", "StdDev"}

	// Define the table rows
	var rows [][]string
	for name, worker := range cli.workers {
		row := []string{
			name,
			strconv.Itoa(worker.command.Stats()),
			worker.interval.String(),
			worker.command.Duration().String(),
			worker.command.MeanExecutionTime().String(),
			worker.command.StandardDeviation().String(),
		}
		for rc, count := range worker.command.ResponseCodes() {
			headers = append(headers, rc)
			row = append(row, strconv.FormatUint(count, 10))
		}

		rows = append(rows, row)
	}

	// Print the table
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(headers)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.AppendBulk(rows)
	table.Render()
}
