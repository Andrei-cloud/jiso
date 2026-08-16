package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"jiso/internal/utils"
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
	defer func() {
		_ = Close()
	}()

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
	defer func() {
		_ = Close()
	}()

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
	defer func() {
		_ = Close()
	}()

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
	defer func() {
		_ = Close()
	}()

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

func TestStressTestSummaryLogging(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stress.db")

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		_ = Close()
	}()

	sessionID := "sess-stress-999"
	_ = UpsertSession(sessionID, "specs/spec.json", "spec.json", "tx.json", "tx.json", "127.0.0.1", "8080", "CLIENT", "2-byte", "active", false)

	stRec := &StressTestSummaryRecord{
		SessionID:              sessionID,
		WorkerID:               "wrk-1",
		StartTime:              time.Now(),
		EndTime:                time.Now().Add(10 * time.Second),
		TargetTPS:              50,
		Concurrency:            2,
		TotalDurationMs:        10000,
		TotalTransactions:      500,
		SuccessfulTransactions: 490,
		FailedTransactions:     10,
		AverageTPS:             49.0,
		PeakTPS:                52.5,
		MinLatencyMs:           2.5,
		MaxLatencyMs:           45.0,
		MeanLatencyMs:          8.5,
		P50LatencyMs:           7.0,
		P90LatencyMs:           15.0,
		P95LatencyMs:           20.0,
		P99LatencyMs:           35.0,
		TransactionsJSON:       `["Purchase","Balance"]`,
		ResponseCodesJSON:      `{"00":490,"05":10}`,
	}

	if err := InsertStressTestSummary(stRec); err != nil {
		t.Fatalf("InsertStressTestSummary failed: %v", err)
	}

	summaries, err := GetSessionStressTestSummaries(sessionID)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("GetSessionStressTestSummaries failed: %v, len: %d", err, len(summaries))
	}
	if summaries[0].TargetTPS != 50 || summaries[0].TotalTransactions != 500 {
		t.Errorf("Unexpected summary record: %+v", summaries[0])
	}
}


func TestVisaSessionsAndApprovedTransactions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_visa.db")

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		_ = Close()
	}()

	visaSessionID := "sess-visa-001"
	otherSessionID := "sess-other-002"

	_ = UpsertSession(visaSessionID, "specs/visa.json", "visa.json", "tx.json", "tx.json", "127.0.0.1", "9000", "CLIENT", "Visa", "active", false)
	_ = UpsertSession(otherSessionID, "specs/spec.json", "spec.json", "tx.json", "tx.json", "127.0.0.1", "8080", "CLIENT", "2-byte", "active", false)

	resp00 := `{"mti":"0110","fields":{"38":"123456","39":"00"}}`
	resp05 := `{"mti":"0110","fields":{"38":"000000","39":"05"}}`

	_ = InsertTransactionEnriched(&EnrichedTransactionRecord{
		SessionID:    visaSessionID,
		TxName:       "Visa Auth Approved",
		Success:      true,
		ResponseCode: "00",
		RequestJSON:  `{"mti":"0100","fields":{"2":"4000000000000002","4":"10000"}}`,
		ResponseJSON: &resp00,
	})

	_ = InsertTransactionEnriched(&EnrichedTransactionRecord{
		SessionID:    visaSessionID,
		TxName:       "Visa Auth Declined",
		Success:      false,
		ResponseCode: "05",
		RequestJSON:  `{"mti":"0100","fields":{"2":"4000000000000002","4":"20000"}}`,
		ResponseJSON: &resp05,
	})

	_ = InsertTransactionEnriched(&EnrichedTransactionRecord{
		SessionID:    otherSessionID,
		TxName:       "Generic Tx",
		Success:      true,
		ResponseCode: "00",
		RequestJSON:  `{"mti":"0200","fields":{"2":"5000000000000001","4":"10000"}}`,
		ResponseJSON: &resp00,
	})

	visaSessions, err := GetVisaSessions()
	if err != nil {
		t.Fatalf("GetVisaSessions failed: %v", err)
	}
	if len(visaSessions) != 1 || visaSessions[0].SessionID != visaSessionID {
		t.Fatalf("Expected 1 visa session (%s), got: %d", visaSessionID, len(visaSessions))
	}
	if visaSessions[0].SuccessCount != 1 || visaSessions[0].FailedCount != 1 {
		t.Errorf("Unexpected visa session counts: success=%d, failed=%d", visaSessions[0].SuccessCount, visaSessions[0].FailedCount)
	}

	approvedTxs, err := GetApprovedVisaTransactions(visaSessionID)
	if err != nil {
		t.Fatalf("GetApprovedVisaTransactions failed: %v", err)
	}
	if len(approvedTxs) != 1 || approvedTxs[0].TxName != "Visa Auth Approved" {
		t.Fatalf("Expected 1 approved tx, got: %d", len(approvedTxs))
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestMessageToJSONWithSpec_CompositeFields(t *testing.T) {
	// 1. Create a spec with composite field 62 (subfields 1 and 2)
	spec := &iso8583.MessageSpec{
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{
				Length:      4,
				Description: "MTI",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Length:      8,
				Description: "Bitmap",
				Enc:         encoding.Binary,
				Pref:        prefix.Binary.Fixed,
			}),
			2: field.NewString(&field.Spec{
				Length:      16,
				Description: "PAN",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			62: field.NewComposite(&field.Spec{
				Length:      255,
				Description: "Custom Payment Service Fields",
				Pref:        prefix.Binary.Fixed,
				Bitmap:      field.NewBitmap(&field.Spec{Length: 1, Description: "Field 62.0 Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed, DisableAutoExpand: true}),
				Subfields: map[string]field.Field{
					"1": field.NewString(&field.Spec{
						Length:      1,
						Description: "ACI",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
					"2": field.NewString(&field.Spec{
						Length:      15,
						Description: "Transaction Identifier",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
				},
			}),
		},
	}

	msg := iso8583.NewMessage(spec)
	msg.MTI("0100")
	require.NoError(t, msg.Field(2, "4085652009074000"))

	// Pack composite field 62 using utils.SetCompositeFieldValue
	compData := map[string]interface{}{
		"1": "A",
		"2": "466215320236000",
	}
	require.NoError(t, utils.SetCompositeFieldValue(msg, spec, 62, compData))

	// 2. Call MessageToJSONWithSpec
	jsonStr, err := MessageToJSONWithSpec(msg, spec)
	require.NoError(t, err)
	require.NotEmpty(t, jsonStr)

	// 3. Verify JSON parses into structured map with subfields preserved
	var parsed struct {
		MTI    string                 `json:"mti"`
		Fields map[string]interface{} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
	assert.Equal(t, "0100", parsed.MTI)
	assert.Equal(t, "4085652009074000", parsed.Fields["2"])

	f62, ok := parsed.Fields["62"].(map[string]interface{})
	require.True(t, ok, "Field 62 in JSON should be a structured map of subfields, got: %T (%v)", parsed.Fields["62"], parsed.Fields["62"])
	assert.Equal(t, "A", f62["1"])
	assert.Equal(t, "466215320236000", f62["2"])
}


