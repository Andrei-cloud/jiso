package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var dbConn *sqlite.Conn

// currentDBPath is the file behind dbConn ("" when the installed handle
// carries no path identity). Guarded by connMu; cleared by Close. It lets
// same-process writers and readers recognize an already-open database
// instead of re-opening (and leaking) a second handle for the same file.
var currentDBPath string

// connMu is the conn's lock: it serialises every read and write leg against
// the process-global dbConn (including the async logger's synchronous
// fallback, Close, and the OpenExisting/InitDB assignments). A single
// *sqlite.Conn is not safe for concurrent statements and cannot host two
// overlapping transactions, so each BEGIN...COMMIT runs entirely under this
// lock. Unexported *Locked helpers assume the caller already holds it.
var connMu sync.Mutex

// SessionRecord is one row of the session table: what was loaded (spec, transaction
// file, peer, TLS) and when it was used, which is everything the session browser's
// list and the stats pane show without joining to another table.
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
	StressTestCount  int       `json:"stress_test_count,omitempty"`
}

// StressTestSummaryRecord is one completed stress run: its window, target rate and
// measured result. It outlives the worker that produced it, which is why the
// results survive the process that ran them.
type StressTestSummaryRecord struct {
	ID                     int64     `json:"id"`
	SessionID              string    `json:"session_id"`
	WorkerID               string    `json:"worker_id"`
	StartTime              time.Time `json:"start_time"`
	EndTime                time.Time `json:"end_time"`
	TargetTPS              int       `json:"target_tps"`
	Concurrency            int       `json:"concurrency"`
	TotalDurationMs        int64     `json:"total_duration_ms"`
	TotalTransactions      int       `json:"total_transactions"`
	SuccessfulTransactions int       `json:"successful_transactions"`
	FailedTransactions     int       `json:"failed_transactions"`
	AverageTPS             float64   `json:"average_tps"`
	PeakTPS                float64   `json:"peak_tps"`
	MinLatencyMs           float64   `json:"min_latency_ms"`
	MaxLatencyMs           float64   `json:"max_latency_ms"`
	MeanLatencyMs          float64   `json:"mean_latency_ms"`
	P50LatencyMs           float64   `json:"p50_latency_ms"`
	P90LatencyMs           float64   `json:"p90_latency_ms"`
	P95LatencyMs           float64   `json:"p95_latency_ms"`
	P99LatencyMs           float64   `json:"p99_latency_ms"`
	TransactionsJSON       string    `json:"transactions_json"`
	ResponseCodesJSON      string    `json:"response_codes_json"`
}

// EnrichedTransactionRecord is one exchange as the session database stores it --
// the wire data plus the names the operator recognises (transaction, spec, session)
// and the outcome's own columns, so the history list and the RC distribution are
// queries rather than decodes.
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

// InitDB initializes the database connection and creates tables. The schema
// is ensured on the freshly opened handle before it is published as the
// process-global conn, so a table-ensure failure closes the handle and leaves
// dbConn untouched (no leaked handle, no stale global).
func InitDB(dbPath string) error {
	if dbPath == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure the local handle before publishing it.
	if err := applyOpenPragmas(conn); err != nil {
		_ = conn.Close()

		return err
	}

	// Create tables on the local handle before publishing it.
	if err := createTables(conn); err != nil {
		_ = conn.Close()

		return fmt.Errorf("failed to create tables: %w", err)
	}

	connMu.Lock()
	if dbConn != nil {
		_ = dbConn.Close() // Replacing an open handle: don't leak the old one
	}
	dbConn = conn
	currentDBPath = dbPath
	connMu.Unlock()

	return nil
}

// applyOpenPragmas configures per-connection behavior needed for safe
// multi-process use. busy_timeout makes a second jiso process sharing the
// same session DB wait for the writer lock instead of failing immediately
// with SQLITE_BUSY. WAL is intentionally not enabled: it would change the
// on-disk journal file set that external tooling may read.
func applyOpenPragmas(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA busy_timeout=5000", nil); err != nil {
		return fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	return nil
}

// IsInitialized reports whether a process-global connection is open
// (InitDB/OpenExisting wired one and Close has not run since).
func IsInitialized() bool {
	connMu.Lock()
	defer connMu.Unlock()

	return dbConn != nil
}

// CurrentPath returns the file behind the open global connection, or ""
// when none is open or the installed handle carries no path identity.
func CurrentPath() string {
	connMu.Lock()
	defer connMu.Unlock()

	return currentDBPath
}

// ErrDBNotFound reports a --db path whose file does not exist. Read-only
// commands must surface it as a config-class failure naming the
// path instead of letting InitDB create an empty database.
var ErrDBNotFound = errors.New("database file does not exist")

