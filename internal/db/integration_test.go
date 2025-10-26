package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEndToEndIntegration tests the complete database integration
func TestEndToEndIntegration(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "integration_test.db")

	// Initialize database
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer Close()

	sessionID := "test-integration-session"

	// Test inserting transactions
	requestJSON := `{"mti":"0200","fields":{"2":"4111111111111111","3":"000000","4":"000000010000","7":"0101120000","11":"000001","37":"000000000001","41":"12345678","43":"Test Terminal"}}`
	responseJSON := `{"mti":"0210","fields":{"2":"4111111111111111","3":"000000","4":"000000010000","7":"0101120000","11":"000001","37":"000000000001","39":"00","41":"12345678","43":"Test Terminal"}}`

	// Insert successful transaction
	err = InsertTransaction(sessionID, "Purchase", requestJSON, &responseJSON, 150, true)
	if err != nil {
		t.Fatalf("Failed to insert successful transaction: %v", err)
	}

	// Insert failed transaction (timeout)
	err = InsertTransaction(sessionID, "Failed Purchase", requestJSON, nil, 0, false)
	if err != nil {
		t.Fatalf("Failed to insert failed transaction: %v", err)
	}

	// Insert another successful transaction with different response code
	responseJSON2 := `{"mti":"0210","fields":{"39":"05"}}`
	err = InsertTransaction(sessionID, "Declined Purchase", requestJSON, &responseJSON2, 200, false)
	if err != nil {
		t.Fatalf("Failed to insert declined transaction: %v", err)
	}

	// Get stats
	stats, err := GetTransactionStats(sessionID)
	if err != nil {
		t.Fatalf("Failed to get transaction stats: %v", err)
	}

	// Verify stats
	if stats["total_transactions"] != 3 {
		t.Errorf("Expected 3 total transactions, got %v", stats["total_transactions"])
	}

	if stats["successful_transactions"] != 1 {
		t.Errorf("Expected 1 successful transaction, got %v", stats["successful_transactions"])
	}

	if stats["failed_transactions"] != 2 {
		t.Errorf("Expected 2 failed transactions, got %v", stats["failed_transactions"])
	}

	if stats["average_processing_time_ms"] != 175.0 {
		t.Errorf(
			"Expected average processing time of 175.0 ms, got %v",
			stats["average_processing_time_ms"],
		)
	}

	// Check response code distribution
	responseCodes, ok := stats["response_code_distribution"].(map[string]int)
	if !ok {
		t.Fatal("Response code distribution not found or wrong type")
	}

	if responseCodes["00"] != 1 {
		t.Errorf("Expected 1 transaction with response code 00, got %v", responseCodes["00"])
	}

	if responseCodes["05"] != 1 {
		t.Errorf("Expected 1 transaction with response code 05, got %v", responseCodes["05"])
	}

	if responseCodes["91"] != 1 {
		t.Errorf(
			"Expected 1 transaction with response code 91 (timeout), got %v",
			responseCodes["91"],
		)
	}

	// Verify database file exists and has content
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Database file was not created")
	}

	// Check file size is greater than 0 (has data)
	fileInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Failed to get file info: %v", err)
	}

	if fileInfo.Size() == 0 {
		t.Fatal("Database file is empty")
	}
}
