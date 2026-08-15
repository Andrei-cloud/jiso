package command

import (
	"os"
	"testing"

	"jiso/internal/command/templates"
	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

func createTempSpec(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "spec_*.json")
	if err != nil {
		t.Fatalf("failed to create temp spec: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to write temp spec: %v", err)
	}
	return f.Name()
}

func createTempTx(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "tx_*.json")
	if err != nil {
		t.Fatalf("failed to create temp tx: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to write temp tx: %v", err)
	}
	return f.Name()
}

func TestSpecCommand_SetArgsAndExecute(t *testing.T) {
	specPath := createTempSpec(t, string(templates.DefaultSpecJSON))

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
	t.Parallel()

	cmd := &SpecCommand{SpecPath: "non_existent_spec.json"}
	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error for non-existent spec file, got nil")
	}
}

func TestSpecCommand_UpdatesTransactionCollectionSpec(t *testing.T) {
	asciiSpecContent := `{
		"fields": {
			"0": {
				"type": "String",
				"length": 4,
				"description": "Message Type Indicator",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"1": {
				"type": "Bitmap",
				"length": 8,
				"description": "Bitmap",
				"enc": "Binary",
				"prefix": "Hex.Fixed"
			},
			"70": {
				"type": "String",
				"length": 3,
				"description": "NM Code",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			}
		}
	}`

	bcdSpecContent := `{
		"fields": {
			"0": {
				"type": "String",
				"length": 4,
				"description": "Message Type Indicator",
				"enc": "BCD",
				"prefix": "BCD.Fixed"
			},
			"1": {
				"type": "Bitmap",
				"length": 8,
				"description": "Bitmap",
				"enc": "Binary",
				"prefix": "Hex.Fixed"
			},
			"70": {
				"type": "String",
				"length": 3,
				"description": "NM Code",
				"enc": "BCD",
				"prefix": "BCD.Fixed"
			}
		}
	}`

	txContent := `[
		{
			"type": "transaction",
			"name": "Echo",
			"fields": {
				"0": "0800",
				"70": "301"
			}
		}
	]`

	defaultSpecPath := createTempSpec(t, asciiSpecContent)
	bcdSpecPath := createTempSpec(t, bcdSpecContent)
	txPath := createTempTx(t, txContent)

	defaultSpec, err := utils.CreateSpecFromFile(defaultSpecPath)
	if err != nil {
		t.Fatalf("failed to load default spec: %v", err)
	}

	tc, err := transactions.NewTransactionCollection(txPath, defaultSpec)
	if err != nil {
		t.Fatalf("failed to load transaction collection: %v", err)
	}

	// Message before spec update (uses asciiSpec -> ASCII MTI '30383030')
	msg1, err := tc.Compose("Echo")
	if err != nil {
		t.Fatalf("failed to compose Echo: %v", err)
	}
	packed1, err := msg1.Pack()
	if err != nil {
		t.Fatalf("failed to pack msg1: %v", err)
	}
	if string(packed1[:4]) != "0800" {
		t.Errorf("expected ASCII 0800 for asciiSpec, got hex %x", packed1[:4])
	}

	// Run SpecCommand for bcdSpec
	specCmd := &SpecCommand{
		SpecPath: bcdSpecPath,
		Tc:       tc,
	}
	if err := specCmd.Execute(); err != nil {
		t.Fatalf("SpecCommand.Execute failed: %v", err)
	}

	// Message after spec update (uses bcdSpec -> BCD MTI 0x08 0x00)
	msg2, err := tc.Compose("Echo")
	if err != nil {
		t.Fatalf("failed to compose Echo after spec update: %v", err)
	}
	packed2, err := msg2.Pack()
	if err != nil {
		t.Fatalf("failed to pack msg2: %v", err)
	}
	if packed2[0] != 0x08 || packed2[1] != 0x00 {
		t.Errorf("expected BCD 0x08 0x00 for bcdSpec, got hex %x", packed2[:2])
	}
}
