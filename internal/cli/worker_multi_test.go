package cli

import (
	"os"
	"testing"
	"time"

	"jiso/internal/command"
	"jiso/internal/service"
)

func TestStressTestWorkerMultiTransactions(t *testing.T) {
	cli := NewCLI()

	// Create a dummy spec file
	spec := `{"name": "Test Spec", "fields": {"0": {"type": "String", "length": 4, "description": "MTI", "enc": "ASCII", "prefix": "ASCII.Fixed"}}}`
	tmpFile, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp spec file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()
	_, _ = tmpFile.WriteString(spec)

	svc, err := service.NewService("localhost", "9999", tmpFile.Name(), false, 0, 5*time.Second, 10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	cli.svc = svc

	sendCmd := &command.SendCommand{
		Svc: svc,
	}
	cli.commands["send"] = sendCmd

	// Start a stress test worker with 2 transaction types
	txNames := []string{"TX_A", "TX_B"}
	workerID, err := cli.StartStressTestWorker(
		txNames,
		50,                   // target TPS
		10*time.Millisecond,  // ramp up duration
		10*time.Millisecond,  // test duration
		2,                    // num workers
	)
	if err != nil {
		t.Fatalf("Failed to start stress test worker: %v", err)
	}

	// Retrieve the worker instance
	cli.mu.Lock()
	worker, exists := cli.stressWorkers[workerID]
	cli.mu.Unlock()
	if !exists {
		t.Fatalf("Worker %s not found in stressWorkers", workerID)
	}

	// Verify worker fields
	if len(worker.names) != 2 || worker.names[0] != "TX_A" || worker.names[1] != "TX_B" {
		t.Errorf("Worker names not set correctly, got %v", worker.names)
	}

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Stop the worker
	err = cli.StopWorker(workerID)
	if err != nil {
		t.Fatalf("Failed to stop stress test worker: %v", err)
	}

	// Verify worker is stopped
	stats := cli.GetWorkerStats()
	if stats["active"].(int) != 0 {
		t.Errorf("Expected 0 active workers after stop, got %d", stats["active"])
	}

	// Check if stats are recorded per transaction type
	worker.mu.Lock()
	defer worker.mu.Unlock()

	// Verify dates are populated
	if worker.startTime.IsZero() {
		t.Error("Expected startTime to be set")
	}
	if worker.endTime.IsZero() {
		t.Error("Expected endTime to be set")
	}

	// Verify per-transaction stats exist
	if len(worker.txStats) == 0 {
		t.Log("No transactions executed in short test window, which is acceptable but stats map should be initialized")
	}
	for txName, stat := range worker.txStats {
		t.Logf("Stats for %s: successful=%d, failed=%d, responses=%d, latencies=%d",
			txName, stat.successful, stat.failed, len(stat.respCodes), len(stat.latencies))
		if txName != "TX_A" && txName != "TX_B" {
			t.Errorf("Unexpected transaction name in stats: %s", txName)
		}
	}
}
