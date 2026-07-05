package command

import (
	"os"
	"testing"
	"time"

	"jiso/internal/service"
)

type mockWorkerController struct {
	names          []string
	targetTps      int
	rampUpDuration time.Duration
	duration       time.Duration
	numWorkers     int
	startCalled    bool
}

func (m *mockWorkerController) StartWorker(name string, count int, interval time.Duration) (string, error) {
	return "", nil
}

func (m *mockWorkerController) StartStressTestWorker(
	names []string,
	targetTps int,
	rampUpDuration time.Duration,
	duration time.Duration,
	numWorkers int,
) (string, error) {
	m.names = names
	m.targetTps = targetTps
	m.rampUpDuration = rampUpDuration
	m.duration = duration
	m.numWorkers = numWorkers
	m.startCalled = true
	return "test-worker", nil
}

func (m *mockWorkerController) StopWorker(id string) error {
	return nil
}

func (m *mockWorkerController) StopAllWorkers() error {
	return nil
}

func (m *mockWorkerController) GetWorkerStats() map[string]interface{} {
	return nil
}

func TestStressTestCommandOffline(t *testing.T) {
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

	cmd := &StressTestCommand{
		Svc: svc,
		Wrk: &mockWorkerController{},
	}

	err = cmd.Execute()
	if err == nil {
		t.Error("Expected error when executing stress command offline, got nil")
	}
}

func TestStressTestCommandInfo(t *testing.T) {
	cmd := &StressTestCommand{}
	if cmd.Name() != "stress" {
		t.Errorf("Expected name 'stress', got '%s'", cmd.Name())
	}
	if cmd.Synopsis() == "" {
		t.Error("Expected synopsis, got empty string")
	}
}
