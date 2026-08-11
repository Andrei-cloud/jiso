package db

import (
	"fmt"
	"time"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var dbConn *sqlite.Conn

type SessionRecord struct {
	SessionID        string    `json:"session_id"`
	StartTime        time.Time `json:"start_time"`
	LastActiveTime   time.Time `json:"last_active_time"`
	SpecPath         string    `json:"spec_path"`
	SpecName         string    `json:"spec_name"`
	TxFilePath       string    `json:"tx_file_path"`
	TxFileName       string    `json:"tx_file_name"`
	Host             string    `json:"host,omitempty"`
	Port             string    `json:"port,omitempty"`
	ConnectionType   string    `json:"connection_type,omitempty"`
	HeaderType       string    `json:"header_type,omitempty"`
	TLSEnabled       bool      `json:"tls_enabled,omitempty"`
	Status           string    `json:"status"`
	TransactionCount int       `json:"transaction_count,omitempty"`
	SuccessCount     int       `json:"success_count,omitempty"`
	FailedCount      int       `json:"failed_count,omitempty"`
}

type EnrichedTransactionRecord struct {
	ID               int64     `json:"id"`
	SessionID        string    `json:"session_id"`
	Timestamp        time.Time `json:"timestamp"`
	TxName           string    `json:"transaction_name"`
	TxFilePath       string    `json:"tx_file_path"`
	TxFileName       string    `json:"tx_file_name"`
	SpecPath         string    `json:"spec_path"`
	SpecName         string    `json:"spec_name"`
	RequestJSON      string    `json:"request_json"`
	ResponseJSON     *string   `json:"response_json"`
	RequestRawHEX    string    `json:"request_raw_hex,omitempty"`
	ResponseRawHEX   *string   `json:"response_raw_hex,omitempty"`
	ProcessingTimeMs int       `json:"processing_time_ms"`
	Success          bool      `json:"success"`
	ResponseCode     string    `json:"response_code"`
}

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
	// Create sessions table
	createSessionsSQL := `CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY,
		start_time DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_active_time DATETIME DEFAULT CURRENT_TIMESTAMP,
		spec_path TEXT,
		spec_name TEXT,
		tx_file_path TEXT,
		tx_file_name TEXT,
		host TEXT,
		port TEXT,
		connection_type TEXT,
		header_type TEXT,
		tls_enabled BOOLEAN,
		status TEXT
	)`
	if err := sqlitex.ExecuteTransient(dbConn, createSessionsSQL, nil); err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	sessColumns := []struct{ name, def string }{
		{"host", "TEXT"},
		{"port", "TEXT"},
		{"connection_type", "TEXT"},
		{"header_type", "TEXT"},
		{"tls_enabled", "BOOLEAN"},
	}
	for _, c := range sessColumns {
		_ = sqlitex.ExecuteTransient(dbConn, fmt.Sprintf("ALTER TABLE sessions ADD COLUMN %s %s", c.name, c.def), nil)
	}

	// Create transactions table
	createTableSQL := `CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		transaction_name TEXT,
		request_json TEXT,
		response_json TEXT,
		processing_time_ms INTEGER,
		success BOOLEAN,
		response_code TEXT,
		tx_file_path TEXT,
		tx_file_name TEXT,
		spec_path TEXT,
		spec_name TEXT,
		request_raw_hex TEXT,
		response_raw_hex TEXT
	)`

	if err := sqlitex.ExecuteTransient(dbConn, createTableSQL, nil); err != nil {
		return err
	}

	// Ensure missing columns exist in pre-existing transactions table
	columns := []struct{ name, def string }{
		{"tx_file_path", "TEXT"},
		{"tx_file_name", "TEXT"},
		{"spec_path", "TEXT"},
		{"spec_name", "TEXT"},
		{"request_raw_hex", "TEXT"},
		{"response_raw_hex", "TEXT"},
	}
	for _, c := range columns {
		_ = sqlitex.ExecuteTransient(dbConn, fmt.Sprintf("ALTER TABLE transactions ADD COLUMN %s %s", c.name, c.def), nil)
	}

	// Create indexes
	indexSQL1 := `CREATE INDEX IF NOT EXISTS idx_session_timestamp ON transactions(session_id, timestamp)`
	if err := sqlitex.ExecuteTransient(dbConn, indexSQL1, nil); err != nil {
		return err
	}

	indexSQL2 := `CREATE INDEX IF NOT EXISTS idx_response_code ON transactions(response_code)`
	return sqlitex.ExecuteTransient(dbConn, indexSQL2, nil)
}

