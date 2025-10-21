package metrics

import (
	"testing"
	"time"
)

func TestNewNetworkingStats(t *testing.T) {
	stats := NewNetworkingStats()
	if stats == nil {
		t.Fatal("NewNetworkingStats returned nil")
	}

	// Check initial values
	if stats.ReconnectAttempts() != 0 {
		t.Errorf("Expected initial reconnect attempts 0, got %d", stats.ReconnectAttempts())
	}
	if stats.ReconnectSuccesses() != 0 {
		t.Errorf("Expected initial reconnect successes 0, got %d", stats.ReconnectSuccesses())
	}
	if stats.ReconnectFailures() != 0 {
		t.Errorf("Expected initial reconnect failures 0, got %d", stats.ReconnectFailures())
	}
	if stats.MeanReconnectTime() != 0 {
		t.Errorf("Expected initial mean reconnect time 0, got %v", stats.MeanReconnectTime())
	}
	if stats.BackoffTriggers() != 0 {
		t.Errorf("Expected initial backoff triggers 0, got %d", stats.BackoffTriggers())
	}
	if stats.MeanBackoffTime() != 0 {
		t.Errorf("Expected initial mean backoff time 0, got %v", stats.MeanBackoffTime())
	}
	if stats.CircuitBreakerTrips() != 0 {
		t.Errorf("Expected initial circuit breaker trips 0, got %d", stats.CircuitBreakerTrips())
	}
	if stats.CircuitBreakerResets() != 0 {
		t.Errorf("Expected initial circuit breaker resets 0, got %d", stats.CircuitBreakerResets())
	}
	if stats.HealthChecks() != 0 {
		t.Errorf("Expected initial health checks 0, got %d", stats.HealthChecks())
	}
	if stats.HealthCheckFailures() != 0 {
		t.Errorf("Expected initial health check failures 0, got %d", stats.HealthCheckFailures())
	}
	if stats.RetriableErrors() != 0 {
		t.Errorf("Expected initial retriable errors 0, got %d", stats.RetriableErrors())
	}
	if stats.PermanentErrors() != 0 {
		t.Errorf("Expected initial permanent errors 0, got %d", stats.PermanentErrors())
	}
}

func TestReconnectMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record attempts
	stats.RecordReconnectAttempt()
	stats.RecordReconnectAttempt()
	if stats.ReconnectAttempts() != 2 {
		t.Errorf("Expected 2 reconnect attempts, got %d", stats.ReconnectAttempts())
	}

	// Record successes
	stats.RecordReconnectSuccess(100 * time.Millisecond)
	stats.RecordReconnectSuccess(200 * time.Millisecond)
	if stats.ReconnectSuccesses() != 2 {
		t.Errorf("Expected 2 reconnect successes, got %d", stats.ReconnectSuccesses())
	}
	expectedMean := 150 * time.Millisecond
	if stats.MeanReconnectTime() != expectedMean {
		t.Errorf("Expected mean reconnect time %v, got %v", expectedMean, stats.MeanReconnectTime())
	}

	// Record failures
	stats.RecordReconnectFailure()
	if stats.ReconnectFailures() != 1 {
		t.Errorf("Expected 1 reconnect failure, got %d", stats.ReconnectFailures())
	}
}

func TestBackoffMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record backoffs
	stats.RecordBackoff(50 * time.Millisecond)
	stats.RecordBackoff(150 * time.Millisecond)
	if stats.BackoffTriggers() != 2 {
		t.Errorf("Expected 2 backoff triggers, got %d", stats.BackoffTriggers())
	}
	expectedMean := 100 * time.Millisecond
	if stats.MeanBackoffTime() != expectedMean {
		t.Errorf("Expected mean backoff time %v, got %v", expectedMean, stats.MeanBackoffTime())
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record trips and resets
	stats.RecordCircuitBreakerTrip()
	stats.RecordCircuitBreakerTrip()
	stats.RecordCircuitBreakerReset()
	if stats.CircuitBreakerTrips() != 2 {
		t.Errorf("Expected 2 circuit breaker trips, got %d", stats.CircuitBreakerTrips())
	}
	if stats.CircuitBreakerResets() != 1 {
		t.Errorf("Expected 1 circuit breaker reset, got %d", stats.CircuitBreakerResets())
	}
}

func TestHealthCheckMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record health checks
	stats.RecordHealthCheck(true)  // success
	stats.RecordHealthCheck(false) // failure
	stats.RecordHealthCheck(true)  // success
	if stats.HealthChecks() != 3 {
		t.Errorf("Expected 3 health checks, got %d", stats.HealthChecks())
	}
	if stats.HealthCheckFailures() != 1 {
		t.Errorf("Expected 1 health check failure, got %d", stats.HealthCheckFailures())
	}
}

func TestErrorMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record errors
	stats.RecordError(true)  // retriable
	stats.RecordError(false) // permanent
	stats.RecordError(true)  // retriable
	if stats.RetriableErrors() != 2 {
		t.Errorf("Expected 2 retriable errors, got %d", stats.RetriableErrors())
	}
	if stats.PermanentErrors() != 1 {
		t.Errorf("Expected 1 permanent error, got %d", stats.PermanentErrors())
	}
}

func TestGetAllMetrics(t *testing.T) {
	stats := NewNetworkingStats()

	// Record some metrics
	stats.RecordReconnectAttempt()
	stats.RecordReconnectSuccess(100 * time.Millisecond)
	stats.RecordBackoff(50 * time.Millisecond)
	stats.RecordCircuitBreakerTrip()
	stats.RecordHealthCheck(false)
	stats.RecordError(true)

	metrics := stats.GetAllMetrics()

	// Check that all expected keys are present
	expectedKeys := []string{
		"reconnect_attempts",
		"reconnect_successes",
		"reconnect_failures",
		"mean_reconnect_time_ms",
		"backoff_triggers",
		"mean_backoff_time_ms",
		"circuit_breaker_trips",
		"circuit_breaker_resets",
		"health_checks",
		"health_check_failures",
		"retriable_errors",
		"permanent_errors",
	}

	for _, key := range expectedKeys {
		if _, exists := metrics[key]; !exists {
			t.Errorf("Expected key %s in metrics map", key)
		}
	}

	// Check specific values
	if val, ok := metrics["reconnect_attempts"].(int64); !ok || val != 1 {
		t.Errorf("Expected reconnect_attempts 1, got %v", metrics["reconnect_attempts"])
	}
	if val, ok := metrics["reconnect_successes"].(int64); !ok || val != 1 {
		t.Errorf("Expected reconnect_successes 1, got %v", metrics["reconnect_successes"])
	}
	if val, ok := metrics["mean_reconnect_time_ms"].(int64); !ok || val != 100 {
		t.Errorf("Expected mean_reconnect_time_ms 100, got %v", metrics["mean_reconnect_time_ms"])
	}
	if val, ok := metrics["backoff_triggers"].(int64); !ok || val != 1 {
		t.Errorf("Expected backoff_triggers 1, got %v", metrics["backoff_triggers"])
	}
	if val, ok := metrics["mean_backoff_time_ms"].(int64); !ok || val != 50 {
		t.Errorf("Expected mean_backoff_time_ms 50, got %v", metrics["mean_backoff_time_ms"])
	}
	if val, ok := metrics["circuit_breaker_trips"].(int64); !ok || val != 1 {
		t.Errorf("Expected circuit_breaker_trips 1, got %v", metrics["circuit_breaker_trips"])
	}
	if val, ok := metrics["health_checks"].(int64); !ok || val != 1 {
		t.Errorf("Expected health_checks 1, got %v", metrics["health_checks"])
	}
	if val, ok := metrics["health_check_failures"].(int64); !ok || val != 1 {
		t.Errorf("Expected health_check_failures 1, got %v", metrics["health_check_failures"])
	}
	if val, ok := metrics["retriable_errors"].(int64); !ok || val != 1 {
		t.Errorf("Expected retriable_errors 1, got %v", metrics["retriable_errors"])
	}
	if val, ok := metrics["permanent_errors"].(int64); !ok || val != 0 {
		t.Errorf("Expected permanent_errors 0, got %v", metrics["permanent_errors"])
	}
}

func TestMeanCalculationsWithZero(t *testing.T) {
	stats := NewNetworkingStats()

	// Test mean calculations with no data
	if stats.MeanReconnectTime() != 0 {
		t.Errorf("Expected mean reconnect time 0 with no data, got %v", stats.MeanReconnectTime())
	}
	if stats.MeanBackoffTime() != 0 {
		t.Errorf("Expected mean backoff time 0 with no data, got %v", stats.MeanBackoffTime())
	}

	// Record some data then check
	stats.RecordReconnectSuccess(100 * time.Millisecond)
	if stats.MeanReconnectTime() != 100*time.Millisecond {
		t.Errorf("Expected mean reconnect time 100ms, got %v", stats.MeanReconnectTime())
	}
}
