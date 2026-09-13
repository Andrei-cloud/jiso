// query_tx.go reads the transactions of a session: the list, the count, one by id,
// and the aggregate stats the dashboard shows. The scan helpers at the bottom are
// the one place a transaction row becomes an EnrichedTransactionRecord, so the
// column order is written down once.
package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// GetApprovedVisaTransactions returns approved transactions for a given session.
func GetApprovedVisaTransactions(sessionID string) ([]*EnrichedTransactionRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT id, session_id, timestamp, transaction_name, request_json, response_json,
		       processing_time_ms, success, response_code, tx_file_path, tx_file_name,
		       spec_path, spec_name, request_raw_hex, response_raw_hex
		FROM transactions
		WHERE session_id = ?
		  AND success = 1
		  AND (response_code = '00' OR response_code = '000' OR response_code = '' OR response_code IS NULL)
		ORDER BY id ASC
	`

	var results []*EnrichedTransactionRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []any{sessionID},
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

// GetSessionTransactions returns all transactions executed within a session
func GetSessionTransactions(sessionID string) ([]*EnrichedTransactionRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

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
		Args: []any{sessionID},
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
	connMu.Lock()
	defer connMu.Unlock()

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
		Args: []any{txID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rec = scanTransactionRecord(stmt)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("transaction ID %d not found: %w", txID, ErrTransactionNotFound)
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
func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// deriveResponseCode derives the response code from response JSON. It reads
// only the top-level fields["39"]: a raw scan for `"39"` could match a
// subfield key inside a composite field (e.g. DE55) before the real
// response code and attribute the wrong code to the transaction.
func deriveResponseCode(responseJSON *string) string {
	if responseJSON == nil {
		return "91" // Timeout
	}

	s := *responseJSON
	if s == "" {
		return "XX"
	}

	var response struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal([]byte(s), &response); err != nil || response.Fields == nil {
		return "XX" // Unknown/error
	}

	raw, ok := response.Fields["39"]
	if !ok {
		return "XX"
	}

	var code string
	if err := json.Unmarshal(raw, &code); err == nil {
		return code
	}

	return "XX" // Default unknown
}

// GetTransactionStats returns statistics for the current session with transaction consistency
func GetTransactionStats(sessionID string) (map[string]any, error) {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	stats := make(map[string]any)

	// Use a read transaction for consistency
	err := sqlitex.ExecuteTransient(dbConn, "BEGIN", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin read transaction: %w", err)
	}
	defer func() {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
	}()

	totalCount, err := querySessionCount(sessionID, "SELECT COUNT(*) FROM transactions WHERE session_id = ?")
	if err != nil {
		return nil, err
	}

	successCount, err := querySessionCount(sessionID, "SELECT COUNT(*) FROM transactions WHERE session_id = ? AND success = 1")
	if err != nil {
		return nil, err
	}

	avgProcessingTime, err := querySessionFloat(sessionID, "SELECT AVG(processing_time_ms) FROM transactions WHERE session_id = ? AND processing_time_ms > 0")
	if err != nil {
		return nil, err
	}

	responseCodes, err := queryResponseCodes(sessionID)
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

// querySessionCount runs a COUNT(*) query scoped to sessionID and returns the
// scalar. It uses the global connection and must be called inside an active read
// transaction.
func querySessionCount(sessionID, query string) (int, error) {
	var count int
	err := sqlitex.ExecuteTransient(dbConn, query, &sqlitex.ExecOptions{
		Args: []any{sessionID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = int(stmt.ColumnInt64(0))

			return nil
		},
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}

// querySessionFloat runs a single-column float aggregate scoped to sessionID.
func querySessionFloat(sessionID, query string) (float64, error) {
	var value float64
	err := sqlitex.ExecuteTransient(dbConn, query, &sqlitex.ExecOptions{
		Args: []any{sessionID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			value = stmt.ColumnFloat(0)

			return nil
		},
	})
	if err != nil {
		return 0, err
	}

	return value, nil
}

// queryResponseCodes returns the response-code -> count distribution for sessionID.
func queryResponseCodes(sessionID string) (map[string]int, error) {
	responseCodes := make(map[string]int)
	err := sqlitex.ExecuteTransient(
		dbConn,
		"SELECT response_code, COUNT(*) FROM transactions WHERE session_id = ? GROUP BY response_code",
		&sqlitex.ExecOptions{
			Args: []any{sessionID},
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

	return responseCodes, nil
}

// ErrTransactionNotFound is the sentinel a session-DB lookup returns when no row
// carries the id. The lookup wraps it with the id it could not find, so callers
// match this with errors.Is rather than reading the message -- comparing text
// would stop working the moment the wrap was added.
var ErrTransactionNotFound = errors.New("transaction not found")

// GetApprovedVisaTransactions returns approved transactions for a given session.
