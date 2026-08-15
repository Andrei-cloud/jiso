package db

import (
	"fmt"
	"log"
	"sync"
	"time"

	"zombiezen.com/go/sqlite/sqlitex"
)

// AsyncLogger handles asynchronous transaction logging to the database
type AsyncLogger struct {
	txChan    chan *TransactionRecord
	wg        sync.WaitGroup
	batchSize int
	interval  time.Duration
	done      chan struct{}
}

// TransactionRecord holds data for a single transaction log
type TransactionRecord struct {
	SessionID        string
	TxName           string
	TxFilePath       string
	TxFileName       string
	SpecPath         string
	SpecName         string
	RequestJSON      string
	ResponseJSON     *string
	RequestRawHEX    string
	ResponseRawHEX   *string
	ResponseCode     string
	ProcessingTimeMs int
	Success          bool
}

var (
	logger *AsyncLogger
	once   sync.Once
)

// InitAsyncLogger initializes the asynchronous logger
func InitAsyncLogger(bufferSize, batchSize int, interval time.Duration) {
	once.Do(func() {
		logger = &AsyncLogger{
			txChan:    make(chan *TransactionRecord, bufferSize),
			batchSize: batchSize,
			interval:  interval,
			done:      make(chan struct{}),
		}
		logger.start()
	})
}

// GetAsyncLogger returns the singleton logger instance
func GetAsyncLogger() *AsyncLogger {
	return logger
}

// StopAsyncLogger stops the background logger and flushes remaining records
func StopAsyncLogger() {
	if logger != nil {
		close(logger.done)
		logger.wg.Wait()
		logger = nil
		once = sync.Once{}
	}
}

// FlushTransactions flushes all queued transactions to the database and waits for write completion
func FlushTransactions() {
	StopAsyncLogger()
}

// LogTransaction queues a basic transaction for logging
func LogTransaction(sessionID, txName, requestJSON string, responseJSON *string, processingTimeMs int, success bool) {
	LogTransactionEnriched(&TransactionRecord{
		SessionID:        sessionID,
		TxName:           txName,
		RequestJSON:      requestJSON,
		ResponseJSON:     responseJSON,
		ProcessingTimeMs: processingTimeMs,
		Success:          success,
	})
}

// LogTransactionEnriched queues an enriched transaction for logging
func LogTransactionEnriched(record *TransactionRecord) {
	if record == nil {
		return
	}
	if logger == nil {
		// Fallback to synchronous if async logger isn't initialized
		responseCode := record.ResponseCode
		if responseCode == "" {
			responseCode = deriveResponseCode(record.ResponseJSON)
		}
		if err := InsertTransactionEnriched(&EnrichedTransactionRecord{
			SessionID:        record.SessionID,
			TxName:           record.TxName,
			TxFilePath:       record.TxFilePath,
			TxFileName:       record.TxFileName,
			SpecPath:         record.SpecPath,
			SpecName:         record.SpecName,
			RequestJSON:      record.RequestJSON,
			ResponseJSON:     record.ResponseJSON,
			RequestRawHEX:    record.RequestRawHEX,
			ResponseRawHEX:   record.ResponseRawHEX,
			ResponseCode:     responseCode,
			ProcessingTimeMs: record.ProcessingTimeMs,
			Success:          record.Success,
		}); err != nil {
			log.Printf("Failed to insert transaction synchronously: %v", err)
		}
		return
	}

	select {
	case logger.txChan <- record:
		// Queued successfully
	default:
		// Channel full, drop or log error to prevent blocking
		log.Printf("AsyncLogger channel full, dropping transaction log for %s", record.TxName)
	}
}

func (l *AsyncLogger) start() {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		batch := make([]*TransactionRecord, 0, l.batchSize)
		ticker := time.NewTicker(l.interval)
		defer ticker.Stop()

		flush := func() {
			if len(batch) > 0 {
				if err := l.writeBatch(batch); err != nil {
					log.Printf("Failed to write batch to DB: %v", err)
				}
				// Clear batch
				batch = batch[:0]
			}
		}

		for {
			select {
			case record := <-l.txChan:
				batch = append(batch, record)
				if len(batch) >= l.batchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-l.done:
				flush()
				// Drain channel non-blockingly without hanging on unclosed channel
				draining := true
				for draining {
					select {
					case record := <-l.txChan:
						batch = append(batch, record)
						if len(batch) >= l.batchSize {
							flush()
						}
					default:
						draining = false
					}
				}
				flush()
				return
			}
		}
	}()
}

func (l *AsyncLogger) writeBatch(batch []*TransactionRecord) error {
	if dbConn == nil {
		return fmt.Errorf("database not initialized")
	}

	// Use a transaction for the entire batch
	err := sqlitex.ExecuteTransient(dbConn, "BEGIN IMMEDIATE", nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil) // Rollback if not committed
	}()

	insertSQL := `
		INSERT INTO transactions (
			session_id, transaction_name, request_json, response_json, 
			processing_time_ms, success, response_code,
			tx_file_path, tx_file_name, spec_path, spec_name,
			request_raw_hex, response_raw_hex
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	touchedSessions := make(map[string]bool)
	for _, record := range batch {
		if record.SessionID != "" && !touchedSessions[record.SessionID] {
			_ = TouchSession(record.SessionID)
			touchedSessions[record.SessionID] = true
		}

		responseCode := record.ResponseCode
		if responseCode == "" {
			responseCode = deriveResponseCode(record.ResponseJSON)
		}

		err = sqlitex.ExecuteTransient(dbConn, insertSQL, &sqlitex.ExecOptions{
			Args: []interface{}{
				record.SessionID,
				record.TxName,
				record.RequestJSON,
				derefOrNil(record.ResponseJSON),
				record.ProcessingTimeMs,
				record.Success,
				responseCode,
				record.TxFilePath,
				record.TxFileName,
				record.SpecPath,
				record.SpecName,
				record.RequestRawHEX,
				derefOrNil(record.ResponseRawHEX),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to insert record in batch: %w", err)
		}
	}

	err = sqlitex.ExecuteTransient(dbConn, "COMMIT", nil)
	if err != nil {
		return fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	return nil
}
