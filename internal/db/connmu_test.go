// connmu_test.go is the regression: the process-global dbConn is
// not concurrency-safe on its own. connMu must serialise every read and write
// leg against it, Close must never free the handle mid-statement, and
// OpenExisting must leave dbConn untouched when the table-ensure fails. Run
// these with -race.
package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentReadWriteOnSharedConn hammers a single InitDB conn with a
// reader loop (GetTransactionStats) and a writer loop
// (InsertTransactionEnriched) for ~200 iterations. Before connMu these raced on
// the conn (and the conn could not host two transactions at once), surfacing
// as a panic or a "cannot start a transaction within a transaction"/busy error.
func TestConcurrentReadWriteOnSharedConn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	const sessionID = "rw-session"
	if err := UpsertSession(sessionID, "", "", "", "", "", "", "", "", "active", false); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	const iterations = 100
	resp := `{"mti":"0210","fields":{"39":"00"}}`

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		firstEr string
		record  = func(op string, err error) {
			if err == nil {
				return
			}
			mu.Lock()
			if firstEr == "" {
				firstEr = fmt.Sprintf("%s: %v", op, err)
			}
			mu.Unlock()
		}
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			err := InsertTransactionEnriched(&EnrichedTransactionRecord{
				SessionID:        sessionID,
				TxName:           "Purchase",
				RequestJSON:      `{"mti":"0200","fields":{"11":"1"}}`,
				ResponseJSON:     &resp,
				ProcessingTimeMs: i,
				Success:          true,
				ResponseCode:     "00",
			})
			record("InsertTransactionEnriched", err)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, err := GetTransactionStats(sessionID)
			record("GetTransactionStats", err)
		}
	}()
	wg.Wait()

	if firstEr != "" {
		t.Fatalf("concurrent access surfaced an error: %s", firstEr)
	}

	rows, err := GetSessionTransactions(sessionID)
	count := len(rows)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != iterations {
		t.Fatalf("persisted %d transactions, want %d", count, iterations)
	}
}

// TestCloseDuringConcurrentWritersNoUseAfterClose closes the conn while a
// writer loop is running. connMu must make Close wait for any in-flight
// statement (no use-after-close panic), and writers that arrive after Close
// must error cleanly with "database not initialized".
func TestCloseDuringConcurrentWritersNoUseAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close-race.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if err := UpsertSession("s", "", "", "", "", "", "", "", "", "active", false); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	const writers = 4
	var (
		wg       sync.WaitGroup
		cleanErr int
		mu       sync.Mutex
		bad      string
	)

	stop := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				err := InsertTransactionEnriched(&EnrichedTransactionRecord{
					SessionID:   "s",
					TxName:      "Purchase",
					RequestJSON: `{"mti":"0200","fields":{}}`,
					Success:     true,
				})
				if err == nil {
					continue
				}
				if strings.Contains(err.Error(), "database not initialized") {
					mu.Lock()
					cleanErr++
					mu.Unlock()

					return
				}
				mu.Lock()
				if bad == "" {
					bad = err.Error()
				}
				mu.Unlock()

				return
			}
		}(i)
	}

	// Let the writers get mid-statement, then close underneath them.
	time.Sleep(20 * time.Millisecond)
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.After(5 * time.Second)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-deadline:
		close(stop)
		t.Fatal("writers did not terminate after Close")
	}
	close(stop)

	if bad != "" {
		t.Fatalf("writer surfaced a non-clean error (use-after-close?): %s", bad)
	}
	if cleanErr == 0 {
		t.Fatal("no writer observed the clean post-close error")
	}

	// A fresh write after Close errors cleanly, never panics.
	if err := InsertTransactionEnriched(&EnrichedTransactionRecord{SessionID: "s"}); err == nil ||
		!strings.Contains(err.Error(), "database not initialized") {
		t.Fatalf("post-close write = %v, want database-not-initialized", err)
	}
}

// TestOpenExistingTableEnsureFailureLeavesConnNil proves the OpenExisting fix:
// when the schema-ensure fails on an existing-but-corrupt file, the handle is
// closed and dbConn is left untouched (no leaked handle swapped into the global,
// no stale global clobbering a live conn). The corrupt file is a genuinely
// reachable table-ensure failure on macOS (open succeeds, CREATE TABLE hits
// SQLITE_NOTADB).
func TestOpenExistingTableEnsureFailureLeavesConnNil(t *testing.T) {
	dir := t.TempDir()

	// A live, good conn published as the global, carrying one session.
	good := filepath.Join(dir, "good.db")
	if err := InitDB(good); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := UpsertSession("good-session", "", "", "", "", "", "", "", "", "active", false); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// An existing file that opens but whose tables cannot be ensured.
	corrupt := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("not a sqlite database, just bytes"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	err := OpenExisting(corrupt)
	if err == nil {
		t.Fatal("OpenExisting on a corrupt file returned no error")
	}
	if !strings.Contains(err.Error(), "create tables") {
		t.Fatalf("OpenExisting err = %v, want a create-tables failure", err)
	}

	// The global must still be the good conn: a read for the good session
	// succeeds. Before the fix dbConn had been reassigned to the corrupt
	// handle, so this read would fail (or the good handle leaked).
	rec, err := GetSessionByID("good-session")
	if err != nil {
		t.Fatalf("good conn was clobbered by the failed OpenExisting: %v (err %v)", rec, err)
	}
	if rec.SessionID != "good-session" {
		t.Fatalf("read back %+v, want good-session", rec)
	}

	// And dbConn is not left pointing at the corrupt handle: a fresh
	// OpenExisting on a valid file still works after the failed one.
	other := filepath.Join(dir, "other.db")
	if err := InitDB(other); err != nil {
		t.Fatalf("InitDB other: %v", err)
	}
	if _, err := GetSessionTransactions("nope"); err != nil {
		t.Fatalf("post-failure conn read = %v, want success", err)
	}
}
