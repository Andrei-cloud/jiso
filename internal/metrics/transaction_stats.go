package metrics

import (
	"math"
	"sync"
	"time"
)

// TransactionStats tracks statistics for transaction executions
type TransactionStats struct {
	start         time.Time
	counts        int
	executionTime time.Duration
	variance      time.Duration
	respCodes     map[string]uint64
	mu            sync.Mutex // Main mutex for all fields
	maxRespCodes  int        // Maximum number of response codes to track
}

// NewTransactionStats creates a new TransactionStats instance
func NewTransactionStats() *TransactionStats {
	return &TransactionStats{
		respCodes:    make(map[string]uint64),
		maxRespCodes: 100, // Limit to prevent unbounded growth
	}
}

// StartClock begins timing the transaction execution
func (ts *TransactionStats) StartClock() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.start = time.Now()
}

// RecordExecution records a transaction execution with its duration
func (ts *TransactionStats) RecordExecution(duration time.Duration, respCode string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.executionTime += duration
	ts.counts++

	if respCode != "" {
		ts.respCodes[respCode]++

		// If we've exceeded the maximum number of response codes, remove the least frequent one
		if len(ts.respCodes) > ts.maxRespCodes {
			var minCode string
			var minCount uint64 = ^uint64(0) // Max uint64 value
			for code, count := range ts.respCodes {
				if count < minCount {
					minCount = count
					minCode = code
				}
			}
			if minCode != "" {
				delete(ts.respCodes, minCode)
			}
		}

		// Calculate variance
		// Note: Mean is calculated on the fly, but for variance we need a running algorithm or store sum/counts
		// Using Welford's online algorithm or similar would be better, but sticking to simple approx for now
		// Re-implementing simplified variance tracking to avoid complex recursion/deps
		// Variance = E[X^2] - (E[X])^2 ? Or just sum of squares diff?
		// Existing code: diff := duration - mean; variance += diff * diff
		// We need mean based on current count.
		currentMean := ts.executionTime / time.Duration(ts.counts)
		diff := duration - currentMean
		ts.variance += diff * diff
	}
}

// ExecutionCount returns the number of executions
func (ts *TransactionStats) ExecutionCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.counts
}

// Duration returns the total elapsed time since starting
func (ts *TransactionStats) Duration() time.Duration {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return time.Since(ts.start)
}

// MeanExecutionTime calculates the mean execution time
func (ts *TransactionStats) MeanExecutionTime() time.Duration {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.counts == 0 {
		return 0
	}
	return ts.executionTime / time.Duration(ts.counts)
}

// StandardDeviation calculates the standard deviation of execution times
func (ts *TransactionStats) StandardDeviation() time.Duration {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.counts <= 1 {
		return 0
	}
	locVariance := ts.variance
	locVariance /= time.Duration(ts.counts)
	return time.Duration(math.Sqrt(float64(locVariance)))
}

// ResponseCodes returns a copy of the response code map
func (ts *TransactionStats) ResponseCodes() map[string]uint64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Return a copy to avoid race conditions
	result := make(map[string]uint64)
	for k, v := range ts.respCodes {
		result[k] = v
	}
	return result
}
