// stress_run.go is the driving loop of a stress run: how a sender is picked and
// paced to the target TPS, and how each outcome is folded into the worker's
// counters. Starting, stopping and the summary live in the files that own those
// questions; nothing here answers them.
package app

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"jiso/internal/app/events"
	"jiso/internal/service"
)

// runStressTest implements the stress testing logic with TPS ramp-up.
func (a *App) runStressTest(w *stressWorker, sender WorkerSender) {
	defer w.wg.Done()

	svc := a.Service()

	// Temporarily disable debugMode during stress test to silence
	// SENDING MESSAGE / raw payloads.
	if restoreDebug := a.silenceServiceDebug(svc); restoreDebug != nil {
		defer restoreDebug()
	}

	w.mu.Lock()
	w.startTime = time.Now()
	w.lastSampleTime = w.startTime
	w.mu.Unlock()

	sender.StartClock()

	// Ramp-up schedule: start at 1 TPS, 10 linear steps to target TPS.
	const rampUpSteps = 10
	startTps := 1.0
	stepDuration := w.rampUpDuration / time.Duration(rampUpSteps)
	tpsIncrement := float64(w.targetTps-1) / float64(rampUpSteps)

	// Initial worker-specific interval for step 0:
	// workerInterval = globalInterval * numWorkers
	initialInterval := time.Duration(float64(time.Second)/startTps) * time.Duration(w.numWorkers)
	if initialInterval < time.Millisecond {
		initialInterval = time.Millisecond
	}
	w.mu.Lock()
	w.currentInterval = initialInterval
	w.mu.Unlock()

	// Parallel senders: each keeps its own next-send schedule and reads
	// the current interval (updated per ramp step) under the lock.
	var workersWg sync.WaitGroup
	for i := 0; i < w.numWorkers; i++ {
		workersWg.Add(1)
		go a.runStressSender(w, sender, &workersWg, i)
	}

	// Main controller loop: progress through ramp-up steps
	for step := 0; step <= rampUpSteps; step++ {
		select {
		case <-w.ctx.Done():
			a.finishStressOnCancel(w, &workersWg)

			return
		default:
		}

		if stop := a.runRampStep(w, &workersWg, step, startTps, tpsIncrement, stepDuration, rampUpSteps); stop {
			return
		}
	}

	// Ramp-up complete, continue at target TPS for the specified duration
	a.finishStressRun(w, &workersWg)
}

// recordStressCompletion folds one ExecuteBackground outcome into the
// worker state under the lock and applies the WorkerProgress throttle
// documented on progressInterval.
func (a *App) recordStressCompletion(w *stressWorker, txName, rcStr string, execTime time.Duration, err error) {
	w.mu.Lock()

	// OFFLINE with a nil error is a skipped send (connection dropped
	// mid-run). Count it in the response-code distribution for
	// visibility, but never as a success and never with a 0 ms latency
	// sample, so summaries don't inflate the success rate.
	if err == nil && rcStr == "OFFLINE" {
		if w.respCodes == nil {
			w.respCodes = make(map[string]int)
		}
		w.respCodes[rcStr]++
		w.mu.Unlock()
		return
	}

	if err == nil {
		w.successful++
		w.consecutiveFailures = 0
	} else {
		w.failed++
		w.consecutiveFailures++
	}

	// Record the aggregate metrics.
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

	// Record per-transaction metrics.
	if txName != "" {
		w.recordTxStats(txName, rcStr, execTime, err == nil)
	}

	total := w.successful + w.failed

	// Instant TPS: EMA sample over the interval since the previous
	// sample (replaces the legacy 200 ms status goroutine math).
	now := time.Now()
	w.updateInstantTPS(now, total)

	// Circuit breaker: record the trip and stop all senders.
	tripped := false
	if w.consecutiveFailures >= CircuitBreakerFailures && !w.completed {
		tripped = true
		w.stopReason = fmt.Sprintf("failed: %d consecutive failures", w.consecutiveFailures)
		if w.networkStats != nil {
			w.networkStats.RecordCircuitBreakerTrip()
		} else if stats := a.NetworkingStats(); stats != nil {
			stats.RecordCircuitBreakerTrip()
		}
	}

	shouldPublish := w.throttle.note(now)
	successful, failed, instantTps := w.successful, w.failed, w.instantTps
	w.mu.Unlock()

	if shouldPublish && total > 0 {
		a.events.Publish(events.WorkerProgress{
			ID:    w.id,
			Done:  total,
			Total: 0,
			Note:  fmt.Sprintf("%.1f tps ok=%d err=%d", instantTps, successful, failed),
		})
	}

	if tripped {
		w.cancel() // Stop all senders by cancelling the shared context
	}
}

