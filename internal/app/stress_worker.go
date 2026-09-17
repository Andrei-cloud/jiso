package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"jiso/internal/app/events"
	"jiso/internal/metrics"
	"jiso/internal/service"
)

// txStats holds the stats for an individual transaction type.
type txStats struct {
	successful int
	failed     int
	respCodes  map[string]int
	latencies  []time.Duration
}

// stressWorker holds the state of one stress-test worker: TPS ramp-up
// schedule, counters, per-transaction stats and the final summary. All
// observable output goes through the App event bus; terminal rendering
// belongs to the frontends.
type stressWorker struct {
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
	stopReason          string
	endTime             time.Time
	mu                  sync.Mutex
	wg                  sync.WaitGroup // WaitGroup to ensure clean shutdown
	requestsWg          sync.WaitGroup // WaitGroup to track async requests
	originalMaxPending  int            // original max pending requests to restore at finish

	// Progress/TPS bookkeeping, updated at each throttled publish
	// (replaces the legacy 200 ms status-printing goroutine).
	throttle       progressThrottle
	lastTotal      int
	lastSampleTime time.Time
	smoothTps      float64
}

// StressStart launches a stress-test worker that ramps from 1 TPS to
// targetTps over rampUpDuration (10 steps), holds targetTps for duration,
// and finishes. numWorkers concurrent senders execute the selected
// transactions asynchronously. It returns the worker ID.
//
// Events on App.Events: WorkerStarted{ID, "stress"} at launch,
// throttled WorkerProgress per completion batch (see progressInterval),
// and a final WorkerStopped with reason "done", StatusStopped, or
// "failed: N consecutive failures" (circuit breaker).
func (a *App) StressStart(names []string, targetTps int, rampUpDuration, duration time.Duration, numWorkers int) (string, error) {
	if targetTps <= 0 {
		return "", errors.New("TPS must be greater than 0")
	}
	if numWorkers <= 0 {
		return "", errors.New("workers must be greater than 0")
	}
	if numWorkers > 50 {
		return "", errors.New("workers cannot exceed 50")
	}

	svc, err := a.openService()
	if err != nil {
		return "", err
	}

	sender, err := a.workerSender()
	if err != nil {
		return "", err
	}

	workerID := shortWorkerID()
	sessionID := uuid.New().String()

	ctx, cancel := context.WithCancel(context.Background())

	originalMaxPending := 100
	if svc != nil {
		originalMaxPending = a.tuneMaxPending(svc, targetTps)
	}

	txStatsMap := newTxStatsMap(targetTps, rampUpDuration, duration, names)

	w := &stressWorker{
		id:                 workerID,
		sessionID:          sessionID,
		names:              append([]string(nil), names...),
		targetTps:          targetTps,
		rampUpDuration:     rampUpDuration,
		duration:           duration,
		numWorkers:         numWorkers,
		startTime:          time.Now(),
		ctx:                ctx,
		cancel:             cancel,
		txStats:            txStatsMap,
		originalMaxPending: originalMaxPending,
		lastSampleTime:     time.Now(),
	}

	a.wmu.Lock()
	if a.isClosed() {
		a.wmu.Unlock()
		cancel()
		a.restoreMaxPending(originalMaxPending)

		return "", ErrClosed
	}
	if a.stressWorkers == nil {
		a.stressWorkers = make(map[string]*stressWorker)
	}
	a.stressWorkers[workerID] = w
	a.wmu.Unlock()

	a.events.Publish(events.WorkerStarted{ID: workerID, Kind: "stress"})

	w.wg.Add(1)
	go a.runStressTest(w, sender)

	return workerID, nil
}

// restoreMaxPending puts the connection manager's pending budget back.
func (a *App) restoreMaxPending(original int) {
	svc := a.Service()
	if svc != nil {
		a.svcKnobsMu.Lock()
		defer a.svcKnobsMu.Unlock()
		svc.SetMaxPendingRequests(original)
	}
}

// StressStop is WorkerStop restricted to stress workers; it exists for
// symmetry with StressStart. Unknown or background IDs return
// "worker '<id>' not found".
func (a *App) StressStop(id string) error {
	a.wmu.Lock()
	_, exists := a.stressWorkers[id]
	a.wmu.Unlock()

	if !exists {
		return &WorkerNotFoundError{ID: id}
	}

	return a.WorkerStop(id)
}

// stopStressWorker cancels a removed-from-list stress worker and waits
// bounded for its goroutines; its finish path publishes WorkerStopped.
func (a *App) stopStressWorker(w *stressWorker) error {
	w.mu.Lock()
	if w.stopReason == "" {
		w.stopReason = StatusStopped
	}
	w.mu.Unlock()

	w.cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		w.requestsWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Stopped cleanly
	case <-time.After(workerStopTimeout):
		// The worker was already removed from the live list; the
		// lingering goroutines finish on their own.
	}

	return nil
}

