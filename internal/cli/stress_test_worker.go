package cli

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"jiso/internal/command"
	"jiso/internal/metrics"
)

// txStats holds the stats for an individual transaction type.
type txStats struct {
	successful int
	failed     int
	respCodes  map[string]int
	latencies  []time.Duration
}

// stressTestWorker holds the state of a stress test worker.
type stressTestWorker struct {
	id                  string
	sessionID           string
	names               []string
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
	instantTps          float64
	peakInstantTps      float64
	rampUpProgress      float64
	currentInterval     time.Duration
	successful          int
	failed              int
	consecutiveFailures int
	latencies           []time.Duration
	respCodes           map[string]int
	txStats             map[string]*txStats
	completed           bool
	endTime             time.Time
	mu                  sync.Mutex
	wg                  sync.WaitGroup // WaitGroup to ensure clean shutdown
	requestsWg          sync.WaitGroup // WaitGroup to track async requests
	originalMaxPending  int            // Store the original max pending requests to restore it later
}

// runStressTest implements the stress testing logic with TPS ramp-up.
func (w *stressTestWorker) runStressTest(cli *CLI) {
	sendCmd, ok := cli.commands["send"].(*command.SendCommand)
	if !ok {
		fmt.Printf("Error: send command not found or has wrong type\n")
		return
	}

	sendCmd.StartClock()

	// Temporarily disable debugMode during stress test to silence SENDING MESSAGE / raw payloads
	var origDebugMode bool
	if cli != nil && cli.svc != nil {
		origDebugMode = cli.svc.GetDebugMode()
		cli.svc.SetDebugMode(false)
		defer func() {
			cli.svc.SetDebugMode(origDebugMode)
		}()
	}

	w.mu.Lock()
	w.startTime = time.Now()
	w.mu.Unlock()

	// Start status printing goroutine
	statusCtx, statusCancel := context.WithCancel(w.ctx)
	defer statusCancel()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		var lastTotal int
		lastTime := time.Now()
		var smoothInstantTPS float64

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

				now := time.Now()
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed > 0 {
					delta := total - lastTotal
					instTPS := float64(delta) / elapsed
					if smoothInstantTPS == 0 {
						smoothInstantTPS = instTPS
					} else {
						smoothInstantTPS = 0.3*instTPS + 0.7*smoothInstantTPS
					}
					lastTotal = total
					lastTime = now

					w.mu.Lock()
					w.instantTps = smoothInstantTPS
					if smoothInstantTPS > w.peakInstantTps {
						w.peakInstantTps = smoothInstantTPS
					}
					w.mu.Unlock()
				}

				totalElapsed := now.Sub(startTime)
				var avgTps float64
				if totalElapsed.Seconds() > 0 {
					avgTps = float64(successful) / totalElapsed.Seconds()
				}

				phase := "RAMP-UP"
				timeStr := fmt.Sprintf("%s/%s", formatDuration(totalElapsed), formatDuration(rampUpDuration))
				if totalElapsed >= rampUpDuration {
					phase = "TESTING"
					maintainElapsed := totalElapsed - rampUpDuration
					timeStr = fmt.Sprintf("%s/%s", formatDuration(maintainElapsed), formatDuration(duration))
				}

				fmt.Printf(
					"\r[STEST] Phase: %-8s | Time: %s | Sent: %d (OK:%d, Err:%d) | Instant TPS: %.1f | Avg TPS: %.1f (Target: %.1f)\033[K",
					phase, timeStr, total, successful, failed, smoothInstantTPS, avgTps, currentTps,
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
	initialInterval := time.Duration(float64(time.Second)/startTps) * time.Duration(w.numWorkers)
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

			// Local random source to avoid global lock contention in math/rand
			localSource := rand.NewSource(time.Now().UnixNano() + int64(workerIndex))
			r := rand.New(localSource)

			nextSend := time.Now()

			for {
				select {
				case <-w.ctx.Done():
					return
				default:
				}

				var name string
				if len(w.names) > 0 {
					name = w.names[r.Intn(len(w.names))]
				}

				// Execute transaction asynchronously to avoid blocking the sender loop
				w.requestsWg.Add(1)
				go func(txName string) {
					defer w.requestsWg.Done()

					rcStr, execTime, err := sendCmd.ExecuteBackground(txName, true, w.sessionID)

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

					// Record per-transaction metrics
					if txName != "" {
						if ts, exists := w.txStats[txName]; exists {
							if err == nil {
								ts.successful++
							} else {
								ts.failed++
							}
							ts.respCodes[rcStr]++
							ts.latencies = append(ts.latencies, execTime)
						}
					}

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
						w.cancel() // Stop all other workers by canceling the context
					}
					w.mu.Unlock()
				}(name)

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
