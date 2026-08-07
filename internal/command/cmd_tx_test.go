package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "jiso/internal/config"
	"jiso/internal/service"
)

func TestTxCommand_SetArgsAndExecute(t *testing.T) {
	cfg.GetConfig().Reset()

	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	tmpDir := t.TempDir()
	txPath := filepath.Join(tmpDir, "transaction.json")

	dummyTxContent := `[
		{
			"type": "transaction",
			"name": "TEST_TX",
			"description": "Test Transaction",
			"fields": {}
		}
	]`
	if err := os.WriteFile(txPath, []byte(dummyTxContent), 0644); err != nil {
		t.Fatalf("Failed to create dummy transaction file: %v", err)
	}

	svc, err := service.NewService("localhost", "9999", specPath, false, 1, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}
	cfg.GetConfig().SetSpec(specPath)

	cmd := &TxCommand{Svc: svc}
	cmd.SetArgs([]string{txPath})

	if cmd.TxPath != txPath {
		t.Errorf("Expected TxPath %s, got %s", txPath, cmd.TxPath)
	}

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if cfg.GetConfig().GetFile() != txPath {
		t.Errorf("Expected config file %s, got %s", txPath, cfg.GetConfig().GetFile())
	}
}

func TestTxCommand_InvalidPath(t *testing.T) {
	cfg.GetConfig().Reset()
	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	cfg.GetConfig().SetSpec(specPath)

	cmd := &TxCommand{TxPath: "non_existent_tx.json"}
	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error for non-existent transaction file, got nil")
	}
}

func TestTxCommand_RequiresSpecBeforeLoadingTransactions(t *testing.T) {
	cfg.GetConfig().Reset()

	tmpDir := t.TempDir()
	txPath := filepath.Join(tmpDir, "transaction.json")
	dummyTxContent := `[
		{
			"type": "transaction",
			"name": "TEST_TX",
			"description": "Test Transaction",
			"fields": {}
		}
	]`
	if err := os.WriteFile(txPath, []byte(dummyTxContent), 0o644); err != nil {
		t.Fatalf("Failed to create dummy transaction file: %v", err)
	}

	cmd := &TxCommand{TxPath: txPath}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when specification is not selected, got nil")
	}
	if got := err.Error(); got == "" || !containsIgnoreCase(got, "specification must be defined") {
		t.Fatalf("Expected missing specification error, got: %v", err)
	}
}

func containsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
