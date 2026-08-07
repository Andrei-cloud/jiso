package cli

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"jiso/internal/command"
	"jiso/internal/config"
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

func (w *stressTestWorker) printSummary(finalTps float64) {
	w.mu.Lock()
	total := w.successful + w.failed
	latenciesCopy := make([]time.Duration, len(w.latencies))
	copy(latenciesCopy, w.latencies)
	respCodesCopy := make(map[string]int, len(w.respCodes))
	for k, v := range w.respCodes {
		respCodesCopy[k] = v
	}
	// Copy txStats
	txStatsCopy := make(map[string]*txStats)
	for k, v := range w.txStats {
		ts := &txStats{
			successful: v.successful,
			failed:     v.failed,
			respCodes:  make(map[string]int),
			latencies:  make([]time.Duration, len(v.latencies)),
		}
		for rk, rv := range v.respCodes {
			ts.respCodes[rk] = rv
		}
		copy(ts.latencies, v.latencies)
		txStatsCopy[k] = ts
	}
	startTimeCopy := w.startTime
	endTimeCopy := w.endTime
	namesCopy := make([]string, len(w.names))
	copy(namesCopy, w.names)
	peakInstantTpsCopy := w.peakInstantTps
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
		switch {
		case d <= timeout/2:
			satisfactory++
		case d <= timeout:
			tolerable++
		default:
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
	fmt.Printf("Session ID:             %s\n", w.sessionID)
	fmt.Printf("Start Time:             %s\n", startTimeCopy.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("End Time:               %s\n", endTimeCopy.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Selected Transactions:  %s\n", strings.Join(namesCopy, ", "))
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("ALL TESTING SUMMARY")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Target TPS:             %-10d Concurrency (Workers): %-10d\n", w.targetTps, w.numWorkers)
	fmt.Printf("Instant TPS (Peak):     %-10.1f Average TPS:           %-10.1f\n", peakInstantTpsCopy, finalTps)
	fmt.Printf("Total Test Duration:    %-10s\n", w.duration)
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
	fmt.Printf(
		"  Min Latency:          %-15s Median (p50):          %-15s\n",
		minLatency.Round(time.Microsecond),
		p50.Round(time.Microsecond),
	)
	fmt.Printf(
		"  Max Latency:          %-15s p90 Percentile:        %-15s\n",
		maxLatency.Round(time.Microsecond),
		p90.Round(time.Microsecond),
	)
	fmt.Printf(
		"  Mean Latency:         %-15s p95 Percentile:        %-15s\n",
		meanLatency.Round(time.Microsecond),
		p95.Round(time.Microsecond),
	)
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
	fmt.Println("                    PER TRANSACTION TYPE DETAILS")
	fmt.Println("================================================================================")
	for _, name := range namesCopy {
		ts := txStatsCopy[name]
		var tsTotal, tsSuccessful, tsFailed int
		var tsRespCodes map[string]int
		var tsLatencies []time.Duration
		if ts != nil {
			tsSuccessful = ts.successful
			tsFailed = ts.failed
			tsTotal = tsSuccessful + tsFailed
			tsRespCodes = ts.respCodes
			tsLatencies = ts.latencies
		} else {
			tsRespCodes = make(map[string]int)
		}

		fmt.Printf("Transaction: %s\n", name)
		fmt.Printf("  Total Executions:     %-10d\n", tsTotal)
		if tsTotal > 0 {
			fmt.Printf("  Successful:           %-10d (%6.2f%%)\n", tsSuccessful, float64(tsSuccessful)/float64(tsTotal)*100.0)
			fmt.Printf("  Failed:               %-10d (%6.2f%%)\n", tsFailed, float64(tsFailed)/float64(tsTotal)*100.0)

			// Response code breakdown
			fmt.Printf("  Response Code Breakdown:\n")
			if count, ok := tsRespCodes["00"]; ok {
				fmt.Printf("    Code %-14s %-10d (%6.2f%%)\n", `"00":`, count, float64(count)/float64(tsTotal)*100.0)
			}
			var sortedCodes []string
			for code := range tsRespCodes {
				if code != "00" {
					sortedCodes = append(sortedCodes, code)
				}
			}
			sort.Strings(sortedCodes)
			for _, code := range sortedCodes {
				count := tsRespCodes[code]
				fmt.Printf("    Code %-14s %-10d (%6.2f%%)\n", `"`+code+`":`, count, float64(count)/float64(tsTotal)*100.0)
			}

			// Latency Profile
			sort.Slice(tsLatencies, func(i, j int) bool {
				return tsLatencies[i] < tsLatencies[j]
			})
			var tsMin, tsMax, tsMean, tsP50, tsP90, tsP95, tsP99 time.Duration
			var tsTotalDuration time.Duration
			if len(tsLatencies) > 0 {
				tsMin = tsLatencies[0]
				tsMax = tsLatencies[len(tsLatencies)-1]
				for _, d := range tsLatencies {
					tsTotalDuration += d
				}
				tsMean = tsTotalDuration / time.Duration(len(tsLatencies))
				tsP50 = percentile(tsLatencies, 0.50)
				tsP90 = percentile(tsLatencies, 0.90)
				tsP95 = percentile(tsLatencies, 0.95)
				tsP99 = percentile(tsLatencies, 0.99)
			}
			fmt.Printf("  Latency Profile:\n")
			fmt.Printf(
				"    Min Latency:        %-15s Median (p50):          %-15s\n",
				tsMin.Round(time.Microsecond),
				tsP50.Round(time.Microsecond),
			)
			fmt.Printf(
				"    Max Latency:        %-15s p90 Percentile:        %-15s\n",
				tsMax.Round(time.Microsecond),
				tsP90.Round(time.Microsecond),
			)
			fmt.Printf(
				"    Mean Latency:       %-15s p95 Percentile:        %-15s\n",
				tsMean.Round(time.Microsecond),
				tsP95.Round(time.Microsecond),
			)
			fmt.Printf("                                         p99 Percentile:        %-15s\n", tsP99.Round(time.Microsecond))
		} else {
			fmt.Printf("  Successful:           0          (  0.00%%\n")
			fmt.Printf("  Failed:               0          (  0.00%%\n")
		}
		fmt.Println("--------------------------------------------------------------------------------")
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
