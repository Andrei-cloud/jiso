// write.go is the write side of the session database: recording a session, the
// transactions it produced, and a finished stress run's summary. Everything here
// takes the connection lock itself; reads are in the query files.
package db

import (
	"fmt"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"zombiezen.com/go/sqlite/sqlitex"

	"jiso/internal/utils"
)

// UpsertSession creates or updates a session record in SQLite
func UpsertSession(sessionID, specPath, specName, txFilePath, txFileName, host, port, connType, headerType, status string, tlsEnabled bool) error {
	connMu.Lock()
	defer connMu.Unlock()

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
		Args: []any{sessionID, specPath, specName, txFilePath, txFileName, host, port, connType, headerType, tlsEnabled, status},
	})
	if err != nil {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
		return err
	}
	return sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
}

// touchSessionLocked stamps a session active. The caller must already hold
// connMu.
func touchSessionLocked(sessionID string) error {
	if dbConn == nil {
		return nil
	}
	sql := `UPDATE sessions SET last_active_time = CURRENT_TIMESTAMP WHERE session_id = ?`
	return sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []any{sessionID},
	})
}

// InsertStressTestSummary records a stress test summary in SQLite
func InsertStressTestSummary(rec *StressTestSummaryRecord) error {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	_ = touchSessionLocked(rec.SessionID)

	err := sqlitex.ExecuteTransient(dbConn, "BEGIN IMMEDIATE", nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	sql := `INSERT INTO stress_tests (
		session_id, worker_id, start_time, end_time, target_tps, concurrency,
		total_duration_ms, total_transactions, successful_transactions, failed_transactions,
		avg_tps, peak_tps, min_latency_ms, max_latency_ms, mean_latency_ms,
		p50_latency_ms, p90_latency_ms, p95_latency_ms, p99_latency_ms,
		transactions_json, response_codes_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	err = sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []any{
			rec.SessionID, rec.WorkerID, rec.StartTime.Format("2006-01-02 15:04:05"), rec.EndTime.Format("2006-01-02 15:04:05"),
			rec.TargetTPS, rec.Concurrency, rec.TotalDurationMs, rec.TotalTransactions, rec.SuccessfulTransactions, rec.FailedTransactions,
			rec.AverageTPS, rec.PeakTPS, rec.MinLatencyMs, rec.MaxLatencyMs, rec.MeanLatencyMs,
			rec.P50LatencyMs, rec.P90LatencyMs, rec.P95LatencyMs, rec.P99LatencyMs,
			rec.TransactionsJSON, rec.ResponseCodesJSON,
		},
	})
	if err != nil {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil)
		return fmt.Errorf("failed to insert stress test summary: %w", err)
	}

	return sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
}

// InsertTransactionEnriched inserts a full enriched transaction record. It is
// the synchronous write path used by the async logger's fallback (the TUI
// never inits the async logger), so the whole BEGIN...COMMIT runs under connMu
// against the shared conn.
func InsertTransactionEnriched(rec *EnrichedTransactionRecord) error {
	connMu.Lock()
	defer connMu.Unlock()

	return insertTransactionEnrichedLocked(rec)
}

// insertTransactionEnrichedLocked is the conn-touched body of
// InsertTransactionEnriched. The caller must already hold connMu.
func insertTransactionEnrichedLocked(rec *EnrichedTransactionRecord) error {
	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	if rec.ResponseCode == "" {
		rec.ResponseCode = deriveResponseCode(rec.ResponseJSON)
	}

	_ = touchSessionLocked(rec.SessionID)

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
		Args: []any{
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

// MessageToJSONWithSpec converts an ISO8583 message to JSON string using the provided spec for composite fields
func MessageToJSONWithSpec(msg *iso8583.Message, spec *iso8583.MessageSpec) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("message is nil")
	}

	// Get MTI
	mti, err := msg.GetMTI()
	if err != nil {
		return "", fmt.Errorf("failed to get MTI: %w", err)
	}

	// Extract all fields and subfields based on spec
	fields := utils.ExtractMessageFields(msg, spec)

	// Create JSON structure
	messageData := map[string]any{
		"mti":    mti,
		"fields": fields,
	}

	jsonBytes, err := json.Marshal(messageData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message to JSON: %w", err)
	}

	return string(jsonBytes), nil
}
