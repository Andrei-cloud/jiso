// db_write_test.go pins the UAT-01 write path with dedicated exec tests (NOT
// golden files): the golden fixture DB is seeded directly with db.InitDB in
// the harness process, which masked the fact that no shipped cobra command
// ever initialized the session database, so `--db` was a silent no-op that
// spammed "database not initialized" per logged transaction.
//
// Contract pinned: `send --db`, `scenario run --db`, and `stress --db`
// against the in-process live mock server exit 0, CREATE the database file,
// record a session row plus transaction rows (read back with the repo's own
// db helpers, the same ones `db stats` uses), and keep stderr free of the
// "database not initialized" text.
package goldentest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"jiso/internal/db"
)

// dbNotInitSpam is the stderr text the un-fixed binary emits per logged
// transaction when the async logger and the global conn were never wired.
const dbNotInitSpam = "database not initialized"

func requireLiveHarness(t *testing.T) {
	t.Helper()

	if buildErr != nil {
		t.Skipf("golden binary unavailable: %v", buildErr)
	}

	if fixtureErr != nil {
		t.Skipf("golden fixtures unavailable: %v", fixtureErr)
	}

	if liveServerErr != nil {
		t.Skipf("live mock server unavailable: %v", liveServerErr)
	}
}

func TestLiveSendWithDBWritesSessionDatabase(t *testing.T) {
	t.Parallel()

	requireLiveHarness(t)

	work := t.TempDir()
	require.NoError(t, copyFixtures(work))

	dbPath := filepath.Join(work, "live.db")

	stdout, stderr, code := runBinary(t, &goldenCase{Args: []string{
		"send", "Echo",
		"-s", "spec.json", "-f", "tx.json",
		"--header", "ascii4",
		"-H", "127.0.0.1", "-p", livePort,
		"--reconnect-attempts", "0",
		"--db", dbPath,
		"--json",
	}}, work)

	require.Equal(t, 0, code, "send --db must exit 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, dbNotInitSpam, "success-path stderr must stay clean")
	require.FileExists(t, dbPath, "send --db must create the session database")

	sessionRows, txRows := countSessionAndTxRows(t, dbPath)
	assert.GreaterOrEqual(t, sessionRows, 1, "the send session must be recorded in sessions")
	assert.GreaterOrEqual(t, txRows, 1, "the executed transaction must be recorded in transactions")
}

func TestLiveScenarioRunWithDBWritesSessionDatabase(t *testing.T) {
	t.Parallel()

	requireLiveHarness(t)

	work := t.TempDir()
	require.NoError(t, copyFixtures(work))

	dbPath := filepath.Join(work, "live.db")

	stdout, stderr, code := runBinary(t, &goldenCase{Args: []string{
		"scenario", "run", "Smoke",
		"-s", "spec.json", "-f", "tx.json",
		"-l", "ascii4",
		"-H", "127.0.0.1", "-p", livePort,
		"--reconnect-attempts", "0",
		"--db", dbPath,
		"--json",
	}}, work)

	require.Equal(t, 0, code, "scenario run --db must exit 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, dbNotInitSpam, "success-path stderr must stay clean")
	require.FileExists(t, dbPath, "scenario run --db must create the session database")

	sessionRows, txRows := countSessionAndTxRows(t, dbPath)
	assert.GreaterOrEqual(t, sessionRows, 1, "the scenario session must be recorded in sessions")
	assert.GreaterOrEqual(t, txRows, 1, "scenario steps must be recorded in transactions")
}

func TestLiveStressWithDBWritesSessionDatabase(t *testing.T) {
	t.Parallel()

	requireLiveHarness(t)

	work := t.TempDir()
	require.NoError(t, copyFixtures(work))

	dbPath := filepath.Join(work, "live.db")

	stdout, stderr, code := runBinary(t, &goldenCase{Args: []string{
		"stress", "--tx", "Echo",
		"--tps", "50", "--ramp", "100ms", "--duration", "200ms", "--workers", "2",
		"-s", "spec.json", "-f", "tx.json",
		"--host", "127.0.0.1", "--port", livePort,
		"--header", "ascii4",
		"--reconnect-attempts", "0",
		"--db", dbPath,
		"--json",
	}}, work)

	require.Equal(t, 0, code, "stress --db must exit 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, dbNotInitSpam, "success-path stderr must stay clean")
	require.FileExists(t, dbPath, "stress --db must create the session database")

	sessionRows, txRows := countSessionAndTxRows(t, dbPath)
	assert.GreaterOrEqual(t, sessionRows, 1, "the stress session must be recorded in sessions")
	assert.GreaterOrEqual(t, txRows, 1, "stress transactions must be recorded in transactions")

	assertStressSummariesRecorded(t, dbPath)
}

// countSessionAndTxRows reads the recorded database back: session rows
// through the repo's own read helper (the same one `db stats` queries, so
// the assertion can never drift from the schema the shipped read path
// expects) and transaction rows through a raw COUNT(*), because the stress
// worker records under its own worker session ID (legacy behavior,
// stress_worker.go), which the per-session join does not list.
func countSessionAndTxRows(t *testing.T, dbPath string) (sessionRows, txRows int) {
	t.Helper()

	require.NoError(t, db.OpenExisting(dbPath), "recorded db must open like db stats does")

	sessions, err := db.GetSessionsList()
	require.NoError(t, err)
	total := countRawRows(t, dbPath, "transactions")

	// Release the db package's global conn before the raw read below.
	require.NoError(t, db.Close())

	return len(sessions), total
}

// assertStressSummariesRecorded pins that the stress run persists its
// summary row into stress_tests (persistStressSummary).
func assertStressSummariesRecorded(t *testing.T, dbPath string) {
	t.Helper()

	assert.Positive(t, countRawRows(t, dbPath, "stress_tests"),
		"the stress run must persist its summary row")
}

func countRawRows(t *testing.T, dbPath, table string) int {
	t.Helper()

	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadOnly)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	var count int

	err = sqlitex.Execute(conn, "SELECT COUNT(*) FROM "+table, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = int(stmt.ColumnInt64(0))

			return nil
		},
	})
	require.NoError(t, err)

	return count
}
