package service

import (
	"os"
	"testing"
	"time"
)

func createTempSpecFile(t *testing.T) string {
	spec := `{
		"name": "Test Spec",
		"fields": {
			"0": {
				"type": "String",
				"length": 4,
				"description": "Message Type Indicator",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			}
		}
	}`

	file, err := os.CreateTemp("", "spec.json")
	if err != nil {
		t.Fatalf("Failed to create temp spec file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(spec)
	if err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	return file.Name()
}

func TestNewService(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.Address != "localhost:8080" {
		t.Errorf("Expected address 'localhost:8080', got '%s'", service.Address)
	}

	if service.MessageSpec == nil {
		t.Error("MessageSpec is nil")
	}

	if service.connManager == nil {
		t.Error("connManager is nil")
	}

	if service.networkStats == nil {
		t.Error("networkStats is nil")
	}

	if service.debugMode != false {
		t.Error("debugMode should be false")
	}
}

func TestServiceGetters(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		true,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if !service.debugMode {
		t.Error("debugMode should be true")
	}

	if service.GetSpec() != service.MessageSpec {
		t.Error("GetSpec should return MessageSpec")
	}

	if service.GetNetworkingStats() != service.networkStats {
		t.Error("GetNetworkingStats should return networkStats")
	}
}

func TestServiceIsConnected(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should not be connected initially
	if service.IsConnected() {
		t.Error("Service should not be connected initially")
	}
}

func TestServiceClose(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Close should not error even if not connected
	err = service.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestServiceDisconnect(t *testing.T) {
	specFile := createTempSpecFile(t)
	defer os.Remove(specFile)

	service, err := NewService(
		"localhost",
		"8080",
		specFile,
		false,
		3,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Disconnect should not error even if not connected
	err = service.Disconnect()
	if err != nil {
		t.Errorf("Disconnect failed: %v", err)
	}
}
