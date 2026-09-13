package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// NetworkingStats tracks networking-related metrics
type NetworkingStats struct {
	// Reconnection metrics
	reconnectAttempts  int64
	reconnectSuccesses int64
	reconnectFailures  int64
	totalReconnectTime time.Duration
	reconnectTimeLock  sync.Mutex

	// Backoff metrics
	backoffTriggers  int64
	totalBackoffTime time.Duration
	backoffTimeLock  sync.Mutex

	// Circuit breaker metrics
	circuitBreakerTrips  int64
	circuitBreakerResets int64

	// Connection health metrics
	healthChecks        int64
	healthCheckFailures int64

	// Error classification metrics
	retriableErrors int64
	permanentErrors int64

	// Wire volume counters (bytes written to / read from sockets)
	txBytes int64
	rxBytes int64
}

// NewNetworkingStats creates a new NetworkingStats instance
func NewNetworkingStats() *NetworkingStats {
	return &NetworkingStats{}
}

// RecordReconnectAttempt records a reconnection attempt
func (ns *NetworkingStats) RecordReconnectAttempt() {
	atomic.AddInt64(&ns.reconnectAttempts, 1)
}

// RecordReconnectSuccess records a successful reconnection
func (ns *NetworkingStats) RecordReconnectSuccess(duration time.Duration) {
	atomic.AddInt64(&ns.reconnectSuccesses, 1)
	ns.reconnectTimeLock.Lock()
	ns.totalReconnectTime += duration
	ns.reconnectTimeLock.Unlock()
}

// RecordReconnectFailure records a failed reconnection
func (ns *NetworkingStats) RecordReconnectFailure() {
	atomic.AddInt64(&ns.reconnectFailures, 1)
}

// RecordBackoff records a backoff delay
func (ns *NetworkingStats) RecordBackoff(delay time.Duration) {
	atomic.AddInt64(&ns.backoffTriggers, 1)
	ns.backoffTimeLock.Lock()
	ns.totalBackoffTime += delay
	ns.backoffTimeLock.Unlock()
}

// RecordCircuitBreakerTrip records a circuit breaker activation
func (ns *NetworkingStats) RecordCircuitBreakerTrip() {
	atomic.AddInt64(&ns.circuitBreakerTrips, 1)
}

// RecordCircuitBreakerReset records a circuit breaker reset
func (ns *NetworkingStats) RecordCircuitBreakerReset() {
	atomic.AddInt64(&ns.circuitBreakerResets, 1)
}

// RecordHealthCheck records a connection health check
func (ns *NetworkingStats) RecordHealthCheck(success bool) {
	atomic.AddInt64(&ns.healthChecks, 1)
	if !success {
		atomic.AddInt64(&ns.healthCheckFailures, 1)
	}
}

// RecordError records an error classification
func (ns *NetworkingStats) RecordError(retriable bool) {
	if retriable {
		atomic.AddInt64(&ns.retriableErrors, 1)
	} else {
		atomic.AddInt64(&ns.permanentErrors, 1)
	}
}

// RecordTxBytes adds n to the written-bytes counter (n <= 0 is a no-op)
func (ns *NetworkingStats) RecordTxBytes(n int64) {
	if n > 0 {
		atomic.AddInt64(&ns.txBytes, n)
	}
}

// RecordRxBytes adds n to the received-bytes counter (n <= 0 is a no-op)
func (ns *NetworkingStats) RecordRxBytes(n int64) {
	if n > 0 {
		atomic.AddInt64(&ns.rxBytes, n)
	}
}

// ReconnectAttempts counts dial attempts the connection layer made for this
// connection, not messages sent, and it never resets, so a rate must be
// derived by differencing two samples.
func (ns *NetworkingStats) ReconnectAttempts() int64 {
	return atomic.LoadInt64(&ns.reconnectAttempts)
}

// ReconnectSuccesses counts attempts that reached a usable connection. It can
// lag ReconnectAttempts by the attempts still in flight.
func (ns *NetworkingStats) ReconnectSuccesses() int64 {
	return atomic.LoadInt64(&ns.reconnectSuccesses)
}

// ReconnectFailures counts attempts that did not. Together with the successes
// it is the denominator the reconnect mean uses.
func (ns *NetworkingStats) ReconnectFailures() int64 {
	return atomic.LoadInt64(&ns.reconnectFailures)
}

