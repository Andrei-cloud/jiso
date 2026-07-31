package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"jiso/internal/command"
	"jiso/internal/metrics"

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
	networkStats        *metrics.NetworkingStats
	successful          int
	failed              int
	consecutiveFailures int
	mu                  sync.Mutex
	wg                  sync.WaitGroup // WaitGroup to ensure clean shutdown
}

// StartWorker starts a new worker with the given parameters
func (cli *CLI) StartWorker(name string, count int, interval time.Duration) (string, error) {
	// Generate a unique ID for the worker
	workerID := uuid.New().String()[:8]

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create a new worker state
	worker := &workerInfo{
		id:           workerID,
		name:         name,
		count:        count,
		interval:     interval,
		startTime:    time.Now(),
		ctx:          ctx,
		cancel:       cancel,
		networkStats: cli.networkStats,
	}

	// Store the worker
	cli.mu.Lock()
	cli.workers[workerID] = worker
	cli.mu.Unlock()

	// Start the worker in a goroutine
	worker.wg.Add(1)
	go func() {
		defer worker.wg.Done()

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
					_, _, err := sendCmd.ExecuteBackground(name, false, "")
					worker.mu.Lock()
					if err == nil {
						worker.successful++
						worker.consecutiveFailures = 0
					} else {
						worker.failed++
						worker.consecutiveFailures++
					}

					// Circuit breaker: record trip if activated
					if worker.consecutiveFailures >= 10 {
						if worker.networkStats != nil {
							worker.networkStats.RecordCircuitBreakerTrip()
						}
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

// StartStressTestWorker starts a stress test worker with TPS ramp-up
func (cli *CLI) StartStressTestWorker(
	names []string,
	targetTps int,
	rampUpDuration time.Duration,
	duration time.Duration,
	numWorkers int,
) (string, error) {
	// Generate a unique ID for the worker
	workerID := uuid.New().String()[:8]
	sessionID := uuid.New().String()

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	originalMaxPending := 100
	if cli.svc != nil {
		originalMaxPending = cli.svc.GetMaxPendingRequests()
		timeoutSec := int(cli.svc.GetResponseTimeout().Seconds())
		if timeoutSec < 1 {
			timeoutSec = 1
		}
		requiredMaxPending := targetTps * timeoutSec
		if requiredMaxPending < 1000 {
			requiredMaxPending = 1000
		}
		if requiredMaxPending > originalMaxPending {
			cli.svc.SetMaxPendingRequests(requiredMaxPending)
		}
	}

	// Compute expected number of requests to pre-allocate maps and slices
	totalDurationSec := int((rampUpDuration + duration).Seconds())
	if totalDurationSec < 1 {
		totalDurationSec = 1
	}
	expectedRequests := targetTps * totalDurationSec
	if expectedRequests < 1000 {
		expectedRequests = 1000
	}

	txStatsMap := make(map[string]*txStats)
	expectedRequestsPerTx := expectedRequests
	if len(names) > 0 {
		expectedRequestsPerTx = expectedRequests / len(names)
	}
	if expectedRequestsPerTx < 200 {
		expectedRequestsPerTx = 200
	}
	for _, name := range names {
		txStatsMap[name] = &txStats{
			respCodes: make(map[string]int),
			latencies: make([]time.Duration, 0, expectedRequestsPerTx),
		}
	}

	// Create a new stress test worker state
	worker := &stressTestWorker{
		id:                 workerID,
		sessionID:          sessionID,
		names:              names,
		targetTps:          targetTps,
		rampUpDuration:     rampUpDuration,
		duration:           duration,
		numWorkers:         numWorkers,
		startTime:          time.Now(),
		ctx:                ctx,
		cancel:             cancel,
		networkStats:       cli.networkStats,
		currentTps:         0,
		actualTps:          0,
		rampUpProgress:     0.0,
		latencies:          make([]time.Duration, 0, expectedRequests),
		respCodes:          make(map[string]int),
		txStats:            txStatsMap,
		originalMaxPending: originalMaxPending,
	}

	// Store the worker
	cli.mu.Lock()
	cli.stressWorkers[workerID] = worker
	cli.mu.Unlock()

	// Start the stress test worker in a goroutine
	worker.wg.Add(1)
	go func() {
		defer worker.wg.Done()
		worker.runStressTest(cli)
	}()

	return workerID, nil
}

// StopWorker stops a worker by its ID
func (cli *CLI) StopWorker(id string) error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	// Check regular workers first
	worker, exists := cli.workers[id]
	if exists {
		worker.cancel()
		// Wait for the goroutine to finish with a timeout
		done := make(chan struct{})
		go func() {
			worker.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Goroutine finished cleanly
		case <-time.After(5 * time.Second):
			// Timeout - goroutine didn't finish, but continue with cleanup
			fmt.Printf("Warning: Worker %s did not stop cleanly within timeout\n", id)
		}
		delete(cli.workers, id)
		return nil
	}

	// Check stress test workers
	stressWorker, exists := cli.stressWorkers[id]
	if exists {
		stressWorker.cancel()
		// Wait for the goroutine to finish with a timeout
		done := make(chan struct{})
		go func() {
			stressWorker.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Goroutine finished cleanly
		case <-time.After(5 * time.Second):
			// Timeout - goroutine didn't finish, but continue with cleanup
			fmt.Printf("Warning: Stress test worker %s did not stop cleanly within timeout\n", id)
		}
		delete(cli.stressWorkers, id)
		return nil
	}

	return fmt.Errorf("worker with ID %s not found", id)
}

// StopAllWorkers stops all running workers
func (cli *CLI) StopAllWorkers() error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	// Collect all workers to stop
	workersToStop := make([]*workerInfo, 0, len(cli.workers))
	stressWorkersToStop := make([]*stressTestWorker, 0, len(cli.stressWorkers))

	for _, worker := range cli.workers {
		workersToStop = append(workersToStop, worker)
	}
	for _, stressWorker := range cli.stressWorkers {
		stressWorkersToStop = append(stressWorkersToStop, stressWorker)
	}

	// Clear maps immediately to prevent new operations
	cli.workers = make(map[string]*workerInfo)
	cli.stressWorkers = make(map[string]*stressTestWorker)

	// Cancel all workers
	for _, worker := range workersToStop {
		worker.cancel()
	}
	for _, stressWorker := range stressWorkersToStop {
		stressWorker.cancel()
	}

	cli.mu.Unlock() // Unlock while waiting for goroutines

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		for _, worker := range workersToStop {
			worker.wg.Wait()
		}
		for _, stressWorker := range stressWorkersToStop {
			stressWorker.wg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished cleanly
	case <-time.After(10 * time.Second):
		// Timeout - some goroutines didn't finish, but continue
		fmt.Printf("Warning: Some workers did not stop cleanly within timeout\n")
	}

	cli.mu.Lock() // Re-lock before returning
	return nil
}

// GetWorkerStats returns statistics for all workers
func (cli *CLI) GetWorkerStats() map[string]interface{} {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	stats := make(map[string]interface{})

	totalWorkers := len(cli.workers) + len(cli.stressWorkers)
	if totalWorkers == 0 {
		stats["active"] = 0
		return stats
	}

	stats["active"] = totalWorkers
	workerDetails := make([]map[string]interface{}, 0, totalWorkers)

	// Add regular workers
	for id, worker := range cli.workers {
		worker.mu.Lock()
		workerStats := map[string]interface{}{
			"id":                   id,
			"name":                 worker.name,
			"type":                 "background",
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

	// Add stress test workers
	for id, stressWorker := range cli.stressWorkers {
		stressWorker.mu.Lock()

		runtimeStr := ""
		if stressWorker.completed {
			runtimeStr = stressWorker.endTime.Sub(stressWorker.startTime).Round(time.Second).String()
		} else {
			runtimeStr = time.Since(stressWorker.startTime).Round(time.Second).String()
		}

		statusStr := "running"
		if stressWorker.completed {
			statusStr = "completed"
		}

		stressWorkerStats := map[string]interface{}{
			"id":                   id,
			"name":                 strings.Join(stressWorker.names, ", "),
			"type":                 "stress_test",
			"status":               statusStr,
			"target_tps":           stressWorker.targetTps,
			"current_tps":          stressWorker.currentTps,
			"actual_tps":           stressWorker.actualTps,
			"ramp_up_progress":     stressWorker.rampUpProgress,
			"ramp_up_duration":     stressWorker.rampUpDuration.String(),
			"duration":             stressWorker.duration.String(),
			"workers":              stressWorker.numWorkers,
			"runtime":              runtimeStr,
			"successful":           stressWorker.successful,
			"failed":               stressWorker.failed,
			"total":                stressWorker.successful + stressWorker.failed,
			"consecutive_failures": stressWorker.consecutiveFailures,
		}
		stressWorker.mu.Unlock()

		workerDetails = append(workerDetails, stressWorkerStats)
	}

	stats["workers"] = workerDetails
	return stats
}

