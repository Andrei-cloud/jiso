// stress_summary.go turns a finished (or stopped) stress worker into the summary an
// operator reads: the aggregate counts, the latency histogram, and the write that
// lets the run be recalled after it is gone.
package app

import (
	"fmt"
	"time"

	"jiso/internal/db"

	json "github.com/goccy/go-json"
)

// buildStressSummary snapshots the finished worker into the shared
// StressSummary shape, including latency percentiles, budget classes,
// histogram buckets and the per-transaction breakdown.
func (a *App) buildStressSummary(w *stressWorker) *StressSummary {
	w.mu.Lock()
	status := StatusRunning
	if w.completed {
		status = StatusCompleted
	}
	summary := &StressSummary{
		WorkerID:            w.id,
		SessionID:           w.sessionID,
		Name:                joinNames(w.names),
		Type:                TypeStressTest,
		Status:              status,
		TransactionNames:    append([]string(nil), w.names...),
		Workers:             w.numWorkers,
		TargetTPS:           w.targetTps,
		ActualTPS:           w.actualTps,
		PeakTPS:             w.peakInstantTps,
		RampUpProgress:      w.rampUpProgress,
		RampUpDuration:      w.rampUpDuration,
		Duration:            w.duration,
		Runtime:             w.endTime.Sub(w.startTime),
		StartTime:           w.startTime,
		EndTime:             w.endTime,
		Successful:          w.successful,
		Failed:              w.failed,
		ConsecutiveFailures: w.consecutiveFailures,
	}
	summary.Sent = summary.Successful + summary.Failed
	summary.ResponseCodes = make(map[string]int, len(w.respCodes))
	for k, v := range w.respCodes {
		summary.ResponseCodes[k] = v
	}
	latencies := make([]time.Duration, len(w.latencies))
	copy(latencies, w.latencies)
	perTx := copyTxStats(w)
	w.mu.Unlock()

	dist := latencyStats(latencies)
	summary.MinLatencyMs, summary.MeanLatencyMs, summary.MaxLatencyMs,
		summary.P50LatencyMs, summary.P90LatencyMs, summary.P95LatencyMs, summary.P99LatencyMs =
		dist.minMs, dist.meanMs, dist.maxMs, dist.p50, dist.p90, dist.p95, dist.p99

	timeout := a.cfg.GetResponseTimeout()
	for _, d := range latencies {
		switch {
		case d <= timeout/2:
			summary.LatencySatisfactory++
		case d <= timeout:
			summary.LatencyTolerable++
		default:
			summary.LatencyExceeded++
		}
	}
	summary.LatencyBuckets = latencyHistogram(latencies)

	summary.Transactions = make([]TransactionSummary, 0, len(perTx))
	for i, ts := range perTx {
		name := ""
		if i < len(summary.TransactionNames) {
			name = summary.TransactionNames[i]
		}
		dist := latencyStats(ts.latencies)
		summary.Transactions = append(summary.Transactions, TransactionSummary{
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

	return summary
}

// persistStressSummary writes the final summary to the session database
// when one is configured, mirroring the legacy reporter record. A persist
// failure is routed to the app debug hook (events.Logf) rather than
// swallowed: in the TUI process the summary lands through the synchronous
// global-conn fallback, and a silent failure there previously hid the fact
// that the summary row never arrived.
func (a *App) persistStressSummary(s *StressSummary) {
	if a.cfg.GetDbPath() == "" {
		return
	}

	txJSON, _ := json.Marshal(s.TransactionNames)
	respJSON, _ := json.Marshal(s.ResponseCodes)

	if err := db.InsertStressTestSummary(&db.StressTestSummaryRecord{
		SessionID:              s.SessionID,
		WorkerID:               s.WorkerID,
		StartTime:              s.StartTime,
		EndTime:                s.EndTime,
		TargetTPS:              s.TargetTPS,
		Concurrency:            s.Workers,
		TotalDurationMs:        s.Duration.Milliseconds(),
		TotalTransactions:      s.Sent,
		SuccessfulTransactions: s.Successful,
		FailedTransactions:     s.Failed,
		AverageTPS:             s.ActualTPS,
		PeakTPS:                s.PeakTPS,
		MinLatencyMs:           s.MinLatencyMs,
		MaxLatencyMs:           s.MaxLatencyMs,
		MeanLatencyMs:          s.MeanLatencyMs,
		P50LatencyMs:           s.P50LatencyMs,
		P90LatencyMs:           s.P90LatencyMs,
		P95LatencyMs:           s.P95LatencyMs,
		P99LatencyMs:           s.P99LatencyMs,
		TransactionsJSON:       string(txJSON),
		ResponseCodesJSON:      string(respJSON),
	}); err != nil {
		a.publishLogf("error", fmt.Sprintf("failed to persist stress summary for worker %s: %v", s.WorkerID, err))
	}
}

// latencyHistogram buckets latencies into the fixed CLI display buckets.
// The labels are stable and reused verbatim by the frontends.
func latencyHistogram(latencies []time.Duration) []LatencyBucket {
	type bucket struct {
		label string
		min   time.Duration
		max   time.Duration
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

	out := make([]LatencyBucket, 0, len(buckets))
	for _, b := range buckets {
		count := 0
		for _, d := range latencies {
			if d > b.min && d <= b.max {
				count++
			}
		}
		if count > 0 {
			out = append(out, LatencyBucket{Label: b.label, Count: count})
		}
	}

	return out
}

// copyTxStats deep-copies each named transaction's stats into a stable snapshot.
// The caller holds the worker lock; a missing per-name entry becomes an empty
// stats row so the per-transaction list stays aligned with the names.
func copyTxStats(w *stressWorker) []txStats {
	perTx := make([]txStats, 0, len(w.txStats))
	for _, name := range w.names {
		ts := w.txStats[name]
		if ts == nil {
			perTx = append(perTx, txStats{respCodes: map[string]int{}})

			continue
		}
		cp := txStats{
			successful: ts.successful,
			failed:     ts.failed,
			respCodes:  make(map[string]int, len(ts.respCodes)),
			latencies:  make([]time.Duration, len(ts.latencies)),
		}
		for k, v := range ts.respCodes {
			cp.respCodes[k] = v
		}
		copy(cp.latencies, ts.latencies)
		perTx = append(perTx, cp)
	}

	return perTx
}
