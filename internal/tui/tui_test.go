package tui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTelemetryBatcher(t *testing.T) {
	var batchCount int32
	var totalProcessed int64

	batcher := NewTelemetryBatcher(50*time.Millisecond, func(b TelemetryBatch) {
		atomic.AddInt32(&batchCount, 1)
		atomic.AddInt64(&totalProcessed, b.TotalCount)
	})

	batcher.Start()

	// High-frequency telemetry events
	for i := 0; i < 100; i++ {
		batcher.Record(float64(i+1), true, "00")
	}

	time.Sleep(150 * time.Millisecond)
	batcher.Stop()

	assert.GreaterOrEqual(t, atomic.LoadInt32(&batchCount), int32(1))
	assert.Equal(t, int64(100), atomic.LoadInt64(&totalProcessed))
}