// finishStressWorker closes out the worker exactly once: it computes the
// final summary, persists the session-DB record, remembers the summary
// for post-stop retrieval, and publishes the final progress snapshot plus
// WorkerStopped. Callers must have waited for their send loops; request
// goroutines are waited here for the circuit-breaker path.
func (a *App) finishStressWorker(w *stressWorker) {
	w.mu.Lock()
	if w.completed {
		w.mu.Unlock()

		return
	}
	w.completed = true
	w.endTime = time.Now()
	w.currentTps = 0.0

	// Calculate actual overall TPS based on the entire run duration
	durationActual := w.endTime.Sub(w.startTime)
	if durationActual > 0 {
		w.actualTps = float64(w.successful) / durationActual.Seconds()
	}
	w.rampUpProgress = 100.0
	reason := w.stopReason
	if reason == "" {
		reason = "done"
		w.stopReason = reason
	}
	w.mu.Unlock()

	// Restore original max pending requests.
	a.restoreMaxPending(w.originalMaxPending)

	summary := a.buildStressSummary(w)

	a.persistStressSummary(summary)

	// Remember the final summary (bounded) so frontends can fetch it
	// after WorkerStopped.
	a.wmu.Lock()
	if a.finishedStress == nil {
		a.finishedStress = make(map[string]*StressSummary)
	}
	if len(a.finishedStress) >= maxRememberedStressRuns {
		var oldest string
		var oldestStart time.Time
		for id, s := range a.finishedStress {
			if oldest == "" || s.StartTime.Before(oldestStart) {
				oldest, oldestStart = id, s.StartTime
			}
		}
		delete(a.finishedStress, oldest)
	}
	a.finishedStress[w.id] = summary
	a.wmu.Unlock()

	total := summary.Successful + summary.Failed
	if total > 0 {
		a.events.Publish(events.WorkerProgress{
			ID:    w.id,
			Done:  total,
			Total: 0,
			Note:  fmt.Sprintf("final ok=%d err=%d", summary.Successful, summary.Failed),
		})
	}

	a.events.Publish(events.WorkerStopped{ID: w.id, Reason: reason})
}

// aggSource snapshots one stress worker's raw counters and latency
// samples for aggregation.
func (w *stressWorker) aggSource() aggSource {
	w.mu.Lock()
	defer w.mu.Unlock()

	src := aggSource{
		completed:           w.completed,
		targetTps:           w.targetTps,
		numWorkers:          w.numWorkers,
		successful:          w.successful,
		failed:              w.failed,
		consecutiveFailures: w.consecutiveFailures,
		peakInstantTps:      w.peakInstantTps,
		startTime:           w.startTime,
		endTime:             w.endTime,
		duration:            w.duration,
		rampUpDuration:      w.rampUpDuration,
		names:               append([]string(nil), w.names...),
		respCodes:           make(map[string]int, len(w.respCodes)),
		latencies:           make([]time.Duration, len(w.latencies)),
		txLatencies:         make(map[string][]time.Duration, len(w.txStats)),
	}
	for k, v := range w.respCodes {
		src.respCodes[k] = v
	}
	copy(src.latencies, w.latencies)
	for _, name := range w.names {
		ts := w.txStats[name]
		if ts == nil {
			src.txCounts = append(src.txCounts, txCounts{name: name})

			continue
		}
		cp := txCounts{name: name, successful: ts.successful, failed: ts.failed, respCodes: make(map[string]int, len(ts.respCodes))}
		for k, v := range ts.respCodes {
			cp.respCodes[k] = v
		}
		src.txCounts = append(src.txCounts, cp)
		src.txLatencies[name] = append([]time.Duration(nil), ts.latencies...)
	}

	return src
}

// runtime returns the run length used in aggregation.
func (s aggSource) runtime(now time.Time) time.Duration {
	if s.completed && !s.endTime.IsZero() {
		return s.endTime.Sub(s.startTime)
	}

	return now.Sub(s.startTime)
}

// tuneMaxPending raises the service's max pending requests so the target TPS fits
// within the response timeout (floored), returning the original budget so the
// worker can restore it later.
func (a *App) tuneMaxPending(svc *service.Service, targetTps int) int {
	a.svcKnobsMu.Lock()
	defer a.svcKnobsMu.Unlock()

	original := svc.GetMaxPendingRequests()
	timeoutSec := int(svc.GetResponseTimeout().Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	required := targetTps * timeoutSec
	if required < targetTps*2 {
		required = targetTps * 2
	}
	if required < 100 {
		required = 100
	}
	if required > original {
		svc.SetMaxPendingRequests(required)
	}

	return original
}

// newTxStatsMap pre-allocates per-transaction stats sized from the target TPS and
// total run duration, floored so short runs still get usable capacity.
func newTxStatsMap(targetTps int, rampUpDuration, duration time.Duration, names []string) map[string]*txStats {
	totalSec := int((rampUpDuration + duration).Seconds())
	if totalSec < 1 {
		totalSec = 1
	}
	expected := targetTps * totalSec
	if expected < 1000 {
		expected = 1000
	}
	perTx := expected
	if len(names) > 0 {
		perTx = expected / len(names)
	}
	if perTx < 200 {
		perTx = 200
	}

	txStatsMap := make(map[string]*txStats, len(names))
	for _, name := range names {
		txStatsMap[name] = &txStats{
			respCodes: make(map[string]int),
			latencies: make([]time.Duration, 0, perTx),
		}
	}

	return txStatsMap
}
