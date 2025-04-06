package cli

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"time"

	cmd "jiso/internal/command"
	"jiso/internal/service"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	"github.com/olekukonko/tablewriter"
)

type workerStats struct {
	totalRuns     int
	successRuns   int
	failedRuns    int
	lastRunTime   time.Time
	responseCodes map[string]uint64
	durations     []time.Duration
	errors        []string
}

// workerState represents the state of a background worker.
type workerState struct {
	command      cmd.BgCommand
	interval     time.Duration
	done         chan struct{}
	stats        workerStats
	lastActivity time.Time
}

func (cli *CLI) setService(svc *service.Service) {
	cli.svc = svc
}

func (cli *CLI) getSpec() *iso8583.MessageSpec {
	if cli.svc == nil {
		return nil
	}
	return cli.svc.GetSpec()
}

// Update StartWorker to track better statistics
func (cli *CLI) StartWorker(
	name string,
	command cmd.BgCommand,
	numWorkers int,
	interval time.Duration,
) {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	baseWorkerName := name

	for i := 0; i < numWorkers; i++ {
		// Create a unique name for each worker
		if i > 0 {
			name = fmt.Sprintf("%s#%d", baseWorkerName, i)
		}

		// Check if this specific worker name exists
		if _, ok := cli.workers[name]; ok {
			name = generateUniqueWorkerName(name, cli.workers)
			fmt.Printf(
				"Worker with name '%s' already exists, new instance will be named '%s'\n",
				baseWorkerName,
				name,
			)
		}

		// Initialize worker with stats
		locWorkerState := &workerState{
			command:      command,
			interval:     interval,
			done:         make(chan struct{}),
			lastActivity: time.Now(),
			stats: workerStats{
				responseCodes: make(map[string]uint64),
				durations:     make([]time.Duration, 0, 100), // Pre-allocate space
				errors:        make([]string, 0),
			},
		}

		cli.workers[name] = locWorkerState

		// Start the worker goroutine with its own done channel
		go func(workerName string, worker *workerState) {
			jitter := time.Duration(rand.Int63n(int64(interval / 2)))
			ticker := time.NewTicker(interval + jitter)
			for {
				select {
				case <-ticker.C:
					startTime := time.Now()
					worker.lastActivity = startTime

					err := command.ExecuteBackground(workerName)

					duration := time.Since(startTime)

					// Add duration with bounds checking
					if len(worker.stats.durations) >= 1000 {
						// Keep only last 1000 samples to prevent unbounded growth
						worker.stats.durations = worker.stats.durations[1:]
					}
					worker.stats.durations = append(worker.stats.durations, duration)
					worker.stats.totalRuns++

					if err != nil {
						worker.stats.failedRuns++
						worker.stats.errors = append(
							worker.stats.errors,
							fmt.Sprintf("%s: %s", time.Now().Format(time.RFC3339), err),
						)
						// Keep only last 10 errors
						if len(worker.stats.errors) > 10 {
							worker.stats.errors = worker.stats.errors[1:]
						}

						fmt.Printf("Error executing background command '%s': %s\n",
							workerName, err)
					} else {
						worker.stats.successRuns++
					}

					// Update response code statistics
					for rc, count := range command.ResponseCodes() {
						worker.stats.responseCodes[rc] = count
					}

				case <-worker.done:
					ticker.Stop()
					return
				}
			}
		}(name, locWorkerState)

		fmt.Printf(
			"Started background worker '%s' with interval %s\n",
			name,
			interval,
		)
	}
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

	// Create a more detailed view of worker statistics
	fmt.Println("\n--- Worker Statistics ---")

	// Summary table
	summaryTable := tablewriter.NewWriter(os.Stdout)
	summaryTable.SetHeader(
		[]string{"Name", "Total", "Success", "Failed", "Success %", "Last Activity", "Status"},
	)

	var workerNames []string
	for name := range cli.workers {
		workerNames = append(workerNames, name)
	}
	sort.Strings(workerNames)

	for _, name := range workerNames {
		worker := cli.workers[name]
		totalRuns := worker.stats.totalRuns
		successRate := 0.0
		if totalRuns > 0 {
			successRate = float64(worker.stats.successRuns) / float64(totalRuns) * 100
		}

		timeSinceLastActivity := time.Since(worker.lastActivity)
		status := "Active"
		if timeSinceLastActivity > worker.interval*2 {
			status = "Stalled"
		}

		summaryTable.Append([]string{
			name,
			strconv.Itoa(totalRuns),
			strconv.Itoa(worker.stats.successRuns),
			strconv.Itoa(worker.stats.failedRuns),
			fmt.Sprintf("%.1f%%", successRate),
			worker.lastActivity.Format("15:04:05"),
			status,
		})
	}

	summaryTable.Render()
	fmt.Println()

	// Response Codes Table
	fmt.Println("--- Response Codes ---")

	// Collect all unique response codes
	allRCodes := make(map[string]struct{})
	for _, worker := range cli.workers {
		for rc := range worker.stats.responseCodes {
			allRCodes[rc] = struct{}{}
		}
	}

	// Skip if no response codes
	if len(allRCodes) == 0 {
		fmt.Println("No response codes recorded")
	} else {
		// Convert to sorted slice
		rcodes := make([]string, 0, len(allRCodes))
		for rc := range allRCodes {
			rcodes = append(rcodes, rc)
		}
		sort.Strings(rcodes)

		// Create response codes table
		rcTable := tablewriter.NewWriter(os.Stdout)
		headers := []string{"Worker"}
		headers = append(headers, rcodes...)
		rcTable.SetHeader(headers)

		for _, name := range workerNames {
			worker := cli.workers[name]
			row := []string{name}

			for _, rc := range rcodes {
				count, exists := worker.stats.responseCodes[rc]
				if exists {
					row = append(row, strconv.FormatUint(count, 10))
				} else {
					row = append(row, "0")
				}
			}

			rcTable.Append(row)
		}

		rcTable.Render()
	}
	fmt.Println()

	// Error log if there are any errors
	for _, name := range workerNames {
		worker := cli.workers[name]
		if len(worker.stats.errors) > 0 {
			fmt.Printf("\nRecent errors for worker %s:\n", name)
			for _, errMsg := range worker.stats.errors {
				fmt.Printf("  - %s\n", errMsg)
			}
		}
	}
}

func generateUniqueWorkerName(baseName string, workers map[string]*workerState) string {
	index := 1
	newName := baseName
	for {
		if _, exists := workers[newName]; !exists {
			return newName
		}
		newName = fmt.Sprintf("%s#%d", baseName, index)
		index++
	}
}