// MeanReconnectTime is the average duration of *successful* reconnects.
// Failed attempts contribute no time, so a connection that has only ever
// failed reports 0 -- that is a missing sample, not a fast reconnect.
func (ns *NetworkingStats) MeanReconnectTime() time.Duration {
	ns.reconnectTimeLock.Lock()
	defer ns.reconnectTimeLock.Unlock()

	attempts := atomic.LoadInt64(&ns.reconnectSuccesses)
	if attempts == 0 {
		return 0
	}
	return ns.totalReconnectTime / time.Duration(attempts)
}

// BackoffTriggers counts how many times the reconnect sequence backed off
// before retrying.
func (ns *NetworkingStats) BackoffTriggers() int64 {
	return atomic.LoadInt64(&ns.backoffTriggers)
}

// MeanBackoffTime is the average backoff actually slept, over triggers. Zero
// means the sequence never backed off, which is what a first-attempt success
// looks like.
func (ns *NetworkingStats) MeanBackoffTime() time.Duration {
	ns.backoffTimeLock.Lock()
	defer ns.backoffTimeLock.Unlock()

	triggers := atomic.LoadInt64(&ns.backoffTriggers)
	if triggers == 0 {
		return 0
	}
	return ns.totalBackoffTime / time.Duration(triggers)
}

// CircuitBreakerTrips counts the times the breaker opened and started
// rejecting work outright.
func (ns *NetworkingStats) CircuitBreakerTrips() int64 {
	return atomic.LoadInt64(&ns.circuitBreakerTrips)
}

// CircuitBreakerResets counts the times it closed again. Trips above resets
// means the breaker is open right now.
func (ns *NetworkingStats) CircuitBreakerResets() int64 {
	return atomic.LoadInt64(&ns.circuitBreakerResets)
}

// HealthChecks counts health probes performed.
func (ns *NetworkingStats) HealthChecks() int64 {
	return atomic.LoadInt64(&ns.healthChecks)
}

// HealthCheckFailures counts probes that failed. The breaker decides its
// state from this ratio, so the two counters are read together.
func (ns *NetworkingStats) HealthCheckFailures() int64 {
	return atomic.LoadInt64(&ns.healthCheckFailures)
}

// RetriableErrors counts errors the connection layer classified as worth
// trying again -- timeouts, reset connections, and the like.
func (ns *NetworkingStats) RetriableErrors() int64 {
	return atomic.LoadInt64(&ns.retriableErrors)
}

// PermanentErrors counts errors classified as terminal (bad certificate,
// unknown authority), which stop the retry loop instead of joining the
// retriable count.
func (ns *NetworkingStats) PermanentErrors() int64 {
	return atomic.LoadInt64(&ns.permanentErrors)
}

// TxBytes is the cumulative bytes written to the peer since the process
// started. It is cumulative on purpose: the TUI derives a rate by
// differencing samples, and a per-message total would make the arithmetic
// wrong.
func (ns *NetworkingStats) TxBytes() int64 {
	return atomic.LoadInt64(&ns.txBytes)
}

// RxBytes is the cumulative bytes read from the peer, with the same
// differencing contract as TxBytes.
func (ns *NetworkingStats) RxBytes() int64 {
	return atomic.LoadInt64(&ns.rxBytes)
}

// GetAllMetrics returns all networking metrics as a map
func (ns *NetworkingStats) GetAllMetrics() map[string]any {
	return map[string]any{
		"reconnect_attempts":     ns.ReconnectAttempts(),
		"reconnect_successes":    ns.ReconnectSuccesses(),
		"reconnect_failures":     ns.ReconnectFailures(),
		"mean_reconnect_time_ms": ns.MeanReconnectTime().Milliseconds(),
		"backoff_triggers":       ns.BackoffTriggers(),
		"mean_backoff_time_ms":   ns.MeanBackoffTime().Milliseconds(),
		"circuit_breaker_trips":  ns.CircuitBreakerTrips(),
		"circuit_breaker_resets": ns.CircuitBreakerResets(),
		"health_checks":          ns.HealthChecks(),
		"health_check_failures":  ns.HealthCheckFailures(),
		"retriable_errors":       ns.RetriableErrors(),
		"permanent_errors":       ns.PermanentErrors(),
		"tx_bytes":               ns.TxBytes(),
		"rx_bytes":               ns.RxBytes(),
	}
}
