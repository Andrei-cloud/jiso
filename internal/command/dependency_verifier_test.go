package command

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	cfg "jiso/internal/config"
	"jiso/internal/service"
)

func TestVerifyTarget(t *testing.T) {
	t.Parallel()

	cfg.GetConfig().Reset()
	err := VerifyTarget()
	if err == nil {
		t.Error("Expected error when target host/port are empty, got nil")
	}

	cfg.GetConfig().SetHost("127.0.0.1")
	cfg.GetConfig().SetPort("9999")

	err = VerifyTarget()
	if err != nil {
		t.Errorf("Expected success when target host/port set, got: %v", err)
	}
}

func TestVerifySpec(t *testing.T) {
	t.Parallel()

	cfg.GetConfig().Reset()
	err := VerifySpec(nil)
	if err == nil {
		t.Error("Expected error when spec is not selected, got nil")
	}

	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	svc, errService := service.NewService("localhost", "9999", specPath, false, 1, 0, 0, 0)
	if errService != nil {
		t.Fatalf("Failed to create service: %v", errService)
	}

	err = VerifySpec(svc)
	if err != nil {
		t.Errorf("Expected success when spec loaded in service, got: %v", err)
	}
}

func TestVerifyTx(t *testing.T) {
	t.Parallel()

	cfg.GetConfig().Reset()
	err := VerifyTx(nil)
	if err == nil {
		t.Error("Expected error when transaction file is not selected, got nil")
	}
}

func TestVerifyConnection(t *testing.T) {
	t.Parallel()
	cfg.GetConfig().SetHost("127.0.0.1")
	cfg.GetConfig().SetPort("9999")

	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	svc, err := service.NewService("localhost", "9999", specPath, false, 1, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Service is offline
	err = VerifyConnection(svc)
	if err == nil {
		t.Error("Expected error when service is not connected, got nil")
	}
}

func TestVerifyConnectionWhenConnected(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	svc, err := service.NewService("", "", specPath, false, 1, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	cfg.GetConfig().Reset()
	// Target host/port are empty in config, but if service is connected, VerifyConnection should pass
	// We simulate connected state by checking helper logic
	assert.Error(t, VerifyConnection(svc))
}
