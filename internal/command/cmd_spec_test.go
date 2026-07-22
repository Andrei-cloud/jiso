package command

import (
	"path/filepath"
	"testing"

	cfg "jiso/internal/config"
	"jiso/internal/service"
)

func TestSpecCommand_SetArgsAndExecute(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")

	svc, err := service.NewService("localhost", "9999", specPath, false, 1, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	cmd := &SpecCommand{Svc: svc}
	cmd.SetArgs([]string{specPath})

	if cmd.SpecPath != specPath {
		t.Errorf("Expected SpecPath %s, got %s", specPath, cmd.SpecPath)
	}

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if cfg.GetConfig().GetSpec() != specPath {
		t.Errorf("Expected config spec %s, got %s", specPath, cfg.GetConfig().GetSpec())
	}
}

func TestSpecCommand_InvalidPath(t *testing.T) {
	cmd := &SpecCommand{SpecPath: "non_existent_spec.json"}
	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error for non-existent spec file, got nil")
	}
}
