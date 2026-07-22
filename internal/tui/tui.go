package tui

import (
	"sync"
	"time"
)

// MetricEvent represents a single telemetry measurement event
type MetricEvent struct {
	Timestamp time.Time
	LatencyMs float64
	Success   bool
	Code      string
}

// TelemetryBatch represents an aggregated snapshot of telemetry measurements over a ticker window
type TelemetryBatch struct {
	TotalCount   int64
	SuccessCount int64
	FailCount    int64
	AvgLatencyMs float64
	MaxLatencyMs float64
	MinLatencyMs float64
	Window       time.Duration
}

// TelemetryBatcher aggregates high-TPS metric events to prevent terminal UI rendering choking
type TelemetryBatcher struct {
	mu           sync.Mutex
	eventChan    chan MetricEvent
	stopChan     chan struct{}
	interval     time.Duration
	onBatch      func(batch TelemetryBatch)
	running      bool
	currentBatch TelemetryBatch
}

// NewTelemetryBatcher creates a new TelemetryBatcher instance
func NewTelemetryBatcher(interval time.Duration, onBatch func(batch TelemetryBatch)) *TelemetryBatcher {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	return &TelemetryBatcher{
		eventChan: make(chan MetricEvent, 10000),
		stopChan:  make(chan struct{}),
		interval:  interval,
		onBatch:   onBatch,
	}
}

// Record emits a metric event into the aggregation channel
func (tb *TelemetryBatcher) Record(latencyMs float64, success bool, code string) {
	if !tb.running {
		return
	}
	select {
	case tb.eventChan <- MetricEvent{
		Timestamp: time.Now(),
		LatencyMs: latencyMs,
		Success:   success,
		Code:      code,
	}:
	default:
		// Drop metric if channel buffer is full to preserve memory
	}
}

// Start launches the background aggregation loop
func (tb *TelemetryBatcher) Start() {
	tb.mu.Lock()
	if tb.running {
		tb.mu.Unlock()
		return
	}
	tb.running = true
	tb.stopChan = make(chan struct{})
	tb.mu.Unlock()

	go tb.loop()
}

// Stop halts the aggregation loop
func (tb *TelemetryBatcher) Stop() {
	tb.mu.Lock()
	if !tb.running {
		tb.mu.Unlock()
		return
	}
	tb.running = false
	close(tb.stopChan)
	tb.mu.Unlock()
}

func (tb *TelemetryBatcher) loop() {
	ticker := time.NewTicker(tb.interval)
	defer ticker.Stop()

	var totalLatency float64
	var minLat, maxLat float64
	var total, success, fail int64

	reset := func() {
		totalLatency = 0
		minLat = 0
		maxLat = 0
		total = 0
		success = 0
		fail = 0
	}

	for {
		select {
		case <-tb.stopChan:
			return
		case ev := <-tb.eventChan:
			total++
			if ev.Success {
				success++
			} else {
				fail++
			}
			totalLatency += ev.LatencyMs
			if total == 1 || ev.LatencyMs < minLat {
				minLat = ev.LatencyMs
			}
			if ev.LatencyMs > maxLat {
				maxLat = ev.LatencyMs
			}
		case <-ticker.C:
			if total > 0 && tb.onBatch != nil {
				batch := TelemetryBatch{
					TotalCount:   total,
					SuccessCount: success,
					FailCount:    fail,
					AvgLatencyMs: totalLatency / float64(total),
					MinLatencyMs: minLat,
					MaxLatencyMs: maxLat,
					Window:       tb.interval,
				}
				tb.onBatch(batch)
			}
			reset()
		}
	}
}