// runStressSender is one stress sender goroutine: it staggers its start, then
// picks a transaction name at random and executes it on schedule (reading the
// current interval, which the ramp controller updates per step) until the
// worker's context is cancelled.
func (a *App) runStressSender(w *stressWorker, sender WorkerSender, workersWg *sync.WaitGroup, workerIndex int) {
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

			rcStr, execTime, err := sender.ExecuteBackground(txName, true, w.sessionID)
			a.recordStressCompletion(w, txName, rcStr, execTime, err)
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
}

// finishStressOnCancel cancels the worker, waits for its sender and request
// goroutines to drain, and closes it out.
func (a *App) finishStressOnCancel(w *stressWorker, workersWg *sync.WaitGroup) {
	w.cancel()
	workersWg.Wait()
	w.requestsWg.Wait()
	a.finishStressWorker(w)
}

// runRampStep advances one ramp-up step: it recomputes the target TPS and the
// per-worker interval, waits out the step (or the context), then records the
// actual TPS observed during it. It reports whether the worker was cancelled.
func (a *App) runRampStep(w *stressWorker, workersWg *sync.WaitGroup, step int, startTps, tpsIncrement float64, stepDuration time.Duration, rampUpSteps int) bool {
	currentTargetTps := startTps + (float64(step) * tpsIncrement)
	if currentTargetTps > float64(w.targetTps) {
		currentTargetTps = float64(w.targetTps)
	}

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

	select {
	case <-w.ctx.Done():
		a.finishStressOnCancel(w, workersWg)

		return true
	case <-time.After(stepDuration):
	}

	if stepDurationActual := time.Since(stepStart); stepDurationActual > 0 {
		w.mu.Lock()
		successfulInThisStep := w.successful - successfulAtStepStart
		w.actualTps = float64(successfulInThisStep) / stepDurationActual.Seconds()
		w.mu.Unlock()
	}

	return false
}

// silenceServiceDebug turns off the service's debug logging for the duration of
// a stress test and returns a restore func; it returns nil when there is no
// service to silence.
func (a *App) silenceServiceDebug(svc *service.Service) func() {
	if svc == nil {
		return nil
	}
	a.svcKnobsMu.Lock()
	orig := svc.GetDebugMode()
	svc.SetDebugMode(false)
	a.svcKnobsMu.Unlock()

	return func() {
		a.svcKnobsMu.Lock()
		defer a.svcKnobsMu.Unlock()
		svc.SetDebugMode(orig)
	}
}

// finishStressRun runs the final target-TPS phase: it sets the target interval,
// waits out the remaining duration, then cancels and drains the senders.
func (a *App) finishStressRun(w *stressWorker, workersWg *sync.WaitGroup) {
	finalInterval := time.Duration(float64(time.Second)/float64(w.targetTps)) * time.Duration(w.numWorkers)
	if finalInterval < time.Millisecond {
		finalInterval = time.Millisecond
	}

	w.mu.Lock()
	w.currentInterval = finalInterval
	w.mu.Unlock()

	select {
	case <-w.ctx.Done():
	case <-time.After(w.duration):
	}

	a.finishStressOnCancel(w, workersWg)
}

// recordTxStats folds one completion into the named transaction's stats. The
// worker lock must be held by the caller.
func (w *stressWorker) recordTxStats(txName, rcStr string, execTime time.Duration, success bool) {
	ts, exists := w.txStats[txName]
	if !exists {
		return
	}
	if success {
		ts.successful++
	} else {
		ts.failed++
	}
	ts.respCodes[rcStr]++
	ts.latencies = append(ts.latencies, execTime)
}

// updateInstantTPS advances the EMA-smoothed instant TPS over the interval
// since the previous sample and tracks the peak. The worker lock must be held.
func (w *stressWorker) updateInstantTPS(now time.Time, total int) {
	elapsed := now.Sub(w.lastSampleTime).Seconds()
	if elapsed <= 0 {
		return
	}
	delta := total - w.lastTotal
	instTPS := float64(delta) / elapsed
	if w.smoothTps == 0 {
		w.smoothTps = instTPS
	} else {
		w.smoothTps = 0.3*instTPS + 0.7*w.smoothTps
	}
	w.lastTotal = total
	w.lastSampleTime = now
	if w.smoothTps > w.peakInstantTps {
		w.peakInstantTps = w.smoothTps
	}
	w.instantTps = w.smoothTps
}
