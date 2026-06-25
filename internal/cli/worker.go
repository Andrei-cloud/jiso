package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"jiso/internal/command"
	"jiso/internal/config"
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
	currentInterval     time.Duration
	successful          int
	failed              int
	consecutiveFailures int
	latencies           []time.Duration
	respCodes           map[string]int
	completed           bool
	endTime             time.Time
	mu                  sync.Mutex
	wg                  sync.WaitGroup // WaitGroup to ensure clean shutdown
	requestsWg          sync.WaitGroup // WaitGroup to track async requests
	originalMaxPending  int            // Store the original max pending requests to restore it later
}

// runStressTest implements the stress testing logic with TPS ramp-up
func (w *stressTestWorker) runStressTest(cli *CLI) {
	sendCmd, ok := cli.commands["send"].(*command.SendCommand)
	if !ok {
		fmt.Printf("Error: send command not found or has wrong type\n")
		return
	}

	sendCmd.StartClock()

	w.mu.Lock()
	w.startTime = time.Now()
	w.mu.Unlock()

	// Start status printing goroutine
	statusCtx, statusCancel := context.WithCancel(w.ctx)
	defer statusCancel()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-statusCtx.Done():
				return
			case <-ticker.C:
				w.mu.Lock()
				successful := w.successful
				failed := w.failed
				total := successful + failed
				currentTps := w.currentTps
				startTime := w.startTime
				completed := w.completed
				rampUpDuration := w.rampUpDuration
				duration := w.duration
				w.mu.Unlock()

				if completed {
					return
				}

				elapsed := time.Since(startTime)
				var tps float64
				if elapsed.Seconds() > 0 {
					tps = float64(total) / elapsed.Seconds()
				}

				var timeStr string
				var phase string
				if elapsed < rampUpDuration {
					phase = "Ramp-up"
					timeStr = fmt.Sprintf("%s/%s", formatDuration(elapsed), formatDuration(rampUpDuration))
				} else {
					phase = "Maintain"
					maintainElapsed := elapsed - rampUpDuration
					timeStr = fmt.Sprintf("%s/%s", formatDuration(maintainElapsed), formatDuration(duration))
				}

				fmt.Printf("\r[STEST] Phase: %-8s | Time: %s | Sent: %d (OK:%d, Err:%d) | TPS: %.1f (Target: %.1f)\033[K",
					phase,
					timeStr,
					total,
					successful,
					failed,
					tps,
					currentTps,
				)
			}
		}
	}()

	// Start with 1 TPS and ramp up to target TPS
	startTps := 1.0
	rampUpSteps := 10 // Number of ramp-up steps
	stepDuration := w.rampUpDuration / time.Duration(rampUpSteps)

	tpsIncrement := float64(w.targetTps-1) / float64(rampUpSteps)

	fmt.Printf("Stress test worker %s starting ramp-up to %d TPS over %s\n",
		w.id, w.targetTps, w.rampUpDuration)

	// Calculate initial worker-specific interval for step 0
	// workerInterval = globalInterval * numWorkers
	initialInterval := time.Duration(float64(time.Second) / startTps) * time.Duration(w.numWorkers)
	if initialInterval < time.Millisecond {
		initialInterval = time.Millisecond
	}
	w.mu.Lock()
	w.currentInterval = initialInterval
	w.mu.Unlock()

	// Start parallel workers
	var workersWg sync.WaitGroup
	for i := 0; i < w.numWorkers; i++ {
		workersWg.Add(1)
		go func(workerIndex int) {
			defer workersWg.Done()

			// Get initial interval under lock
			w.mu.Lock()
			interval := w.currentInterval
			w.mu.Unlock()

			// Stagger startup to distribute requests evenly
			globalInterval := interval / time.Duration(w.numWorkers)
			if globalInterval < 1 {
				globalInterval = 1
			}
			staggerDelay := time.Duration(workerIndex) * globalInterval
			select {
			case <-w.ctx.Done():
				return
			case <-time.After(staggerDelay):
			}

			nextSend := time.Now()

			for {
				select {
				case <-w.ctx.Done():
					return
				default:
				}

				// Execute transaction asynchronously to avoid blocking the sender loop
				w.requestsWg.Add(1)
				go func() {
					defer w.requestsWg.Done()

					rcStr, execTime, err := sendCmd.ExecuteBackground(w.name)

					w.mu.Lock()
					if err == nil {
						w.successful++
						w.consecutiveFailures = 0
					} else {
						w.failed++
						w.consecutiveFailures++
					}

					// Record the metrics in w
					if w.respCodes == nil {
						w.respCodes = make(map[string]int)
					}
					if rcStr == "" {
						if err != nil {
							rcStr = "ERROR"
						} else {
							rcStr = "00"
						}
					}
					w.respCodes[rcStr]++
					w.latencies = append(w.latencies, execTime)

					// Circuit breaker: record trip if activated
					if w.consecutiveFailures >= 10 {
						if w.networkStats != nil {
							w.networkStats.RecordCircuitBreakerTrip()
						}
						fmt.Printf(
							"\nStress test worker %s stopped due to %d consecutive failures\n",
							w.id,
							w.consecutiveFailures,
						)
						w.cancel() // Stop all other workers by cancelling the context
					}
					w.mu.Unlock()
				}()

				w.mu.Lock()
				interval = w.currentInterval
				w.mu.Unlock()

				// Sleep until the next scheduled send time for this worker
				nextSend = nextSend.Add(interval)
				if time.Now().After(nextSend) {
					// Lagged behind. Reset nextSend to now.
					nextSend = time.Now()
				} else {
					select {
					case <-w.ctx.Done():
						return
					case <-time.After(time.Until(nextSend)):
					}
				}
			}
		}(i)
	}

	// Main controller loop: Progress through ramp-up steps
	for step := 0; step <= rampUpSteps; step++ {
		select {
		case <-w.ctx.Done():
			// Context canceled, cleanup and return
			w.cancel()
			workersWg.Wait()
			w.requestsWg.Wait()
			w.finishAndPrintSummary(cli)
			return
		default:
		}

		// Calculate current target TPS for this step
		currentTargetTps := startTps + (float64(step) * tpsIncrement)
		if currentTargetTps > float64(w.targetTps) {
			currentTargetTps = float64(w.targetTps)
		}

		// Calculate worker interval for this TPS
		globalInterval := time.Duration(float64(time.Second) / currentTargetTps)
		workerInterval := globalInterval * time.Duration(w.numWorkers)
		if workerInterval < time.Millisecond {
			workerInterval = time.Millisecond
		}

		w.mu.Lock()
		w.currentTps = currentTargetTps
		w.currentInterval = workerInterval
		w.rampUpProgress = float64(step) / float64(rampUpSteps) * 100.0
		w.mu.Unlock()

		stepStart := time.Now()
		w.mu.Lock()
		successfulAtStepStart := w.successful
		w.mu.Unlock()

		// Wait for the duration of this step
		select {
		case <-w.ctx.Done():
			w.cancel()
			workersWg.Wait()
			w.requestsWg.Wait()
			w.finishAndPrintSummary(cli)
			return
		case <-time.After(stepDuration):
		}

		// Calculate actual TPS for this step
		stepDurationActual := time.Since(stepStart)
		if stepDurationActual > 0 {
			w.mu.Lock()
			successfulInThisStep := w.successful - successfulAtStepStart
			actualTps := float64(successfulInThisStep) / stepDurationActual.Seconds()
			w.actualTps = actualTps
			w.mu.Unlock()
		}
	}

	// Ramp-up complete, continue at target TPS for the specified duration
	fmt.Printf(
		"\nWorker %s: Ramp-up complete. Maintaining %d TPS for %s\n",
		w.id,
		w.targetTps,
		w.duration,
	)

	finalInterval := time.Duration(float64(time.Second)/float64(w.targetTps)) * time.Duration(w.numWorkers)
	if finalInterval < time.Millisecond {
		finalInterval = time.Millisecond
	}

	w.mu.Lock()
	w.currentInterval = finalInterval
	w.mu.Unlock()

	// Wait for the final test phase duration
	select {
	case <-w.ctx.Done():
	case <-time.After(w.duration):
	}

	// Cancel context to stop all worker goroutines
	w.cancel()

	// Wait for all worker goroutines to exit cleanly
	workersWg.Wait()

	// Wait for all outstanding request goroutines to finish
	w.requestsWg.Wait()

	// Finish and print summary
	w.finishAndPrintSummary(cli)

	fmt.Printf("Worker %s: Test duration elapsed. Stopping.\n", w.id)
}

