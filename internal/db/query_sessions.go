// query_sessions.go reads sessions and their stress-run summaries. Every query
// takes the connection lock, so a caller never has to know which handle it is
// reading through, and every query returns the record types from db.go rather than
// raw rows.
package db

import (
	"errors"
	"fmt"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// GetSessionStressTestSummaries returns all stress test summaries recorded for a session
func GetSessionStressTestSummaries(sessionID string) ([]*StressTestSummaryRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `SELECT id, session_id, worker_id, start_time, end_time, target_tps, concurrency,
	               total_duration_ms, total_transactions, successful_transactions, failed_transactions,
	               avg_tps, peak_tps, min_latency_ms, max_latency_ms, mean_latency_ms,
	               p50_latency_ms, p90_latency_ms, p95_latency_ms, p99_latency_ms,
	               transactions_json, response_codes_json
	        FROM stress_tests
	        WHERE session_id = ?
	        ORDER BY id ASC`

	var results []*StressTestSummaryRecord
	err := sqlitex.ExecuteTransient(dbConn, sql, &sqlitex.ExecOptions{
		Args: []any{sessionID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			rec := &StressTestSummaryRecord{
				ID:                     stmt.ColumnInt64(0),
				SessionID:              stmt.ColumnText(1),
				WorkerID:               stmt.ColumnText(2),
				TargetTPS:              int(stmt.ColumnInt64(5)),
				Concurrency:            int(stmt.ColumnInt64(6)),
				TotalDurationMs:        stmt.ColumnInt64(7),
				TotalTransactions:      int(stmt.ColumnInt64(8)),
				SuccessfulTransactions: int(stmt.ColumnInt64(9)),
				FailedTransactions:     int(stmt.ColumnInt64(10)),
				AverageTPS:             stmt.ColumnFloat(11),
				PeakTPS:                stmt.ColumnFloat(12),
				MinLatencyMs:           stmt.ColumnFloat(13),
				MaxLatencyMs:           stmt.ColumnFloat(14),
				MeanLatencyMs:          stmt.ColumnFloat(15),
				P50LatencyMs:           stmt.ColumnFloat(16),
				P90LatencyMs:           stmt.ColumnFloat(17),
				P95LatencyMs:           stmt.ColumnFloat(18),
				P99LatencyMs:           stmt.ColumnFloat(19),
				TransactionsJSON:       stmt.ColumnText(20),
				ResponseCodesJSON:      stmt.ColumnText(21),
			}
			rec.StartTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(3))
			rec.EndTime, _ = time.Parse("2006-01-02 15:04:05", stmt.ColumnText(4))
			results = append(results, rec)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetSessionsList fetches all recorded sessions from SQLite
func GetSessionsList() ([]*SessionRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT s.session_id, s.start_time, s.last_active_time, s.spec_path, s.spec_name, s.tx_file_path, s.tx_file_name, s.status,
		       s.host, s.port, s.connection_type, s.header_type, s.tls_enabled,
		       COUNT(t.id) as total_tx,
		       SUM(CASE WHEN t.success = 1 THEN 1 ELSE 0 END) as success_tx,
		       SUM(CASE WHEN t.success = 0 THEN 1 ELSE 0 END) as failed_tx,
		       COUNT(DISTINCT st.id) as stress_count
		FROM sessions s
		LEFT JOIN transactions t ON s.session_id = t.session_id
		LEFT JOIN stress_tests st ON s.session_id = st.session_id
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
				StressTestCount:  int(stmt.ColumnInt64(16)),
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

// GetSessionByID returns a specific session record. A session ID absent from
// the database yields ErrSessionNotFound; no record is fabricated.
func GetSessionByID(sessionID string) (*SessionRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

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
		Args: []any{sessionID},
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
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return rec, nil
}

// GetVisaSessions fetches sessions that used a Visa specification or header format
func GetVisaSessions() ([]*SessionRecord, error) {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	sql := `
		SELECT s.session_id, s.start_time, s.last_active_time, s.spec_path, s.spec_name, s.tx_file_path, s.tx_file_name, s.status,
		       s.host, s.port, s.connection_type, s.header_type, s.tls_enabled,
		       COUNT(t.id) as total_tx,
		       SUM(CASE WHEN t.success = 1 AND (t.response_code = '00' OR t.response_code = '000' OR t.response_code = '') THEN 1 ELSE 0 END) as success_tx,
		       SUM(CASE WHEN t.success = 0 OR (t.response_code != '00' AND t.response_code != '000' AND t.response_code != '') THEN 1 ELSE 0 END) as failed_tx,
		       COUNT(DISTINCT st.id) as stress_count
		FROM sessions s
		LEFT JOIN transactions t ON s.session_id = t.session_id
		LEFT JOIN stress_tests st ON s.session_id = st.session_id
		WHERE LOWER(s.spec_name) LIKE '%visa%'
		   OR LOWER(s.spec_path) LIKE '%visa%'
		   OR LOWER(s.header_type) LIKE '%visa%'
		   OR LOWER(t.spec_name) LIKE '%visa%'
		   OR LOWER(t.spec_path) LIKE '%visa%'
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
				StressTestCount:  int(stmt.ColumnInt64(16)),
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

// ErrSessionNotFound is returned by GetSessionByID when the database has no
// row for the session ID: callers must surface it instead of receiving a
// fabricated zero-value record.
var ErrSessionNotFound = errors.New("session not found")

// ErrTransactionNotFound is returned by GetTransactionByID when the database
// has no row for the transaction ID, so callers can surface it as a typed
// not-found instead of an opaque text error. The wrapped text keeps the
// legacy "transaction ID <id> not found" prefix verbatim.
