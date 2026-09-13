package app

import (
	"testing"
	"time"
)

// TestStressSummaryJSONRoundTrip pins the StressSummary wire contract (the
// json tags readers outside this package match on) using the same shape the
// live worker and the persisted record produce.
func TestStressSummaryJSONRoundTrip(t *testing.T) {
	t.Parallel()

	start := scenarioFixtureTime()
	live := &StressSummary{
		WorkerID:            "w-1",
		Name:                "Purchase, Echo",
		Type:                TypeStressTest,
		Status:              StatusRunning,
		Workers:             4,
		TargetTPS:           50,
		CurrentTPS:          42.5,
		ActualTPS:           39.75,
		RampUpProgress:      80.0,
		RampUpDuration:      30 * time.Second,
		Duration:            2 * time.Minute,
		Runtime:             65 * time.Second,
		Sent:                103,
		Successful:          100,
		Failed:              3,
		ConsecutiveFailures: 3,
	}
	final := &StressSummary{
		WorkerID:         "w-2",
		Status:           StatusCompleted,
		TransactionNames: []string{"Purchase", "Echo"},
		StartTime:        start,
		EndTime:          start.Add(90 * time.Second),
		Duration:         time.Minute,
		Sent:             600,
		Successful:       590,
		Failed:           10,
		ResponseCodes:    map[string]int{"00": 585, "05": 10, "ERROR": 5},
		P99LatencyMs:     45.0,
	}

	tests := []struct {
		name     string
		summary  *StressSummary
		wantKeys []string
	}{
		{
			name:     "live worker shape",
			summary:  live,
			wantKeys: []string{"worker_id", "target_tps", "ramp_up_duration", "consecutive_failures"},
		},
		{
			name:     "final persisted shape with histogram and percentiles",
			summary:  final,
			wantKeys: []string{"response_codes", "p99_latency_ms", "transaction_names", "start_time"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.summary, tt.wantKeys)
		})
	}
}
