// stress_stats.go is the read side: the queries an operator or the TUI asks about
// stress runs in flight and in the past, plus the aggregation types they read
// through. Nothing here mutates a worker; the run loop owns that.
package app

import (
	"sort"
	"strings"
	"time"
)

// StressStats returns the aggregate summary across every stress worker
// the App currently tracks (running plus completed-but-not-yet-stopped):
// totals summed, latency percentiles over the merged samples, peak over
// workers. With no tracked workers it returns a zero-totals summary with
// Status "idle" and a nil error, so frontends can poll it unconditionally.
func (a *App) StressStats() (*StressSummary, error) {
	a.wmu.Lock()
	workers := make([]*stressWorker, 0, len(a.stressWorkers))
	for _, w := range a.stressWorkers {
		workers = append(workers, w)
	}
	a.wmu.Unlock()

	agg := &StressSummary{WorkerID: "aggregate", Name: "all stress workers", Type: TypeStressTest, Status: "idle"}
	if len(workers) == 0 {
		return agg, nil
	}

	sources := make([]aggSource, 0, len(workers))
	for _, w := range workers {
		sources = append(sources, w.aggSource())
	}
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].startTime.Before(sources[j].startTime)
	})

	acc := newStressAggregate(agg)
	now := time.Now()
	for i := range sources {
		acc.add(&sources[i], now)
	}
	acc.finalize(a.cfg.GetResponseTimeout())

	return agg, nil
}

// stressAggregate merges several stress workers' snapshots into one StressSummary:
// it folds running totals, the union of names and response codes, concatenated
// latencies, and per-transaction stats as each source is added.
type stressAggregate struct {
	agg            *StressSummary
	status         string
	nameSet        map[string]struct{}
	names          []string
	respCodes      map[string]int
	perTx          map[string]*txAgg
	txOrder        []string
	latencies      []time.Duration
	maxRuntime     time.Duration
	elapsedSeconds float64
}

func newStressAggregate(agg *StressSummary) *stressAggregate {
	return &stressAggregate{
		agg:       agg,
		status:    StatusCompleted,
		nameSet:   make(map[string]struct{}),
		names:     make([]string, 0),
		respCodes: make(map[string]int),
		perTx:     make(map[string]*txAgg),
	}
}

// add folds one worker's snapshot into the aggregate.
func (s *stressAggregate) add(src *aggSource, now time.Time) {
	if !src.completed {
		s.status = StatusRunning
	}
	runtime := src.runtime(now)

	s.agg.Sent += src.successful + src.failed
	s.agg.Successful += src.successful
	s.agg.Failed += src.failed
	s.agg.Workers += src.numWorkers
	s.agg.TargetTPS += src.targetTps
	if src.peakInstantTps > s.agg.PeakTPS {
		s.agg.PeakTPS = src.peakInstantTps
	}
	if runtime > s.maxRuntime {
		s.maxRuntime = runtime
	}
	if src.consecutiveFailures > s.agg.ConsecutiveFailures {
		s.agg.ConsecutiveFailures = src.consecutiveFailures
	}
	if src.duration > s.agg.Duration {
		s.agg.Duration = src.duration
	}
	if src.rampUpDuration > s.agg.RampUpDuration {
		s.agg.RampUpDuration = src.rampUpDuration
	}
	if s.agg.StartTime.IsZero() || src.startTime.Before(s.agg.StartTime) {
		s.agg.StartTime = src.startTime
	}
	if src.endTime.After(s.agg.EndTime) {
		s.agg.EndTime = src.endTime
	}
	s.elapsedSeconds += runtime.Seconds()

	for _, n := range src.names {
		if _, dup := s.nameSet[n]; !dup {
			s.nameSet[n] = struct{}{}
			s.names = append(s.names, n)
		}
	}
	for k, v := range src.respCodes {
		s.respCodes[k] += v
	}
	s.latencies = append(s.latencies, src.latencies...)

	s.addTxStats(src)
}

