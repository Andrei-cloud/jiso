package db

import (
	"os"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestInitDB(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Database file was not created")
	}

	// Verify tables were created
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadOnly)
	if err != nil {
		t.Fatalf("Failed to open database for verification: %v", err)
	}
	defer conn.Close()

	// Check if transactions table exists
	var tableCount int
	err = sqlitex.ExecuteTransient(
		conn,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='transactions'",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				tableCount = int(stmt.ColumnInt64(0))
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}

	if tableCount != 1 {
		t.Fatalf("Expected 1 transactions table, got %d", tableCount)
	}
}

func TestInsertTransaction(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer Close()

	sessionID := "test-session-123"
	txName := "Test Transaction"
	requestJSON := `{"mti":"0200","fields":{"2":"1234567890123456"}}`
	responseJSON := `{"mti":"0210","fields":{"39":"00"}}`
	processingTimeMs := 150
	success := true

	// Insert transaction
	err = InsertTransaction(
		sessionID,
		txName,
		requestJSON,
		&responseJSON,
		processingTimeMs,
		success,
	)
	if err != nil {
		t.Fatalf("Failed to insert transaction: %v", err)
	}

	// Verify transaction was inserted
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadOnly)
	if err != nil {
		t.Fatalf("Failed to open database for verification: %v", err)
	}
	defer conn.Close()

	var count int
	var storedResponseJSON string
	err = sqlitex.ExecuteTransient(
		conn,
		"SELECT COUNT(*), response_json FROM transactions WHERE session_id = ?",
		&sqlitex.ExecOptions{
			Args: []interface{}{sessionID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				count = int(stmt.ColumnInt64(0))
				storedResponseJSON = stmt.ColumnText(1)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("Failed to query transaction: %v", err)
	}

	if count != 1 {
		t.Fatalf("Expected 1 transaction, got %d", count)
	}

	if storedResponseJSON != responseJSON {
		t.Errorf("Expected response_json to be %q, got %q", responseJSON, storedResponseJSON)
	}
}

func TestDeriveResponseCode(t *testing.T) {
	tests := []struct {
		name         string
		responseJSON *string
		expected     string
	}{
		{
			name:         "nil response (timeout)",
			responseJSON: nil,
			expected:     "91",
		},
		{
			name:         "invalid JSON",
			responseJSON: stringPtr("invalid json"),
			expected:     "XX",
		},
		{
			name:         "valid response with code 00",
			responseJSON: stringPtr(`{"mti":"0210","fields":{"39":"00"}}`),
			expected:     "00",
		},
		{
			name:         "valid response with code 05",
			responseJSON: stringPtr(`{"mti":"0210","fields":{"39":"05"}}`),
			expected:     "05",
		},
		{
			name:         "response without fields",
			responseJSON: stringPtr(`{"mti":"0210"}`),
			expected:     "XX",
		},
		{
			name:         "response without response code field",
			responseJSON: stringPtr(`{"mti":"0210","fields":{}}`),
			expected:     "XX",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveResponseCode(tt.responseJSON)
			if result != tt.expected {
				t.Errorf("deriveResponseCode() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetTransactionStats(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer Close()

	sessionID := "test-session-stats"

	// Insert some test transactions
	transactions := []struct {
		txName           string
		requestJSON      string
		responseJSON     *string
		processingTimeMs int
		success          bool
	}{
		{"Tx1", `{"mti":"0200"}`, stringPtr(`{"mti":"0210","fields":{"39":"00"}}`), 100, true},
		{"Tx2", `{"mti":"0200"}`, stringPtr(`{"mti":"0210","fields":{"39":"00"}}`), 200, true},
		{"Tx3", `{"mti":"0200"}`, stringPtr(`{"mti":"0210","fields":{"39":"05"}}`), 150, false},
		{"Tx4", `{"mti":"0200"}`, nil, 0, false}, // Timeout
	}

	for _, tx := range transactions {
		err = InsertTransaction(
			sessionID,
			tx.txName,
			tx.requestJSON,
			tx.responseJSON,
			tx.processingTimeMs,
			tx.success,
		)
		if err != nil {
			t.Fatalf("Failed to insert transaction: %v", err)
		}
	}

	// Get stats
	stats, err := GetTransactionStats(sessionID)
	if err != nil {
		t.Fatalf("Failed to get transaction stats: %v", err)
	}

	// Verify stats
	if stats["total_transactions"] != 4 {
		t.Errorf("Expected 4 total transactions, got %v", stats["total_transactions"])
	}

	if stats["successful_transactions"] != 2 {
		t.Errorf("Expected 2 successful transactions, got %v", stats["successful_transactions"])
	}

	if stats["failed_transactions"] != 2 {
		t.Errorf("Expected 2 failed transactions, got %v", stats["failed_transactions"])
	}

	// Check response code distribution
	responseCodes, ok := stats["response_code_distribution"].(map[string]int)
	if !ok {
		t.Fatal("Response code distribution not found or wrong type")
	}

	if responseCodes["00"] != 2 {
		t.Errorf("Expected 2 transactions with response code 00, got %v", responseCodes["00"])
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
}

func TestEnrichedSessionsAndTransactions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_enriched.db")

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer Close()

	sessionID := "sess-enrich-123"
	specPath := "specs/spec.json"
	specName := "spec.json"
	txPath := "transactions/transactions.json"
	txName := "transactions.json"

	if err := UpsertSession(sessionID, specPath, specName, txPath, txName, "127.0.0.1", "8080", "CLIENT", "2-byte", "active", true); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	sessions, err := GetSessionsList()
	if err != nil || len(sessions) == 0 {
		t.Fatalf("GetSessionsList failed: %v, len: %d", err, len(sessions))
	}
	if sessions[0].SessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, sessions[0].SessionID)
	}
	if sessions[0].Host != "127.0.0.1" || sessions[0].Port != "8080" || sessions[0].ConnectionType != "CLIENT" || !sessions[0].TLSEnabled {
		t.Errorf("Unexpected session connection details: %+v", sessions[0])
	}


	rec := &EnrichedTransactionRecord{
		SessionID:        sessionID,
		TxName:           "Purchase Test",
		TxFilePath:       txPath,
		TxFileName:       txName,
		SpecPath:         specPath,
		SpecName:         specName,
		RequestJSON:      `{"mti":"0200","fields":{"3":"000000"}}`,
		ResponseJSON:     stringPtr(`{"mti":"0210","fields":{"39":"00"}}`),
		ProcessingTimeMs: 120,
		Success:          true,
	}

	if err := InsertTransactionEnriched(rec); err != nil {
		t.Fatalf("InsertTransactionEnriched failed: %v", err)
	}

	txs, err := GetSessionTransactions(sessionID)
	if err != nil || len(txs) != 1 {
		t.Fatalf("GetSessionTransactions failed: %v, count: %d", err, len(txs))
	}

	fetched, err := GetTransactionByID(txs[0].ID)
	if err != nil {
		t.Fatalf("GetTransactionByID failed: %v", err)
	}
	if fetched.TxName != "Purchase Test" {
		t.Errorf("Expected TxName 'Purchase Test', got %s", fetched.TxName)
	}
}

func stringPtr(s string) *string {
	return &s
}
