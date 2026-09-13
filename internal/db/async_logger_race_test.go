package db

import (
	"sync"
	"testing"
	"time"
)

// currentLogger reads the singleton under the same mutex the lifecycle
// functions use, keeping the regression tests' observation race-clean
// (the production accessor GetAsyncLogger was test-only dead code).
func currentLogger() *AsyncLogger {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	return logger
}

// Regression: concurrent StopAsyncLogger callers both passed the
// "logger != nil" check and double-closed logger.done (panic). With -race
// the unsynchronized global access also trips the detector.
func TestStopAsyncLoggerConcurrent(t *testing.T) {
	InitAsyncLogger(4, 2, time.Hour)
	if currentLogger() == nil {
		t.Fatal("logger not initialized")
	}

	LogTransactionEnriched(&TransactionRecord{SessionID: "s1", TxName: "tx", RequestJSON: `{}`, Success: true})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			StopAsyncLogger()
		}()
	}
	wg.Wait()

	if currentLogger() != nil {
		t.Fatal("logger global not cleared after stop")
	}

	// A third stop must be a no-op, and the singleton must be
	// re-initializable after a full stop.
	StopAsyncLogger()

	InitAsyncLogger(4, 2, time.Hour)
	if currentLogger() == nil {
		t.Fatal("logger not re-initialized after stop")
	}
	StopAsyncLogger()
}

// Regression: FlushTransactions is documented to flush and may be called
// concurrently with StopAsyncLogger from shutdown hooks.
func TestFlushTransactionsConcurrentWithStop(t *testing.T) {
	InitAsyncLogger(4, 2, time.Hour)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		FlushTransactions()
	}()
	go func() {
		defer wg.Done()
		StopAsyncLogger()
	}()
	wg.Wait()

	if currentLogger() != nil {
		t.Fatal("logger global not cleared after stop")
	}
}
