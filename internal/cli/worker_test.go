package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"jiso/internal/command"
	"jiso/internal/service"
)

func TestWorkerResourceCleanup(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start a worker
	workerID, err := cli.StartWorker("test-transaction", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Verify worker is running
	stats := cli.GetWorkerStats()
	activeCount, _ := stats["active"].(int)
	if activeCount != 1 {
		t.Errorf("Expected 1 active worker, got %d", activeCount)
	}

	// Stop the worker
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify worker is stopped
	stats = cli.GetWorkerStats()
	activeCount, _ = stats["active"].(int)
	if activeCount != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", activeCount)
	}

	// Verify worker is removed from map
	cli.mu.Lock()
	_, exists := cli.workers[workerID]
	cli.mu.Unlock()
	if exists {
		t.Error("Worker still exists in map after stopping")
	}
}

func TestStressTestWorkerResourceCleanup(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start a stress test worker with short duration
	workerID, err := cli.StartStressTestWorker(
		[]string{"test-transaction"},
		10,                   // target TPS
		100*time.Millisecond, // ramp up duration
		200*time.Millisecond, // test duration
		1,                    // num workers
	)
	if err != nil {
		t.Fatalf("Failed to start stress test worker: %v", err)
	}

	// Verify worker is running
	stats := cli.GetWorkerStats()
	activeCount, _ := stats["active"].(int)
	if activeCount != 1 {
		t.Errorf("Expected 1 active worker, got %d", activeCount)
	}

	// Stop the worker immediately to avoid executing background logic
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop stress test worker: %v", err)
	}

	// Verify worker is stopped
	stats = cli.GetWorkerStats()
	activeCount, _ = stats["active"].(int)
	if activeCount != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", activeCount)
	}
}

func TestStressTestWorkerParallel(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a dummy spec file
	spec := `{"name": "Test Spec", "fields": {"0": {"type": "String", "length": 4, "description": "MTI", "enc": "ASCII", "prefix": "ASCII.Fixed"}}}`
	tmpFile, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp spec file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
		_ = tmpFile.Close()
	}()
	_, _ = tmpFile.WriteString(spec)

	svc, err := service.NewService(
		"localhost", "9999", tmpFile.Name(), false, 0, 5*time.Second, 10*time.Second, 5*time.Second,
	)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	sendCmd := &command.SendCommand{
		Svc: svc,
	}
	cli.commands["send"] = sendCmd

	// Start a stress test worker with short duration
	workerID, err := cli.StartStressTestWorker(
		[]string{"test-transaction"},
		20,                  // target TPS
		50*time.Millisecond, // ramp up duration
		50*time.Millisecond, // test duration
		3,                   // num workers
	)
	if err != nil {
		t.Fatalf("Failed to start stress test worker: %v", err)
	}

	// Let it run for a bit
	time.Sleep(150 * time.Millisecond)

	// Stop the worker
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop stress test worker: %v", err)
	}

	// Verify worker is stopped
	stats := cli.GetWorkerStats()
	activeCount, _ := stats["active"].(int)
	if activeCount != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", activeCount)
	}
}

func TestStressTestWorkerSessionID(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start two stress test workers
	workerID1, err := cli.StartStressTestWorker(
		[]string{"test-transaction-1"},
		10,
		10*time.Millisecond,
		10*time.Millisecond,
		1,
	)
	if err != nil {
		t.Fatalf("Failed to start worker 1: %v", err)
	}
	defer func() {
		_ = cli.StopWorker(workerID1)
	}()

	workerID2, err := cli.StartStressTestWorker(
		[]string{"test-transaction-2"},
		10,
		10*time.Millisecond,
		10*time.Millisecond,
		1,
	)
	if err != nil {
		t.Fatalf("Failed to start worker 2: %v", err)
	}
	defer func() {
		_ = cli.StopWorker(workerID2)
	}()

	cli.mu.Lock()
	worker1 := cli.stressWorkers[workerID1]
	worker2 := cli.stressWorkers[workerID2]
	cli.mu.Unlock()

	if worker1 == nil || worker2 == nil {
		t.Fatalf("Workers not found in map")
	}

	if worker1.sessionID == "" {
		t.Errorf("Worker 1 sessionID is empty")
	}
	if worker2.sessionID == "" {
		t.Errorf("Worker 2 sessionID is empty")
	}

	if worker1.sessionID == worker2.sessionID {
		t.Errorf("Workers share the same sessionID: %s", worker1.sessionID)
	}
}

func TestStopAllWorkersCleanup(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start multiple workers
	workerID1, err := cli.StartWorker("test-transaction-1", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to start worker 1: %v", err)
	}

	workerID2, err := cli.StartWorker("test-transaction-2", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to start worker 2: %v", err)
	}

	stressWorkerID, err := cli.StartStressTestWorker(
		[]string{"test-transaction-3"},
		10,
		100*time.Millisecond,
		200*time.Millisecond,
		1,
	)
	if err != nil {
		t.Fatalf("Failed to start stress test worker: %v", err)
	}

	// Verify all workers are running
	stats := cli.GetWorkerStats()
	activeCount, _ := stats["active"].(int)
	if activeCount != 3 {
		t.Errorf("Expected 3 active workers, got %d", activeCount)
	}

	// Stop all workers immediately to avoid executing background logic
	err = cli.StopAllWorkers()
	if err != nil {
		t.Fatalf("Failed to stop all workers: %v", err)
	}

	// Verify all workers are stopped
	stats = cli.GetWorkerStats()
	activeCount, _ = stats["active"].(int)
	if activeCount != 0 {
		t.Errorf("Expected 0 active workers after stop all, got %d", activeCount)
	}

	// Verify workers are removed from maps
	cli.mu.Lock()
	_, exists1 := cli.workers[workerID1]
	_, exists2 := cli.workers[workerID2]
	_, exists3 := cli.stressWorkers[stressWorkerID]
	cli.mu.Unlock()

	if exists1 || exists2 || exists3 {
		t.Error("Some workers still exist in maps after StopAllWorkers")
	}
}

