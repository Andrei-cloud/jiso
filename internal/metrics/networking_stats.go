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

// Getters for metrics
func (ns *NetworkingStats) ReconnectAttempts() int64 {
	return atomic.LoadInt64(&ns.reconnectAttempts)
}

func (ns *NetworkingStats) ReconnectSuccesses() int64 {
	return atomic.LoadInt64(&ns.reconnectSuccesses)
}

func (ns *NetworkingStats) ReconnectFailures() int64 {
	return atomic.LoadInt64(&ns.reconnectFailures)
}

func (ns *NetworkingStats) MeanReconnectTime() time.Duration {
	ns.reconnectTimeLock.Lock()
	defer ns.reconnectTimeLock.Unlock()

	attempts := atomic.LoadInt64(&ns.reconnectSuccesses)
	if attempts == 0 {
		return 0
	}
	return ns.totalReconnectTime / time.Duration(attempts)
}

func (ns *NetworkingStats) BackoffTriggers() int64 {
	return atomic.LoadInt64(&ns.backoffTriggers)
}

func (ns *NetworkingStats) MeanBackoffTime() time.Duration {
	ns.backoffTimeLock.Lock()
	defer ns.backoffTimeLock.Unlock()

	triggers := atomic.LoadInt64(&ns.backoffTriggers)
	if triggers == 0 {
		return 0
	}
	return ns.totalBackoffTime / time.Duration(triggers)
}

func (ns *NetworkingStats) CircuitBreakerTrips() int64 {
	return atomic.LoadInt64(&ns.circuitBreakerTrips)
}

func (ns *NetworkingStats) CircuitBreakerResets() int64 {
	return atomic.LoadInt64(&ns.circuitBreakerResets)
}

func (ns *NetworkingStats) HealthChecks() int64 {
	return atomic.LoadInt64(&ns.healthChecks)
}

func (ns *NetworkingStats) HealthCheckFailures() int64 {
	return atomic.LoadInt64(&ns.healthCheckFailures)
}

func (ns *NetworkingStats) RetriableErrors() int64 {
	return atomic.LoadInt64(&ns.retriableErrors)
}

func (ns *NetworkingStats) PermanentErrors() int64 {
	return atomic.LoadInt64(&ns.permanentErrors)
}

// GetAllMetrics returns all networking metrics as a map
func (ns *NetworkingStats) GetAllMetrics() map[string]interface{} {
	return map[string]interface{}{
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
	}
}
