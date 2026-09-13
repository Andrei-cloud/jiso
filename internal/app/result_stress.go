package app

import (
	"math"
	"sort"
	"time"
)

// StressSummary is the JSON-serializable view of one stress-test worker run.
// It merges the two shapes the CLI produces today: the live per-worker rows
// of CLI.GetWorkerStats (map[string]any, printed by the workers command)
// and the final summary that stressTestWorker.printSummary renders and
// persists as db.StressTestSummaryRecord. Latency percentiles are float64
// milliseconds because that is the precision the session DB stores; planned
// durations are time.Duration.
// The worker status and type vocabulary. These strings leave the package: they
// are the JSON the TUI and any API reader matches on, so they are a wire contract
// rather than local labels. As literals they were spelled at thirteen sites across
// five files, which is how one "complete" becomes a status no operator ever sees --
// and the split of stress_worker.go tripped the linter on exactly that duplication.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	// StatusStopped is how a worker an operator stopped reports itself, as
	// opposed to one that finished; the §K list and the toast say different things.
	StatusStopped = "stopped"

	// TypeStressTest is the WorkerView/summary Type tag for a stress run, as
	// opposed to a background worker.
	TypeStressTest = "stress_test"
)

// StressSummary is one stress run as the result family reports it: counts, rate
// and latency percentiles for one worker. Plain fields and snake_case json tags
// only, so a headless frontend can marshal it straight into the operator's
// --json output.
type StressSummary struct {
	WorkerID            string         `json:"worker_id"`
	SessionID           string         `json:"session_id,omitempty"`
	Name                string         `json:"name"`
	Type                string         `json:"type"`
	Status              string         `json:"status"`
	TransactionNames    []string       `json:"transaction_names,omitempty"`
	Workers             int            `json:"workers"`
	TargetTPS           int            `json:"target_tps"`
	CurrentTPS          float64        `json:"current_tps"`
	ActualTPS           float64        `json:"actual_tps"`
	PeakTPS             float64        `json:"peak_tps"`
	RampUpProgress      float64        `json:"ramp_up_progress"`
	RampUpDuration      time.Duration  `json:"ramp_up_duration"`
	Duration            time.Duration  `json:"duration"`
	Runtime             time.Duration  `json:"runtime"`
	StartTime           time.Time      `json:"start_time"`
	EndTime             time.Time      `json:"end_time"`
	Sent                int            `json:"sent"`
	Successful          int            `json:"successful"`
	Failed              int            `json:"failed"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	ResponseCodes       map[string]int `json:"response_codes,omitempty"`

	MinLatencyMs  float64 `json:"min_latency_ms"`
	MeanLatencyMs float64 `json:"mean_latency_ms"`
	MaxLatencyMs  float64 `json:"max_latency_ms"`
	P50LatencyMs  float64 `json:"p50_latency_ms"`
	P90LatencyMs  float64 `json:"p90_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	P99LatencyMs  float64 `json:"p99_latency_ms"`

	// LatencyBudget classifies recorded latencies against the configured
	// response timeout: Satisfactory <= 50% of it, Tolerable <= 100%,
	// Exceeded beyond that.
	LatencySatisfactory int `json:"latency_satisfactory"`
	LatencyTolerable    int `json:"latency_tolerable"`
	LatencyExceeded     int `json:"latency_exceeded"`

	// LatencyBuckets holds the fixed-bucket latency histogram counts the
	// CLI summary table renders; labels are stable bucket names.
	LatencyBuckets []LatencyBucket `json:"latency_buckets,omitempty"`

	// Transactions holds the per-transaction-type breakdown, in the
	// order the transactions were selected for the run.
	Transactions []TransactionSummary `json:"transactions,omitempty"`
}

// LatencyBucket is one fixed-bucket latency histogram entry of a
// StressSummary (label plus count; empty buckets may be omitted).
type LatencyBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// TransactionSummary is the per-transaction-type breakdown of one stress
// run: execution counts, response-code distribution, and latency
// percentiles in float64 milliseconds.
type TransactionSummary struct {
	Name          string         `json:"name"`
	Successful    int            `json:"successful"`
	Failed        int            `json:"failed"`
	ResponseCodes map[string]int `json:"response_codes,omitempty"`

	MinLatencyMs  float64 `json:"min_latency_ms"`
	MeanLatencyMs float64 `json:"mean_latency_ms"`
	MaxLatencyMs  float64 `json:"max_latency_ms"`
	P50LatencyMs  float64 `json:"p50_latency_ms"`
	P90LatencyMs  float64 `json:"p90_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	P99LatencyMs  float64 `json:"p99_latency_ms"`
}

// latencySummary is one stress sample's latency distribution in float64
// milliseconds: min/mean/max and the p50/p90/p95/p99 quantiles.
type latencySummary struct {
	minMs, meanMs, maxMs float64
	p50, p90, p95, p99   float64
}

// Latency stats returns min, mean, max, p50, p90, p95 and p99 of the
// (unsorted) latencies as float64 milliseconds. A zero value is returned
// for an empty sample.
func latencyStats(latencies []time.Duration) latencySummary {
	if len(latencies) == 0 {
		return latencySummary{}
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sortDurationSlice(sorted)

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }

	return latencySummary{
		minMs:  ms(sorted[0]),
		meanMs: ms(total / time.Duration(len(sorted))),
		maxMs:  ms(sorted[len(sorted)-1]),
		p50:    ms(percentileDuration(sorted, 0.50)),
		p90:    ms(percentileDuration(sorted, 0.90)),
		p95:    ms(percentileDuration(sorted, 0.95)),
		p99:    ms(percentileDuration(sorted, 0.99)),
	}
}

// percentileDuration interpolates the pct quantile of a sorted slice.
func percentileDuration(sorted []time.Duration, pct float64) time.Duration {
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

func sortDurationSlice(sorted []time.Duration) {
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
}
