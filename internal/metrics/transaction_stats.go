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
	respCodesLock sync.Mutex
	maxRespCodes  int // Maximum number of response codes to track
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
	ts.start = time.Now()
}

// RecordExecution records a transaction execution with its duration
func (ts *TransactionStats) RecordExecution(duration time.Duration, respCode string) {
	ts.executionTime += duration
	ts.counts++

	if respCode != "" {
		ts.respCodesLock.Lock()
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

		ts.respCodesLock.Unlock()

		// Calculate variance
		mean := ts.MeanExecutionTime()
		diff := duration - mean
		ts.variance += diff * diff
	}
}

// ExecutionCount returns the number of executions
func (ts *TransactionStats) ExecutionCount() int {
	return ts.counts
}

// Duration returns the total elapsed time since starting
func (ts *TransactionStats) Duration() time.Duration {
	return time.Since(ts.start)
}

// MeanExecutionTime calculates the mean execution time
func (ts *TransactionStats) MeanExecutionTime() time.Duration {
	if ts.counts == 0 {
		return 0
	}
	return ts.executionTime / time.Duration(ts.counts)
}

// StandardDeviation calculates the standard deviation of execution times
func (ts *TransactionStats) StandardDeviation() time.Duration {
	if ts.counts <= 1 {
		return 0
	}
	locVariance := ts.variance
	locVariance /= time.Duration(ts.counts)
	return time.Duration(math.Sqrt(float64(locVariance)))
}

// ResponseCodes returns a copy of the response code map
func (ts *TransactionStats) ResponseCodes() map[string]uint64 {
	ts.respCodesLock.Lock()
	defer ts.respCodesLock.Unlock()

	// Return a copy to avoid race conditions
	result := make(map[string]uint64)
	for k, v := range ts.respCodes {
		result[k] = v
	}
	return result
}