func TestCLICloseWaitsForWorkers(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start a worker
	_, err := cli.StartWorker("test-transaction", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Verify worker is running before close
	stats := cli.GetWorkerStats()
	activeCount, _ := stats["active"].(int)
	if activeCount != 1 {
		t.Errorf("Expected 1 active worker before Close, got %d", activeCount)
	}

	// Close CLI (should wait for workers)
	cli.Close()

	// Verify no workers remain after close
	stats = cli.GetWorkerStats()
	activeCount, _ = stats["active"].(int)
	if activeCount != 0 {
		t.Errorf("Expected 0 active workers after Close, got %d", activeCount)
	}

	// Verify maps are cleared
	cli.mu.Lock()
	workerCount := len(cli.workers) + len(cli.stressWorkers)
	cli.mu.Unlock()
	if workerCount != 0 {
		t.Errorf("Expected 0 workers in maps after Close, got %d", workerCount)
	}
}

func TestWorkerStopTimeout(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a worker that doesn't respond to cancellation quickly
	workerID := "test-timeout"
	ctx, cancel := context.WithCancel(context.Background())

	worker := &workerInfo{
		id:           workerID,
		name:         "test",
		count:        1,
		interval:     100 * time.Millisecond,
		startTime:    time.Now(),
		ctx:          ctx,
		cancel:       cancel,
		networkStats: cli.networkStats,
	}

	// Add worker to map
	cli.mu.Lock()
	cli.workers[workerID] = worker
	cli.mu.Unlock()

	// Start a goroutine that ignores cancellation for a while
	worker.wg.Add(1)
	go func() {
		defer worker.wg.Done()
		select {
		case <-ctx.Done():
			// Simulate slow cleanup
			time.Sleep(100 * time.Millisecond)
		case <-time.After(2 * time.Second):
			// Fallback timeout
		}
	}()

	// Stop worker (should timeout after 5 seconds)
	start := time.Now()
	err := cli.StopWorker(workerID)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Should complete within reasonable time (less than 6 seconds due to timeout)
	if duration > 6*time.Second {
		t.Errorf("Worker stop took too long: %v", duration)
	}

	// Worker should be removed despite timeout
	cli.mu.Lock()
	_, exists := cli.workers[workerID]
	cli.mu.Unlock()
	if exists {
		t.Error("Worker still exists in map after timeout stop")
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sorted   []time.Duration
		pct      float64
		expected time.Duration
	}{
		{
			name:     "empty slice",
			sorted:   []time.Duration{},
			pct:      0.5,
			expected: 0,
		},
		{
			name:     "pct <= 0.0",
			sorted:   []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			pct:      -0.1,
			expected: 10 * time.Millisecond,
		},
		{
			name:     "pct >= 1.0",
			sorted:   []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			pct:      1.1,
			expected: 20 * time.Millisecond,
		},
		{
			name:     "single element",
			sorted:   []time.Duration{15 * time.Millisecond},
			pct:      0.5,
			expected: 15 * time.Millisecond,
		},
		{
			name:     "median exact match (odd count)",
			sorted:   []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond},
			pct:      0.5,
			expected: 20 * time.Millisecond,
		},
		{
			name:     "median interpolation (even count)",
			sorted:   []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			pct:      0.5,
			expected: 15 * time.Millisecond,
		},
		{
			name:     "90th percentile interpolation",
			sorted:   []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond},
			pct:      0.9,
			expected: 28 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual := percentile(tc.sorted, tc.pct)
			if actual != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, actual)
			}
		})
	}
}

func TestStressTestWorkerIntervalAndMaxPending(t *testing.T) {
	t.Parallel()

	cli := NewCLI()

	// Create a dummy spec file
	spec := `{"name": "Test Spec", "fields": {"0": {"type": "String", "length": 4, "description": "MTI", "enc": "ASCII", "prefix": "ASCII.Fixed"}}}`
	tmpFile, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp spec file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
		_ = tmpFile.Close()
	}()
	_, _ = tmpFile.WriteString(spec)

	svc, err := service.NewService(
		"localhost", "9999", tmpFile.Name(), false, 0, 5*time.Second, 10*time.Second, 5*time.Second,
	)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	cli.svc = svc

	sendCmd := &command.SendCommand{
		Svc: svc,
	}
	cli.commands["send"] = sendCmd

	// Set original pending requests to 100
	svc.SetMaxPendingRequests(100)

	// Start stress test worker with target TPS of 200
	workerID, err := cli.StartStressTestWorker(
		[]string{"test-transaction"},
		200,                 // target TPS
		10*time.Millisecond, // ramp up duration
		10*time.Millisecond, // test duration
		2,                   // num workers
	)
	if err != nil {
		t.Fatalf("Failed to start stress test worker: %v", err)
	}

	// Verify that max pending requests was increased to at least 400
	if svc.GetMaxPendingRequests() < 400 {
		t.Errorf("Expected max pending requests to be scaled to at least 400, got %d", svc.GetMaxPendingRequests())
	}

	// Stop the worker
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop stress test worker: %v", err)
	}

	// Give worker goroutine brief moment to run finishAndPrintSummary
	time.Sleep(50 * time.Millisecond)

	// Verify that max pending requests was restored to 100
	if svc.GetMaxPendingRequests() != 100 {
		t.Errorf("Expected max pending requests to be restored to 100, got %d", svc.GetMaxPendingRequests())
	}
}