// finalize writes the merged totals, latency distribution and per-transaction
// breakdown into the aggregate's StressSummary.
func (s *stressAggregate) finalize(timeout time.Duration) {
	s.agg.Status = s.status
	s.agg.Name = joinNames(s.names)
	s.agg.TransactionNames = s.names
	s.agg.ResponseCodes = s.respCodes
	s.agg.Runtime = s.maxRuntime
	if s.elapsedSeconds > 0 {
		s.agg.ActualTPS = float64(s.agg.Successful) / s.elapsedSeconds
	}
	if len(s.latencies) > 0 {
		dist := latencyStats(s.latencies)
		s.agg.MinLatencyMs, s.agg.MeanLatencyMs, s.agg.MaxLatencyMs,
			s.agg.P50LatencyMs, s.agg.P90LatencyMs, s.agg.P95LatencyMs, s.agg.P99LatencyMs =
			dist.minMs, dist.meanMs, dist.maxMs, dist.p50, dist.p90, dist.p95, dist.p99
	}

	for _, d := range s.latencies {
		switch {
		case d <= timeout/2:
			s.agg.LatencySatisfactory++
		case d <= timeout:
			s.agg.LatencyTolerable++
		default:
			s.agg.LatencyExceeded++
		}
	}
	s.agg.LatencyBuckets = latencyHistogram(s.latencies)

	s.agg.Transactions = make([]TransactionSummary, 0, len(s.txOrder))
	for _, name := range s.txOrder {
		ts := s.perTx[name]
		dist := latencyStats(ts.latencies)
		s.agg.Transactions = append(s.agg.Transactions, TransactionSummary{
			Name:          name,
			Successful:    ts.successful,
			Failed:        ts.failed,
			ResponseCodes: ts.respCodes,
			MinLatencyMs:  dist.minMs,
			MeanLatencyMs: dist.meanMs,
			MaxLatencyMs:  dist.maxMs,
			P50LatencyMs:  dist.p50,
			P90LatencyMs:  dist.p90,
			P95LatencyMs:  dist.p95,
			P99LatencyMs:  dist.p99,
		})
	}
}

// StressSummaryByID returns the live snapshot of a tracked stress worker,
// or the remembered final summary once it finished (so frontends can
// render the summary after WorkerStopped even when the worker was
// already stopped and removed). Unknown IDs return "worker '<id>' not
// found".
func (a *App) StressSummaryByID(id string) (*StressSummary, error) {
	a.wmu.Lock()
	if w, exists := a.stressWorkers[id]; exists {
		a.wmu.Unlock()

		return a.buildStressSummary(w), nil
	}
	s := a.finishedStress[id]
	a.wmu.Unlock()

	if s == nil {
		return nil, &WorkerNotFoundError{ID: id}
	}

	return s, nil
}

// joinNames formats the transaction name list the way the CLI displays
// it.
func joinNames(names []string) string {
	return strings.Join(names, ", ")
}

// view snapshots the stress worker state for the worker list.
func (w *stressWorker) view() WorkerView {
	w.mu.Lock()
	defer w.mu.Unlock()

	status := StatusRunning
	if w.completed {
		status = StatusCompleted
	}

	runtime := time.Since(w.startTime)
	if w.completed && !w.endTime.IsZero() {
		runtime = w.endTime.Sub(w.startTime)
	}

	return WorkerView{
		ID:                  w.id,
		Name:                joinNames(w.names),
		Type:                TypeStressTest,
		Status:              status,
		Workers:             w.numWorkers,
		Runtime:             runtime,
		Successful:          w.successful,
		Failed:              w.failed,
		ConsecutiveFailures: w.consecutiveFailures,
		TargetTPS:           float64(w.targetTps),
		CurrentTPS:          w.currentTps,
		ActualTPS:           w.actualTps,
		InstantTPS:          w.instantTps,
		RampUpProgress:      w.rampUpProgress,
		RampUpDuration:      w.rampUpDuration,
		Duration:            w.duration,
	}
}

// aggSource is the raw per-worker input StressStats merges into the
// aggregate summary.
type aggSource struct {
	completed           bool
	targetTps           int
	numWorkers          int
	successful          int
	failed              int
	consecutiveFailures int
	peakInstantTps      float64
	startTime           time.Time
	endTime             time.Time
	duration            time.Duration
	rampUpDuration      time.Duration
	names               []string
	respCodes           map[string]int
	txCounts            []txCounts
	latencies           []time.Duration
	txLatencies         map[string][]time.Duration
}

type txCounts struct {
	name       string
	successful int
	failed     int
	respCodes  map[string]int
}

// txAgg accumulates per-transaction-type input across workers for the
// StressStats aggregate.
type txAgg struct {
	successful int
	failed     int
	respCodes  map[string]int
	latencies  []time.Duration
}

// addTxStats folds a source's per-transaction counts and latencies into the
// aggregate's per-tx map, preserving first-seen order.
func (s *stressAggregate) addTxStats(src *aggSource) {
	for _, tc := range src.txCounts {
		ts, ok := s.perTx[tc.name]
		if !ok {
			ts = &txAgg{respCodes: make(map[string]int)}
			s.perTx[tc.name] = ts
			s.txOrder = append(s.txOrder, tc.name)
		}
		ts.successful += tc.successful
		ts.failed += tc.failed
		for k, v := range tc.respCodes {
			ts.respCodes[k] += v
		}
	}
	for name, lats := range src.txLatencies {
		ts, ok := s.perTx[name]
		if !ok {
			ts = &txAgg{respCodes: make(map[string]int)}
			s.perTx[name] = ts
			s.txOrder = append(s.txOrder, name)
		}
		ts.latencies = append(ts.latencies, lats...)
	}
}