// UpsertSession creates or updates a session record in SQLite
func UpsertSession(sessionID, specPath, specName, txFilePath, txFileName, host, port, connType, headerType, status string, tlsEnabled bool) error {
	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	sql := `INSERT INTO sessions (
		session_id, spec_path, spec_name, tx_file_path, tx_file_name,
		host, port, connection_type, header_type, tls_enabled, status,
		start_time, last_active_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(session_id) DO UPDATE SET
		spec_path=CASE WHEN excluded.spec_path != '' THEN excluded.spec_path ELSE sessions.spec_path END,
		spec_name=CASE WHEN excluded.spec_name != '' THEN excluded.spec_name ELSE sessions.spec_name END,
		tx_file_path=CASE WHEN excluded.tx_file_path != '' THEN excluded.tx_file_path ELSE sessions.tx_file_path END,
		tx_file_name=CASE WHEN excluded.tx_file_name != '' THEN excluded.tx_file_name ELSE sessions.tx_file_name END,
		host=CASE WHEN excluded.host != '' THEN excluded.host ELSE sessions.host END,
		port=CASE WHEN excluded.port != '' THEN excluded.port ELSE sessions.port END,
		connection_type=CASE WHEN excluded.connection_type != '' THEN excluded.connection_type ELSE sessions.connection_type END,
		header_type=CASE WHEN excluded.header_type != '' THEN excluded.header_type ELSE sessions.header_type END,
		tls_enabled=excluded.tls_enabled,
		status=CASE WHEN excluded.status != '' THEN excluded.status ELSE sessions.status END,
		last_active_time=CURRENT_TIMESTAMP`

	err := sqlitex.ExecuteTransient(dbConn, "BEGIN IMMEDIATE", nil)
	if err != nil {
		return err
	}
	err = sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{sessionID, specPath, specName, txFilePath, txFileName, host, port, connType, headerType, tlsEnabled, status},
	})
	if err != nil {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
		return err
	}
	return sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
}

// UpdateSessionConnection updates connection details for an active session
func UpdateSessionConnection(sessionID, connType, host, port, headerType string, tlsEnabled bool) error {
	if dbConn == nil {
		return nil
	}
	sql := `UPDATE sessions SET
		connection_type = CASE WHEN ? != '' THEN ? ELSE connection_type END,
		host = CASE WHEN ? != '' THEN ? ELSE host END,
		port = CASE WHEN ? != '' THEN ? ELSE port END,
		header_type = CASE WHEN ? != '' THEN ? ELSE header_type END,
		tls_enabled = ?,
		last_active_time = CURRENT_TIMESTAMP
		WHERE session_id = ?`
	return sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{connType, connType, host, host, port, port, headerType, headerType, tlsEnabled, sessionID},
	})
}

// TouchSession updates the last active timestamp of a session
func TouchSession(sessionID string) error {
	if dbConn == nil {
		return nil
	}
	sql := `UPDATE sessions SET last_active_time = CURRENT_TIMESTAMP WHERE session_id = ?`
	return sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{sessionID},
	})
}

// InsertTransaction inserts a transaction with basic parameters
func InsertTransaction(
	sessionID, txName, requestJSON string,
	responseJSON *string,
	processingTimeMs int,
	success bool,
) error {
	return InsertTransactionEnriched(&EnrichedTransactionRecord{
		SessionID:        sessionID,
		TxName:           txName,
		RequestJSON:      requestJSON,
		ResponseJSON:     responseJSON,
		ProcessingTimeMs: processingTimeMs,
		Success:          success,
	})
}

// InsertTransactionEnriched inserts a full enriched transaction record
func InsertTransactionEnriched(rec *EnrichedTransactionRecord) error {
	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	if rec.ResponseCode == "" {
		rec.ResponseCode = deriveResponseCode(rec.ResponseJSON)
	}

	_ = TouchSession(rec.SessionID)

	err := sqlitex.ExecuteTransient(dbConn, "BEGIN IMMEDIATE", nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	insertSQL := `
		INSERT INTO transactions (
			session_id, transaction_name, request_json, response_json, 
			processing_time_ms, success, response_code,
			tx_file_path, tx_file_name, spec_path, spec_name,
			request_raw_hex, response_raw_hex
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	err = sqlitex.ExecuteTransient(dbConn, insertSQL, &sqlitex.ExecOptions{
		Args: []interface{}{
			rec.SessionID, rec.TxName, rec.RequestJSON, derefOrNil(rec.ResponseJSON),
			rec.ProcessingTimeMs, rec.Success, rec.ResponseCode,
			rec.TxFilePath, rec.TxFileName, rec.SpecPath, rec.SpecName,
			rec.RequestRawHEX, derefOrNil(rec.ResponseRawHEX),
		},
	})
	if err != nil {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
		return fmt.Errorf("failed to insert transaction: %w", err)
	}

	return sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
}

// GetSessionsList fetches all recorded sessions from SQLite
func GetSessionsList() ([]*SessionRecord, error) {
	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT s.session_id, s.start_time, s.last_active_time, s.spec_path, s.spec_name, s.tx_file_path, s.tx_file_name, s.status,
		       s.host, s.port, s.connection_type, s.header_type, s.tls_enabled,
		       COUNT(t.id) as total_tx,
		       SUM(CASE WHEN t.success = 1 THEN 1 ELSE 0 END) as success_tx,
		       SUM(CASE WHEN t.success = 0 THEN 1 ELSE 0 END) as failed_tx
		FROM sessions s
		LEFT JOIN transactions t ON s.session_id = t.session_id
		GROUP BY s.session_id
		ORDER BY s.start_time DESC
	`

	var results []*SessionRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rec := &SessionRecord{
				SessionID:        stmt.ColumnText(0),
				SpecPath:         stmt.ColumnText(3),
				SpecName:         stmt.ColumnText(4),
				TxFilePath:       stmt.ColumnText(5),
				TxFileName:       stmt.ColumnText(6),
				Status:           stmt.ColumnText(7),
				Host:             stmt.ColumnText(8),
				Port:             stmt.ColumnText(9),
				ConnectionType:   stmt.ColumnText(10),
				HeaderType:       stmt.ColumnText(11),
				TLSEnabled:       stmt.ColumnBool(12),
				TransactionCount: int(stmt.ColumnInt64(13)),
				SuccessCount:     int(stmt.ColumnInt64(14)),
				FailedCount:      int(stmt.ColumnInt64(15)),
			}
			rec.StartTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(1))
			rec.LastActiveTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(2))
			results = append(results, rec)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetSessionByID returns a specific session record
