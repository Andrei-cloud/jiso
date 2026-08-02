package command

import (
	"path/filepath"
	"testing"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
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

func TestSpecCommand_UpdatesTransactionCollectionSpec(t *testing.T) {
	defaultSpecPath, _ := filepath.Abs(filepath.Join("..", "..", "specs", "spec.json"))
	visaSpecPath, _ := filepath.Abs(filepath.Join("..", "..", "specs", "visa.json"))
	txPath, _ := filepath.Abs(filepath.Join("..", "..", "transactions", "transaction.json"))

	defaultSpec, err := utils.CreateSpecFromFile(defaultSpecPath)
	if err != nil {
		t.Fatalf("failed to load default spec: %v", err)
	}

	tc, err := transactions.NewTransactionCollection(txPath, defaultSpec)
	if err != nil {
		t.Fatalf("failed to load transaction collection: %v", err)
	}

	// Message before spec update (uses spec.json -> ASCII MTI '30383030')
	msg1, err := tc.Compose("Echo")
	if err != nil {
		t.Fatalf("failed to compose Echo: %v", err)
	}
	packed1, err := msg1.Pack()
	if err != nil {
		t.Fatalf("failed to pack msg1: %v", err)
	}
	if string(packed1[:4]) != "0800" { // ASCII '0800' is 30 38 30 30 bytes, i.e. string "0800"
		t.Errorf("expected ASCII 0800 for spec.json, got hex %x", packed1[:4])
	}

	// Run SpecCommand for visa.json
	specCmd := &SpecCommand{
		SpecPath: visaSpecPath,
		Tc:       tc,
	}
	if err := specCmd.Execute(); err != nil {
		t.Fatalf("SpecCommand.Execute failed: %v", err)
	}

	// Message after spec update (uses visa.json -> BCD MTI 0x08 0x00)
	msg2, err := tc.Compose("Echo")
	if err != nil {
		t.Fatalf("failed to compose Echo after spec update: %v", err)
	}
	packed2, err := msg2.Pack()
	if err != nil {
		t.Fatalf("failed to pack msg2: %v", err)
	}
	if packed2[0] != 0x08 || packed2[1] != 0x00 {
		t.Errorf("expected BCD 0x08 0x00 for visa.json, got hex %x", packed2[:2])
	}
}
