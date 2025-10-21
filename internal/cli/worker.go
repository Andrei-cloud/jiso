package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"jiso/internal/command"

	"github.com/google/uuid"
)

// workerInfo holds the state of a background worker
// Use a different name to avoid conflict with existing workerState
type workerInfo struct {
	id                  string
	name                string
	count               int
	interval            time.Duration
	startTime           time.Time
	ctx                 context.Context
	cancel              context.CancelFunc
	successful          int
	failed              int
	consecutiveFailures int
	mu                  sync.Mutex
}

// StartWorker starts a new worker with the given parameters
func (cli *CLI) StartWorker(name string, count int, interval time.Duration) (string, error) {
	// Generate a unique ID for the worker
	workerID := uuid.New().String()[:8]

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create a new worker state
	worker := &workerInfo{
		id:        workerID,
		name:      name,
		count:     count,
		interval:  interval,
		startTime: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Store the worker
	cli.mu.Lock()
	cli.workers[workerID] = worker
	cli.mu.Unlock()

	// Start the worker in a goroutine
	go func() {
		sendCmd, ok := cli.commands["send"].(*command.SendCommand)
		if !ok {
			fmt.Printf("Error: send command not found or has wrong type\n")
			return
		}

		sendCmd.StartClock()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for i := 0; i < count; i++ {
					err := sendCmd.ExecuteBackground(name)
					worker.mu.Lock()
					if err == nil {
						worker.successful++
						worker.consecutiveFailures = 0
					} else {
						worker.failed++
						worker.consecutiveFailures++
					}

					// Circuit breaker: stop worker after 10 consecutive failures
					if worker.consecutiveFailures >= 10 {
						fmt.Printf(
							"Worker %s stopped due to %d consecutive failures\n",
							worker.id,
							worker.consecutiveFailures,
						)
						worker.mu.Unlock()
						return
					}
					worker.mu.Unlock()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return workerID, nil
}

// StopWorker stops a worker by its ID
func (cli *CLI) StopWorker(id string) error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	worker, exists := cli.workers[id]
	if !exists {
		return fmt.Errorf("worker with ID %s not found", id)
	}

	worker.cancel()
	delete(cli.workers, id)
	return nil
}

// StopAllWorkers stops all running workers
func (cli *CLI) StopAllWorkers() error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	for id, worker := range cli.workers {
		worker.cancel()
		delete(cli.workers, id)
	}
	return nil
}

// GetWorkerStats returns statistics for all workers
func (cli *CLI) GetWorkerStats() map[string]interface{} {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	stats := make(map[string]interface{})

	if len(cli.workers) == 0 {
		stats["active"] = 0
		return stats
	}

	stats["active"] = len(cli.workers)
	workerDetails := make([]map[string]interface{}, 0, len(cli.workers))

	for id, worker := range cli.workers {
		worker.mu.Lock()
		workerStats := map[string]interface{}{
			"id":                   id,
			"name":                 worker.name,
			"workers":              worker.count,
			"interval":             worker.interval.String(),
			"runtime":              time.Since(worker.startTime).Round(time.Second).String(),
			"successful":           worker.successful,
			"failed":               worker.failed,
			"total":                worker.successful + worker.failed,
			"consecutive_failures": worker.consecutiveFailures,
		}
		worker.mu.Unlock()

		workerDetails = append(workerDetails, workerStats)
	}

	stats["workers"] = workerDetails
	return stats
}
