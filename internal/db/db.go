package db

import (
	"encoding/json"
	"fmt"

	"github.com/moov-io/iso8583"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var dbConn *sqlite.Conn

// InitDB initializes the database connection and creates tables
func InitDB(dbPath string) error {
	if dbPath == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	dbConn = conn

	// Create tables
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}

// InitDBWithConn initializes with an existing connection (for testing)
func InitDBWithConn(conn *sqlite.Conn) error {
	dbConn = conn
	return createTables()
}

// Close closes the database connection
func Close() error {
	if dbConn != nil {
		return dbConn.Close()
	}
	return nil
}

// createTables creates the necessary database tables
func createTables() error {
	// Create transactions table
	createTableSQL := `CREATE TABLE IF NOT EXISTS transactions (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, transaction_name TEXT, request_json TEXT, response_json TEXT, processing_time_ms INTEGER, success BOOLEAN, response_code TEXT)`

	if err := sqlitex.ExecuteTransient(dbConn, createTableSQL, nil); err != nil {
		return err
	}

	// Create indexes
	indexSQL1 := `CREATE INDEX IF NOT EXISTS idx_session_timestamp ON transactions(session_id, timestamp)`
	if err := sqlitex.ExecuteTransient(dbConn, indexSQL1, nil); err != nil {
		return err
	}

	indexSQL2 := `CREATE INDEX IF NOT EXISTS idx_response_code ON transactions(response_code)`
	return sqlitex.ExecuteTransient(dbConn, indexSQL2, nil)
}

// InsertTransaction inserts a new transaction record with proper transaction handling
func InsertTransaction(
	sessionID, txName, requestJSON string,
	responseJSON *string,
	processingTimeMs int,
	success bool,
) error {
	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	// Derive response code from response JSON
	responseCode := deriveResponseCode(responseJSON)

	// Use a transaction for atomicity
	err := sqlitex.ExecuteTransient(dbConn, "BEGIN IMMEDIATE", nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	insertSQL := `
		INSERT INTO transactions (
			session_id, transaction_name, request_json, response_json, 
			processing_time_ms, success, response_code
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	err = sqlitex.ExecuteTransient(dbConn, insertSQL, &sqlitex.ExecOptions{
		Args: []interface{}{
			sessionID, txName, requestJSON, derefOrNil(responseJSON), processingTimeMs, success, responseCode,
		},
	})
	if err != nil {
		// Rollback on error
		sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
		return fmt.Errorf("failed to insert transaction: %w", err)
	}

	err = sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// derefOrNil dereferences a string pointer or returns nil if it's nil
func derefOrNil(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// deriveResponseCode derives the response code from response JSON
func deriveResponseCode(responseJSON *string) string {
	if responseJSON == nil {
		return "91" // Timeout
	}

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(*responseJSON), &response); err != nil {
		return "XX" // Unknown/error
	}

	if fields, ok := response["fields"].(map[string]interface{}); ok {
		if code, ok := fields["39"].(string); ok {
			return code
		}
	}

	return "XX" // Default unknown
}

// MessageToJSON converts an ISO8583 message to JSON string
func MessageToJSON(msg *iso8583.Message) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("message is nil")
	}

	// Get MTI
	mti, err := msg.GetMTI()
	if err != nil {
		return "", fmt.Errorf("failed to get MTI: %w", err)
	}

	// Get all fields
	fields := make(map[string]interface{})
	for i := 2; i <= 128; i++ { // Skip MTI (0) and bitmap (1)
		if field := msg.GetField(i); field != nil {
			if str, err := field.String(); err == nil && str != "" {
				fields[fmt.Sprintf("%d", i)] = str
			}
		}
	}

	// Create JSON structure
	messageData := map[string]interface{}{
		"mti":    mti,
		"fields": fields,
	}

	jsonBytes, err := json.Marshal(messageData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message to JSON: %w", err)
	}

	return string(jsonBytes), nil
}

// GetTransactionStats returns statistics for the current session with transaction consistency
func GetTransactionStats(sessionID string) (map[string]interface{}, error) {
	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	stats := make(map[string]interface{})

	// Use a read transaction for consistency
	err := sqlitex.ExecuteTransient(dbConn, "BEGIN", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin read transaction: %w", err)
	}
	defer sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil) // Rollback if not committed

	// Get total count
	var totalCount int
	err = sqlitex.ExecuteTransient(
		dbConn,
		"SELECT COUNT(*) FROM transactions WHERE session_id = ?",
		&sqlitex.ExecOptions{
			Args: []interface{}{sessionID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				totalCount = int(stmt.ColumnInt64(0))
				return nil
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Get success count
	var successCount int
	err = sqlitex.ExecuteTransient(
		dbConn,
		"SELECT COUNT(*) FROM transactions WHERE session_id = ? AND success = 1",
		&sqlitex.ExecOptions{
			Args: []interface{}{sessionID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				successCount = int(stmt.ColumnInt64(0))
				return nil
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Get average processing time
	var avgProcessingTime float64
	err = sqlitex.ExecuteTransient(
		dbConn,
		"SELECT AVG(processing_time_ms) FROM transactions WHERE session_id = ? AND processing_time_ms > 0",
		&sqlitex.ExecOptions{
			Args: []interface{}{sessionID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				avgProcessingTime = stmt.ColumnFloat(0)
				return nil
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Get response code distribution
	responseCodes := make(map[string]int)
	err = sqlitex.ExecuteTransient(
		dbConn,
		"SELECT response_code, COUNT(*) FROM transactions WHERE session_id = ? GROUP BY response_code",
		&sqlitex.ExecOptions{
			Args: []interface{}{sessionID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				code := stmt.ColumnText(0)
				count := int(stmt.ColumnInt64(1))
				responseCodes[code] = count
				return nil
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Commit the read transaction
	err = sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to commit read transaction: %w", err)
	}

	stats["total_transactions"] = totalCount
	stats["successful_transactions"] = successCount
	stats["failed_transactions"] = totalCount - successCount
	stats["average_processing_time_ms"] = avgProcessingTime
	stats["response_code_distribution"] = responseCodes

	return stats, nil
}