func GetSessionByID(sessionID string) (*SessionRecord, error) {
	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT s.session_id, s.start_time, s.last_active_time, s.spec_path, s.spec_name, s.tx_file_path, s.tx_file_name, s.status,
		       s.host, s.port, s.connection_type, s.header_type, s.tls_enabled,
		       COUNT(t.id) as total_tx,
		       SUM(CASE WHEN t.success = 1 THEN 1 ELSE 0 END) as success_tx,
		       SUM(CASE WHEN t.success = 0 THEN 1 ELSE 0 END) as failed_tx
		FROM sessions s
		LEFT JOIN transactions t ON s.session_id = t.session_id
		WHERE s.session_id = ?
		GROUP BY s.session_id
	`

	var rec *SessionRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{sessionID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rec = &SessionRecord{
				SessionID:        stmt.ColumnText(0),
				SpecPath:         stmt.ColumnText(3),
				SpecName:         stmt.ColumnText(4),
				TxFilePath:       stmt.ColumnText(5),
				TxFileName:       stmt.ColumnText(6),
				Status:           stmt.ColumnText(7),
				Host:             stmt.ColumnText(8),
				Port:             stmt.ColumnText(9),
				ConnectionType:   stmt.ColumnText(10),
				HeaderType:       stmt.ColumnText(11),
				TLSEnabled:       stmt.ColumnBool(12),
				TransactionCount: int(stmt.ColumnInt64(13)),
				SuccessCount:     int(stmt.ColumnInt64(14)),
				FailedCount:      int(stmt.ColumnInt64(15)),
			}
			rec.StartTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(1))
			rec.LastActiveTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(2))
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if rec == nil {
		rec = &SessionRecord{
			SessionID: sessionID,
			Status:    "active",
		}
	}
	return rec, nil
}


// GetSessionTransactions returns all transactions executed within a session
func GetSessionTransactions(sessionID string) ([]*EnrichedTransactionRecord, error) {
	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT id, session_id, timestamp, transaction_name, request_json, response_json,
		       processing_time_ms, success, response_code, tx_file_path, tx_file_name,
		       spec_path, spec_name, request_raw_hex, response_raw_hex
		FROM transactions
		WHERE session_id = ?
		ORDER BY id ASC
	`

	var results []*EnrichedTransactionRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{sessionID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			results = append(results, scanTransactionRecord(stmt))
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetTransactionByID returns a single transaction by ID
func GetTransactionByID(txID int64) (*EnrichedTransactionRecord, error) {
	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT id, session_id, timestamp, transaction_name, request_json, response_json,
		       processing_time_ms, success, response_code, tx_file_path, tx_file_name,
		       spec_path, spec_name, request_raw_hex, response_raw_hex
		FROM transactions
		WHERE id = ?
	`

	var rec *EnrichedTransactionRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []interface{}{txID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rec = scanTransactionRecord(stmt)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("transaction ID %d not found", txID)
	}
	return rec, nil
}

func scanTransactionRecord(stmt *sqlite.Stmt) *EnrichedTransactionRecord {
	rec := &EnrichedTransactionRecord{
		ID:               stmt.ColumnInt64(0),
		SessionID:        stmt.ColumnText(1),
		TxName:           stmt.ColumnText(3),
		RequestJSON:      stmt.ColumnText(4),
		ProcessingTimeMs: int(stmt.ColumnInt64(6)),
		Success:          stmt.ColumnBool(7),
		ResponseCode:     stmt.ColumnText(8),
		TxFilePath:       stmt.ColumnText(9),
		TxFileName:       stmt.ColumnText(10),
		SpecPath:         stmt.ColumnText(11),
		SpecName:         stmt.ColumnText(12),
		RequestRawHEX:    stmt.ColumnText(13),
	}
	rec.Timestamp, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(2))
	if !stmt.ColumnIsNull(5) {
		respJSON := stmt.ColumnText(5)
		rec.ResponseJSON = &respJSON
	}
	if !stmt.ColumnIsNull(14) {
		respHex := stmt.ColumnText(14)
		rec.ResponseRawHEX = &respHex
	}
	return rec
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
	defer func() {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
	}()

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

