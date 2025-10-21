package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"jiso/internal/command"
	"jiso/internal/metrics"

	"github.com/google/uuid"
)

// stressTestWorker holds the state of a stress test worker
type stressTestWorker struct {
	id                  string
	name                string
	targetTps           int
	rampUpDuration      time.Duration
	duration            time.Duration
	numWorkers          int
	startTime           time.Time
	ctx                 context.Context
	cancel              context.CancelFunc
	networkStats        *metrics.NetworkingStats
	currentTps          float64
	actualTps           float64
	rampUpProgress      float64
	successful          int
	failed              int
	consecutiveFailures int
	mu                  sync.Mutex
}

// runStressTest implements the stress testing logic with TPS ramp-up
func (w *stressTestWorker) runStressTest(cli *CLI) {
	sendCmd, ok := cli.commands["send"].(*command.SendCommand)
	if !ok {
		fmt.Printf("Error: send command not found or has wrong type\n")
		return
	}

	sendCmd.StartClock()

	// Start with 1 TPS and ramp up to target TPS
	startTps := 1.0
	rampUpSteps := 100 // Number of ramp-up steps
	stepDuration := w.rampUpDuration / time.Duration(rampUpSteps)

	tpsIncrement := float64(w.targetTps-1) / float64(rampUpSteps)

	fmt.Printf("Stress test worker %s starting ramp-up to %d TPS over %s\n",
		w.id, w.targetTps, w.rampUpDuration)

	for step := 0; step <= rampUpSteps; step++ {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		// Calculate current target TPS for this step
		currentTargetTps := startTps + (float64(step) * tpsIncrement)
		if currentTargetTps > float64(w.targetTps) {
			currentTargetTps = float64(w.targetTps)
		}

		w.mu.Lock()
		w.currentTps = currentTargetTps
		w.rampUpProgress = float64(step) / float64(rampUpSteps) * 100.0
		w.mu.Unlock()

		// Calculate interval for this TPS
		interval := time.Duration(float64(time.Second) / currentTargetTps / float64(w.numWorkers))
		if interval < time.Millisecond {
			interval = time.Millisecond // Minimum interval
		}

		// Run at this TPS for the step duration
		stepEnd := time.Now().Add(stepDuration)
		stepStart := time.Now()
		stepTransactions := 0
		successfulAtStepStart := w.successful
		nextSend := time.Now() // Send first transaction immediately

		for time.Now().Before(stepEnd) {
			// Wait until it's time to send the next batch
			if time.Now().Before(nextSend) {
				time.Sleep(time.Until(nextSend))
			}

			// Send one batch of transactions (one per worker)
			for i := 0; i < w.numWorkers; i++ {
				err := sendCmd.ExecuteBackground(w.name)
				w.mu.Lock()
				if err == nil {
					w.successful++
					w.consecutiveFailures = 0
				} else {
					w.failed++
					w.consecutiveFailures++
				}

				// Circuit breaker: record trip if activated
				if w.consecutiveFailures >= 10 {
					if w.networkStats != nil {
						w.networkStats.RecordCircuitBreakerTrip()
					}
					fmt.Printf(
						"Stress test worker %s stopped due to %d consecutive failures\n",
						w.id,
						w.consecutiveFailures,
					)
					w.mu.Unlock()
					return
				}
				w.mu.Unlock()
				stepTransactions++
			}

			// Schedule next send
			nextSend = nextSend.Add(interval)
		}

		// Calculate actual TPS for this step
		stepDurationActual := time.Since(stepStart)
		if stepDurationActual > 0 {
			// TPS should be based on successful transactions in THIS step only
			w.mu.Lock()
			successfulInThisStep := w.successful - successfulAtStepStart
			actualTps := float64(successfulInThisStep) / stepDurationActual.Seconds()
			w.actualTps = actualTps
			w.mu.Unlock()

			fmt.Printf(
				"\rWorker %s: Step %d/%d - Target: %.1f TPS, Progress: %.1f%%",
				w.id,
				step+1,
				rampUpSteps+1,
				currentTargetTps,
				w.rampUpProgress,
			)
		}
	}

	// Ramp-up complete, continue at target TPS for the specified duration
	fmt.Printf(
		"\nWorker %s: Ramp-up complete. Maintaining %d TPS for %s\n",
		w.id,
		w.targetTps,
		w.duration,
	)

	finalInterval := time.Duration(
		float64(time.Second) / float64(w.targetTps) / float64(w.numWorkers),
	)
	if finalInterval < time.Millisecond {
		finalInterval = time.Millisecond
	}

	testEnd := time.Now().Add(w.duration)
	nextSend := time.Now() // Send first transaction immediately
	successfulAtFinalStart := w.successful
	finalStartTime := time.Now()

	for time.Now().Before(testEnd) {
		// Wait until it's time to send the next batch
		if time.Now().Before(nextSend) {
			time.Sleep(time.Until(nextSend))
		}

		// Send one batch of transactions (one per worker)
		for i := 0; i < w.numWorkers; i++ {
			err := sendCmd.ExecuteBackground(w.name)
			w.mu.Lock()
			if err == nil {
				w.successful++
				w.consecutiveFailures = 0
			} else {
				w.failed++
				w.consecutiveFailures++
			}

			// Circuit breaker: record trip if activated
			if w.consecutiveFailures >= 10 {
				if w.networkStats != nil {
					w.networkStats.RecordCircuitBreakerTrip()
				}
				fmt.Printf(
					"Stress test worker %s stopped due to %d consecutive failures\n",
					w.id,
					w.consecutiveFailures,
				)
				w.mu.Unlock()
				return
			}
			w.mu.Unlock()
		}

		// Schedule next send
		nextSend = nextSend.Add(finalInterval)
	}

	// Calculate final TPS based on the test duration phase
	finalDurationActual := time.Since(finalStartTime)
	if finalDurationActual > 0 {
		w.mu.Lock()
		successfulInFinal := w.successful - successfulAtFinalStart
		finalTps := float64(successfulInFinal) / finalDurationActual.Seconds()
		w.mu.Unlock()

		fmt.Printf(
			"Worker %s: Test completed. Target TPS: %d, Actual TPS: %.1f, Total transactions: %d successful, %d failed\n",
			w.id,
			w.targetTps,
			finalTps,
			w.successful,
			w.failed,
		)
	}

	fmt.Printf("Worker %s: Test duration elapsed. Stopping.\n", w.id)
}

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
	name string,
	targetTps int,
	rampUpDuration time.Duration,
	duration time.Duration,
	numWorkers int,
) (string, error) {
	// Generate a unique ID for the worker
	workerID := uuid.New().String()[:8]

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create a new stress test worker state
	worker := &stressTestWorker{
		id:             workerID,
		name:           name,
		targetTps:      targetTps,
		rampUpDuration: rampUpDuration,
		duration:       duration,
		numWorkers:     numWorkers,
		startTime:      time.Now(),
		ctx:            ctx,
		cancel:         cancel,
		networkStats:   cli.networkStats,
		currentTps:     0,
		actualTps:      0,
		rampUpProgress: 0.0,
	}

	// Store the worker
	cli.mu.Lock()
	cli.stressWorkers[workerID] = worker
	cli.mu.Unlock()

	// Start the stress test worker in a goroutine
	go func() {
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
		delete(cli.workers, id)
		return nil
	}

	// Check stress test workers
	stressWorker, exists := cli.stressWorkers[id]
	if exists {
		stressWorker.cancel()
		delete(cli.stressWorkers, id)
		return nil
	}

	return fmt.Errorf("worker with ID %s not found", id)
}

// StopAllWorkers stops all running workers
func (cli *CLI) StopAllWorkers() error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	for id, worker := range cli.workers {
		worker.cancel()
		delete(cli.workers, id)
	}

	for id, stressWorker := range cli.stressWorkers {
		stressWorker.cancel()
		delete(cli.stressWorkers, id)
	}
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
		stressWorkerStats := map[string]interface{}{
			"id":                   id,
			"name":                 stressWorker.name,
			"type":                 "stress_test",
			"target_tps":           stressWorker.targetTps,
			"current_tps":          stressWorker.currentTps,
			"actual_tps":           stressWorker.actualTps,
			"ramp_up_progress":     stressWorker.rampUpProgress,
			"ramp_up_duration":     stressWorker.rampUpDuration.String(),
			"duration":             stressWorker.duration.String(),
			"workers":              stressWorker.numWorkers,
			"runtime":              time.Since(stressWorker.startTime).Round(time.Second).String(),
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
