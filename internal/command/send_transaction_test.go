package command

import (
	"os"
	"testing"
	"time"

	"jiso/internal/metrics"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
	"jiso/internal/view"
)

func createTestSpecFile(t *testing.T) string {
	spec := `{
		"name": "Test Spec",
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
			"11": {
				"type": "String",
				"length": 6,
				"description": "Systems Trace Audit Number (STAN)",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"39": {
				"type": "String",
				"length": 2,
				"description": "Response Code",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"70": {
				"type": "Numeric",
				"length": 3,
				"description": "Network Management Information Code",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed",
				"padding": {
					"type": "Left",
					"pad": "0"
				}
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

func createTestTransactionFile(t *testing.T) string {
	transactions := `[
		{
			"name": "test",
			"description": "Test transaction",
			"fields": {
				"0": "0800",
				"11": "123456",
				"70": "001"
			},
			"dataset": []
		}
	]`

	file, err := os.CreateTemp("", "transactions.json")
	if err != nil {
		t.Fatalf("Failed to create temp transaction file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(transactions)
	if err != nil {
		t.Fatalf("Failed to write transaction file: %v", err)
	}

	return file.Name()
}

func TestExecuteBackground(t *testing.T) {
	specFile := createTestSpecFile(t)
	defer os.Remove(specFile)

	transactionFile := createTestTransactionFile(t)
	defer os.Remove(transactionFile)

	spec, err := utils.CreateSpecFromFile(specFile)
	if err != nil {
		t.Fatalf("Failed to load spec: %v", err)
	}

	// Create transaction repository
	tc, err := transactions.NewTransactionCollection(transactionFile, spec)
	if err != nil {
		t.Fatalf("Failed to create transaction collection: %v", err)
	}

	// Create service
	svc, err := service.NewService(
		"localhost",
		"9999",
		specFile,
		false,
		0,
		5*time.Second,
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Create command
	cmd := &SendCommand{
		Tc:           tc,
		Svc:          svc,
		stats:        metrics.NewTransactionStats(),
		networkStats: metrics.NewNetworkingStats(),
		renderer:     view.NewISOMessageRenderer(nil),
	}

	// Since no real server, test the offline case
	err = cmd.ExecuteBackground("test")
	if err != nil {
		t.Errorf("ExecuteBackground should not error when offline, got %v", err)
	}
}