// OpenExisting opens an existing database for the read-only review commands
// (db stats, db tx, ctf list, ctf export). Unlike InitDB it never creates the
// database file: a missing path yields ErrDBNotFound wrapping the os.Stat
// result. An existing file is opened and its schema ensured
// exactly as InitDB does, so existing-but-table-less databases keep their
// current "report what they are" behavior. Write paths (REPL, serve, golden
// fixture builder) keep using InitDB.
func OpenExisting(path string) error {
	if path == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s: %w", ErrDBNotFound, path, err)
		}

		return fmt.Errorf("failed to stat database file %s: %w", path, err)
	}

	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := applyOpenPragmas(conn); err != nil {
		_ = conn.Close()

		return err
	}

	// Ensure the schema on the local handle first: on failure close the
	// handle and leave dbConn nil (no leaked handle, no stale global).
	if err := createTables(conn); err != nil {
		_ = conn.Close()

		return fmt.Errorf("failed to create tables: %w", err)
	}

	connMu.Lock()
	if dbConn != nil {
		_ = dbConn.Close() // Replacing an open handle: don't leak the old one
	}
	dbConn = conn
	// Same-path reuse guards need the path identity; InitDB sets it and
	// OpenExisting must too, or a later writer re-opens the same file.
	currentDBPath = path
	connMu.Unlock()

	return nil
}

// Close closes the database connection and clears the global so a later
// InitDB/OpenExisting republishes a fresh handle. It holds connMu so a close
// can never free the conn out from under an in-flight statement.
func Close() error {
	connMu.Lock()
	defer connMu.Unlock()

	if dbConn != nil {
		err := dbConn.Close()
		dbConn = nil
		currentDBPath = ""

		return err
	}

	return nil
}

// createTables creates the necessary database tables on conn. The caller owns
// the handle (InitDB/OpenExisting call it before publishing dbConn) so it
// never touches the global.
// ddlText is the SQLite column type the schema migrations add. It was spelled as
// a literal at ten sites: a "TXT" there is a migration that silently never runs,
// and nothing at runtime notices that a column is missing.
const ddlText = "TEXT"

func createTables(conn *sqlite.Conn) error {
	if err := createSessionsTable(conn); err != nil {
		return err
	}

	if err := createStressTable(conn); err != nil {
		return err
	}

	if err := createTransactionsTable(conn); err != nil {
		return err
	}

	return createTransactionsIndexes(conn)
}

// createSessionsTable creates the sessions table and adds any columns missing
// from a pre-existing one.
func createSessionsTable(conn *sqlite.Conn) error {
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
	if err := sqlitex.ExecuteTransient(conn, createSessionsSQL, nil); err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	sessColumns := []struct{ name, def string }{
		{"host", ddlText},
		{"port", ddlText},
		{"connection_type", ddlText},
		{"header_type", ddlText},
		{"tls_enabled", "BOOLEAN"},
	}
	for _, c := range sessColumns {
		_ = sqlitex.ExecuteTransient(conn, fmt.Sprintf("ALTER TABLE sessions ADD COLUMN %s %s", c.name, c.def), nil)
	}
	return nil
}

// createStressTable creates the stress_tests table and its session index.
func createStressTable(conn *sqlite.Conn) error {
	// Create stress_tests table
	createStressSQL := `CREATE TABLE IF NOT EXISTS stress_tests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		worker_id TEXT,
		start_time DATETIME,
		end_time DATETIME,
		target_tps INTEGER,
		concurrency INTEGER,
		total_duration_ms INTEGER,
		total_transactions INTEGER,
		successful_transactions INTEGER,
		failed_transactions INTEGER,
		avg_tps REAL,
		peak_tps REAL,
		min_latency_ms REAL,
		max_latency_ms REAL,
		mean_latency_ms REAL,
		p50_latency_ms REAL,
		p90_latency_ms REAL,
		p95_latency_ms REAL,
		p99_latency_ms REAL,
		transactions_json TEXT,
		response_codes_json TEXT
	)`
	if err := sqlitex.ExecuteTransient(conn, createStressSQL, nil); err != nil {
		return fmt.Errorf("failed to create stress_tests table: %w", err)
	}

	_ = sqlitex.ExecuteTransient(conn, `CREATE INDEX IF NOT EXISTS idx_stress_session ON stress_tests(session_id)`, nil)
	return nil
}

// createTransactionsTable creates the transactions table and adds any columns
// missing from a pre-existing one.
func createTransactionsTable(conn *sqlite.Conn) error {
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

	if err := sqlitex.ExecuteTransient(conn, createTableSQL, nil); err != nil {
		return err
	}

	// Ensure missing columns exist in pre-existing transactions table
	columns := []struct{ name, def string }{
		{"tx_file_path", ddlText},
		{"tx_file_name", ddlText},
		{"spec_path", ddlText},
		{"spec_name", ddlText},
		{"request_raw_hex", ddlText},
		{"response_raw_hex", ddlText},
	}
	for _, c := range columns {
		_ = sqlitex.ExecuteTransient(conn, fmt.Sprintf("ALTER TABLE transactions ADD COLUMN %s %s", c.name, c.def), nil)
	}
	return nil
}

// createTransactionsIndexes creates the transactions lookup indexes.
func createTransactionsIndexes(conn *sqlite.Conn) error {
	// Create indexes
	indexSQL1 := `CREATE INDEX IF NOT EXISTS idx_session_timestamp ON transactions(session_id, timestamp)`
	if err := sqlitex.ExecuteTransient(conn, indexSQL1, nil); err != nil {
		return err
	}

	indexSQL2 := `CREATE INDEX IF NOT EXISTS idx_response_code ON transactions(response_code)`
	return sqlitex.ExecuteTransient(conn, indexSQL2, nil)
}
