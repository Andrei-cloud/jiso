package cli

import (
	"context"
	"testing"
	"time"

	"jiso/internal/command"
)

func TestNewCLI(t *testing.T) {
	cli := NewCLI()
	if cli == nil {
		t.Fatal("NewCLI returned nil")
	}

	if cli.commands == nil {
		t.Error("commands map not initialized")
	}

	if cli.workers == nil {
		t.Error("workers map not initialized")
	}

	if cli.networkStats == nil {
		t.Error("networkStats not initialized")
	}
}

func TestAddCommand(t *testing.T) {
	cli := NewCLI()

	// Create a mock command
	mockCmd := &mockCommand{name: "test"}

	cli.AddCommand(mockCmd)

	if len(cli.commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(cli.commands))
	}

	if cli.commands["test"] != mockCmd {
		t.Error("Command not added correctly")
	}
}

func TestWorkerResourceCleanup(t *testing.T) {
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
	if stats["active"].(int) != 1 {
		t.Errorf("Expected 1 active worker, got %d", stats["active"])
	}

	// Stop the worker
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify worker is stopped
	stats = cli.GetWorkerStats()
	if stats["active"].(int) != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", stats["active"])
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
	cli := NewCLI()

	// Create a real send command with nil dependencies for testing
	sendCmd := &command.SendCommand{}
	cli.commands["send"] = sendCmd

	// Start a stress test worker with short duration
	workerID, err := cli.StartStressTestWorker(
		"test-transaction",
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
	if stats["active"].(int) != 1 {
		t.Errorf("Expected 1 active worker, got %d", stats["active"])
	}

	// Stop the worker immediately to avoid executing background logic
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop stress test worker: %v", err)
	}

	// Verify worker is stopped
	stats = cli.GetWorkerStats()
	if stats["active"].(int) != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", stats["active"])
	}
}

func TestStopAllWorkersCleanup(t *testing.T) {
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
		"test-transaction-3",
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
	if stats["active"].(int) != 3 {
		t.Errorf("Expected 3 active workers, got %d", stats["active"])
	}

	// Stop all workers immediately to avoid executing background logic
	err = cli.StopAllWorkers()
	if err != nil {
		t.Fatalf("Failed to stop all workers: %v", err)
	}

	// Verify all workers are stopped
	stats = cli.GetWorkerStats()
	if stats["active"].(int) != 0 {
		t.Errorf("Expected 0 active workers after stop all, got %d", stats["active"])
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
	if stats["active"].(int) != 1 {
		t.Errorf("Expected 1 active worker before Close, got %d", stats["active"])
	}

	// Close CLI (should wait for workers)
	cli.Close()

	// Verify no workers remain after close
	stats = cli.GetWorkerStats()
	if stats["active"].(int) != 0 {
		t.Errorf("Expected 0 active workers after Close, got %d", stats["active"])
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

// Mock command for testing
type mockCommand struct {
	name string
}

func (m *mockCommand) Name() string {
	return m.name
}

func (m *mockCommand) Synopsis() string {
	return "mock synopsis"
}

func (m *mockCommand) Execute() error {
	return nil
}

type mockSendCommand struct {
	*command.SendCommand
}

func (m *mockSendCommand) ExecuteBackground(name string) error {
	// Don't call the real method, just simulate some work
	time.Sleep(1 * time.Millisecond)
	return nil
}
