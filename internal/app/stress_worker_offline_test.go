package app

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: ExecuteBackground reports a skipped send (connection dropped
// mid-run) as response code "OFFLINE" with a nil error. The old bookkeeping
// counted it as a success with a 0 ms latency sample, inflating the
// stress-summary success rate.
func TestRecordStressCompletionOfflineIsSkippedNotSuccess(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = a.Close() }()

	w := &stressWorker{id: "w1", lastSampleTime: time.Now()}

	a.recordStressCompletion(w, "tx", "OFFLINE", 0, nil)
	a.recordStressCompletion(w, "tx", "00", 5*time.Millisecond, nil)
	a.recordStressCompletion(w, "tx", "", 0, errors.New("boom"))

	w.mu.Lock()
	defer w.mu.Unlock()

	assert.Equal(t, 1, w.successful, "OFFLINE must not count as success")
	assert.Equal(t, 1, w.failed)
	assert.Equal(t, 1, w.respCodes["OFFLINE"], "skipped sends stay visible in the response-code distribution")
	assert.Equal(t, 1, w.respCodes["00"])
	assert.Equal(t, 1, w.respCodes["ERROR"])
	assert.Len(t, w.latencies, 2, "skipped sends must not add a 0 ms latency sample")
}