func (w *stressTestWorker) finishAndPrintSummary(cli *CLI) {
	w.mu.Lock()
	if w.completed {
		w.mu.Unlock()
		return
	}
	w.completed = true
	w.endTime = time.Now()
	w.currentTps = 0.0

	// Calculate actual overall TPS based on entire run duration
	durationActual := w.endTime.Sub(w.startTime)
	if durationActual > 0 {
		w.actualTps = float64(w.successful) / durationActual.Seconds()
	}
	w.rampUpProgress = 100.0
	w.mu.Unlock()

	// Restore original max pending requests
	if cli != nil && cli.svc != nil {
		cli.svc.SetMaxPendingRequests(w.originalMaxPending)
	}

	w.printSummary(w.actualTps)
}

func (w *stressTestWorker) printSummary(finalTps float64) {
	w.mu.Lock()
	total := w.successful + w.failed
	latenciesCopy := make([]time.Duration, len(w.latencies))
	copy(latenciesCopy, w.latencies)
	respCodesCopy := make(map[string]int, len(w.respCodes))
	for k, v := range w.respCodes {
		respCodesCopy[k] = v
	}
	w.mu.Unlock()

	if total == 0 {
		fmt.Printf("\nWorker %s: Stress test completed but no transactions were executed.\n", w.id)
		return
	}

	// Sort latencies to calculate percentiles
	sort.Slice(latenciesCopy, func(i, j int) bool {
		return latenciesCopy[i] < latenciesCopy[j]
	})

	var minLatency, maxLatency, meanLatency, p50, p90, p95, p99 time.Duration
	var totalDuration time.Duration

	if len(latenciesCopy) > 0 {
		minLatency = latenciesCopy[0]
		maxLatency = latenciesCopy[len(latenciesCopy)-1]
		for _, d := range latenciesCopy {
			totalDuration += d
		}
		meanLatency = totalDuration / time.Duration(len(latenciesCopy))

		p50 = percentile(latenciesCopy, 0.50)
		p90 = percentile(latenciesCopy, 0.90)
		p95 = percentile(latenciesCopy, 0.95)
		p99 = percentile(latenciesCopy, 0.99)
	}

	// Get response timeout budget
	timeout := config.GetConfig().GetResponseTimeout()

	// Latency budgets
	var satisfactory, tolerable, exceeded int
	for _, d := range latenciesCopy {
		if d <= timeout/2 {
			satisfactory++
		} else if d <= timeout {
			tolerable++
		} else {
			exceeded++
		}
	}

	// Build histogram
	type bucket struct {
		label string
		min   time.Duration
		max   time.Duration
		count int
	}
	buckets := []bucket{
		{label: "  0ms -  10ms", min: 0, max: 10 * time.Millisecond},
		{label: " 10ms -  50ms", min: 10 * time.Millisecond, max: 50 * time.Millisecond},
		{label: " 50ms - 100ms", min: 50 * time.Millisecond, max: 100 * time.Millisecond},
		{label: "100ms - 250ms", min: 100 * time.Millisecond, max: 250 * time.Millisecond},
		{label: "250ms - 500ms", min: 250 * time.Millisecond, max: 500 * time.Millisecond},
		{label: "500ms - 1.0s ", min: 500 * time.Millisecond, max: 1000 * time.Millisecond},
		{label: " 1.0s - 2.5s ", min: 1000 * time.Millisecond, max: 2500 * time.Millisecond},
		{label: " 2.5s - 5.0s ", min: 2500 * time.Millisecond, max: 5000 * time.Millisecond},
		{label: "    > 5.0s   ", min: 5000 * time.Millisecond, max: 999999 * time.Hour},
	}

	for _, d := range latenciesCopy {
		for i := range buckets {
			if d > buckets[i].min && d <= buckets[i].max {
				buckets[i].count++
				break
			}
		}
	}

	// Print the output
	fmt.Println("\n================================================================================")
	fmt.Printf("                          STRESS TEST SUMMARY - Worker %s\n", w.id)
	fmt.Println("================================================================================")
	fmt.Printf("Target TPS:             %-10d Concurrency (Workers): %-10d\n", w.targetTps, w.numWorkers)
	fmt.Printf("Actual TPS:             %-10.1f Total Test Duration:   %-10s\n", finalTps, w.duration)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Transaction Counts:\n")
	fmt.Printf("  Total Executions:     %-10d\n", total)
	fmt.Printf("  Successful:           %-10d (%6.2f%%)\n", w.successful, float64(w.successful)/float64(total)*100.0)
	fmt.Printf("  Failed:               %-10d (%6.2f%%)\n", w.failed, float64(w.failed)/float64(total)*100.0)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Response Code Breakdown:\n")
	// For predictable ordering, print success code "00" first if present, then others
	if count, ok := respCodesCopy["00"]; ok {
		fmt.Printf("  Code %-16s %-10d (%6.2f%%)\n", `"00":`, count, float64(count)/float64(total)*100.0)
	}
	for code, count := range respCodesCopy {
		if code == "00" {
			continue
		}
		fmt.Printf("  Code %-16s %-10d (%6.2f%%)\n", `"`+code+`":`, count, float64(count)/float64(total)*100.0)
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Profile:\n")
	fmt.Printf("  Min Latency:          %-15s Median (p50):          %-15s\n", minLatency.Round(time.Microsecond), p50.Round(time.Microsecond))
	fmt.Printf("  Max Latency:          %-15s p90 Percentile:        %-15s\n", maxLatency.Round(time.Microsecond), p90.Round(time.Microsecond))
	fmt.Printf("  Mean Latency:         %-15s p95 Percentile:        %-15s\n", meanLatency.Round(time.Microsecond), p95.Round(time.Microsecond))
	fmt.Printf("                                       p99 Percentile:        %-15s\n", p99.Round(time.Microsecond))
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Budget (Timeout: %s):\n", timeout)
	fmt.Printf("  Satisfactory (<= 50%% of timeout):  %-10d (%6.2f%%)\n", satisfactory, float64(satisfactory)/float64(total)*100.0)
	fmt.Printf("  Tolerable    (51%%-100%% of timeout): %-10d (%6.2f%%)\n", tolerable, float64(tolerable)/float64(total)*100.0)
	fmt.Printf("  Exceeded     (> 100%% of timeout):   %-10d (%6.2f%%)\n", exceeded, float64(exceeded)/float64(total)*100.0)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Histogram:\n")

	// Find max count to scale the bar
	maxCount := 0
	for _, b := range buckets {
		if b.count > maxCount {
			maxCount = b.count
		}
	}

	for _, b := range buckets {
		if b.count == 0 {
			continue // skip empty buckets to reduce noise
		}
		barLength := 0
		if maxCount > 0 {
			barLength = (b.count * 30) / maxCount
		}
		bar := strings.Repeat("█", barLength)
		fmt.Printf("  [%s]: %-30s %-10d (%6.2f%%)\n", b.label, bar, b.count, float64(b.count)/float64(total)*100.0)
	}
	fmt.Println("================================================================================")
}

func percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if pct <= 0.0 {
		return sorted[0]
	}
	if pct >= 1.0 {
		return sorted[len(sorted)-1]
	}
	idx := float64(len(sorted)-1) * pct
	low := int(math.Floor(idx))
	high := int(math.Ceil(idx))
	if low == high {
		return sorted[low]
	}
	diff := idx - float64(low)
	return time.Duration(float64(sorted[low]) + diff*float64(sorted[high]-sorted[low]))
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
					_, _, err := sendCmd.ExecuteBackground(name)
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

	// Create a new stress test worker state
	worker := &stressTestWorker{
		id:                 workerID,
		name:               name,
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
		respCodes:          make(map[string]int),
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
			"name":                 stressWorker.name,
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

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
