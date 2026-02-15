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
	RequestJSON      string
	ResponseJSON     *string
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
	}
}

// LogTransaction queues a transaction for logging
func LogTransaction(sessionID, txName, requestJSON string, responseJSON *string, processingTimeMs int, success bool) {
	if logger == nil {
		// Fallback to synchronous if async logger isn't initialized
		if err := InsertTransaction(sessionID, txName, requestJSON, responseJSON, processingTimeMs, success); err != nil {
			log.Printf("Failed to insert transaction synchronously: %v", err)
		}
		return
	}

	record := &TransactionRecord{
		SessionID:        sessionID,
		TxName:           txName,
		RequestJSON:      requestJSON,
		ResponseJSON:     responseJSON,
		ProcessingTimeMs: processingTimeMs,
		Success:          success,
	}

	select {
	case logger.txChan <- record:
		// Queued successfully
	default:
		// Channel full, drop or log error to prevent blocking
		log.Printf("AsyncLogger channel full, dropping transaction log for %s", txName)
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
				// Flush remaining
				flush()
				// Drain channel
				for record := range l.txChan {
					batch = append(batch, record)
					if len(batch) >= l.batchSize {
						flush()
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
	defer sqlitex.ExecuteTransient(dbConn, "ROLLBACK", nil) // Rollback if not committed

	insertSQL := `
		INSERT INTO transactions (
			session_id, transaction_name, request_json, response_json, 
			processing_time_ms, success, response_code
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	stmt := dbConn.Prep(insertSQL)
	if stmt == nil {
		return fmt.Errorf("failed to prepare statement")
	}
	// No need to finalize cached statement from Prep, but if we create a new one we should.
	// sqlitex.ExecuteTransient handles preparation and finalization, but here we want to reuse the statement for the batch.
	// However, using sqlitex.ExecuteTransient inside the loop is easier and safe enough for SQLite batching
	// because the outer transaction holds the lock.

	for _, record := range batch {
		responseCode := deriveResponseCode(record.ResponseJSON)

		err = sqlitex.ExecuteTransient(dbConn, insertSQL, &sqlitex.ExecOptions{
			Args: []interface{}{
				record.SessionID,
				record.TxName,
				record.RequestJSON,
				derefOrNil(record.ResponseJSON),
				record.ProcessingTimeMs,
				record.Success,
				responseCode,
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
