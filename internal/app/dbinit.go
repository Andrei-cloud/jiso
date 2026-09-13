// dbinit.go owns the session-database wiring seam (UAT-01). Before it, the
// only production caller of db.InitDB/db.InitAsyncLogger was the pre-v2
// service initialization path (CLI.InitService), so the cobra command tree
// — send, scenario run, stress, the TUI — logged through
// LogTransactionToDB against a connection that was never opened: every
// --db was a silent no-op with a per-transaction
// "database not initialized" line on stderr.
package app

import (
	"sync"
	"time"

	"jiso/internal/config"
	"jiso/internal/db"
)

// Async logger tuning shared by every seam caller (the pre-v2 service used
// the same buffer/batch/interval values).
const (
	sessionDBLoggerBuffer   = 1000
	sessionDBLoggerBatch    = 50
	sessionDBLoggerInterval = 100 * time.Millisecond
)

var sessionDBInitMu sync.Mutex

// EnsureSessionDB opens the session database for cfg's resolved path and
// starts the async logger, at most once per process and path: an empty
// path is a no-op (session logging stays disabled exactly as before), and
// a repeat call for the path whose connection is already open is a no-op
// so the cobra PersistentPreRunE seam and the service initialization path
// share one wiring instead of duplicating it. A failed init returns the
// database error (open/create-tables text naming the cause) for the caller
// to surface; it never leaves a half-wired global behind.
func EnsureSessionDB(cfg *config.Config) error {
	if cfg == nil {
		cfg = config.GetConfig()
	}

	dbPath := cfg.GetDbPath()
	if dbPath == "" {
		return nil
	}

	sessionDBInitMu.Lock()
	defer sessionDBInitMu.Unlock()

	if db.IsInitialized() && db.CurrentPath() == dbPath {
		return nil
	}

	if err := db.InitDB(dbPath); err != nil {
		return err
	}
	db.InitAsyncLogger(sessionDBLoggerBuffer, sessionDBLoggerBatch, sessionDBLoggerInterval)

	return nil
}
