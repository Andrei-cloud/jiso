// dbview_test.go covers the §I read façade against a fixture
// DB built in-test with the db package into t.TempDir: session list
// order + limit, stats math (RC distribution, avg latency), tx history
// fields (MTI, newest-first, limit), review reconstruction (hex present,
// response section), the typed errors (unset path, missing file), and
// the no-file-left-behind contract. Timestamps are pinned with
// direct UPDATEs so order/relative-time assertions are deterministic
// regardless of wall-clock insert times. Tests here mutate the shared
// config singleton and must not run in parallel.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"jiso/internal/config"
	"jiso/internal/db"
)

// mustTimes pins stored DATETIMEs to fixed values (the insert paths
// default them to CURRENT_TIMESTAMP).
func mustTimes(t *testing.T, path string, stmts ...string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = conn.Close() }()
	for _, s := range stmts {
		if err := sqlitex.ExecuteTransient(conn, s, nil); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

// dbViewFixture builds the §I fixture: two sessions (today / yesterday)
// with three transactions on the first (ok 0200/00, fail 0210/96, and a
// no-response row) and one on the second. Returns the DB path.
func dbViewFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	if err := db.InitDB(path); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture write: %v", err)
		}
	}
	must(db.UpsertSession("9f3ca1e2b7d84455a1", "specs/visa.json", "visa.json", "tx/pool.json", "pool.json", "10.0.0.5", "8080", "caller", "binary2", "closed", false))
	must(db.UpsertSession("77b255c9", "", "visa.json", "", "pool.json", "", "", "", "", "closed", false))

	req := `{"mti":"0200","fields":{"2":"4242424242424242","3":"000000","4":"100","11":"1","12":"041201"}}`
	resp := `{"mti":"0210","fields":{"2":"4242424242424242","11":"1","39":"00"}}`
	resp96 := `{"mti":"0210","fields":{"39":"96"}}`
	respStr, resp96Str := resp, resp96

	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "9f3ca1e2b7d84455a1", TxName: "Purchase", RequestJSON: req,
		ResponseJSON: &respStr, ProcessingTimeMs: 3, Success: true, ResponseCode: "00",
		SpecPath: "", TxFileName: "pool.json",
	}))
	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "9f3ca1e2b7d84455a1", TxName: "Purchase", RequestJSON: req,
		ResponseJSON: &resp96Str, ProcessingTimeMs: 2, Success: false, ResponseCode: "96",
		TxFileName: "pool.json",
	}))
	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "9f3ca1e2b7d84455a1", TxName: "Sign On", RequestJSON: `{"mti":"0800","fields":{}}`,
		ProcessingTimeMs: 0, Success: false,
	}))
	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "77b255c9", TxName: "Echo", RequestJSON: `{"mti":"0800","fields":{}}`,
		ProcessingTimeMs: 1, Success: true, ResponseCode: "00",
	}))
	mustTimes(t, path,
		`UPDATE sessions SET start_time='2026-09-08 09:55:00', last_active_time='2026-09-08 09:56:00' WHERE session_id='77b255c9'`,
		`UPDATE transactions SET timestamp='2026-09-08 09:55:30' WHERE transaction_name='Echo'`,
	)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return path
}

// dbViewApp builds an App carrying only the config leg the façade reads.
func dbViewApp(t *testing.T, dbPath string) *App {
	t.Helper()
	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetDbPath(dbPath)

	return &App{cfg: cfg}
}

// nowFixture is the fake "now": 2026-09-09 12:04 UTC (session 1 today,
// session 2 yesterday).
var nowFixture = time.Date(2026, 9, 9, 12, 4, 0, 0, time.UTC)

func TestDbViewListSessionsOrderAndLimit(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	sessions, err := a.ListSessions(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}

	limited, err := a.ListSessions(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListSessions(1): %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limited = %d, want 1", len(limited))
	}
}

func TestDbViewSessionStatsMath(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	stats, err := a.SessionStats(context.Background(), "9f3ca1e2b7d84455a1")
	if err != nil {
		t.Fatalf("SessionStats: %v", err)
	}
	if stats.TotalTransactions != 3 || stats.SuccessfulTransactions != 1 || stats.FailedTransactions != 2 {
		t.Errorf("counters = %+v, want 3/1/2", stats)
	}
	if stats.AverageProcessingTimeMs != 2.5 {
		t.Errorf("avg = %v, want 2.5 (rows with ms>0 only)", stats.AverageProcessingTimeMs)
	}
	if got := stats.ResponseCodeDistribution; got["00"] != 1 || got["96"] != 1 || got["91"] != 1 {
		t.Errorf("RC distribution = %v, want 00:1 96:1 91:1", got)
	}
}

func TestDbViewTxHistoryFieldsAndOrder(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	rows, err := a.TxHistory(context.Background(), "9f3ca1e2b7d84455a1", 0)
	if err != nil {
		t.Fatalf("TxHistory: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	newest := rows[0]
	if newest.TxName != "Sign On" || newest.ID <= rows[1].ID {
		t.Errorf("newest row = %+v, want Sign On (newest id first)", newest)
	}
	first := rows[2]
	if first.TxName != "Purchase" || first.MTI != "0200" || first.ResponseCode != "00" || !first.Success {
		t.Errorf("oldest row = %+v, want Purchase/0200/00/ok", first)
	}
	if first.ProcessingTime != 3*time.Millisecond {
		t.Errorf("processing = %v, want 3ms", first.ProcessingTime)
	}

	limited, err := a.TxHistory(context.Background(), "9f3ca1e2b7d84455a1", 2)
	if err != nil {
		t.Fatalf("TxHistory(2): %v", err)
	}
	if len(limited) != 2 || limited[0].TxName != "Sign On" {
		t.Errorf("limited = %+v, want newest 2 newest-first", limited)
	}
}

func TestDbViewReviewTxReconstruction(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	rows, err := a.TxHistory(context.Background(), "9f3ca1e2b7d84455a1", 0)
	if err != nil {
		t.Fatalf("TxHistory: %v", err)
	}
	var purchaseID int64
	for _, r := range rows {
		if r.ResponseCode == "00" {
			purchaseID = r.ID
		}
	}
	if purchaseID == 0 {
		t.Fatal("fixture has no RC-00 row")
	}

	review, err := a.ReviewTx(context.Background(), purchaseID)
	if err != nil {
		t.Fatalf("ReviewTx: %v", err)
	}
	if review == nil || review.Request == nil || review.Request.HEX == "" {
		t.Fatalf("review = %+v, want request hex present", review)
	}
	if !review.HasResponse || review.Response == nil {
		t.Fatalf("review = %+v, want response section", review)
	}
	if review.Request.DescribeText == "" || review.Response.DescribeText == "" {
		t.Error("describe text missing from reconstruction")
	}
	if review.TxName != "Purchase" || review.ResponseCode != "00" {
		t.Errorf("review row = %+v", review)
	}
}

func TestDbViewReviewMissingRowIsError(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))
	if _, err := a.ReviewTx(context.Background(), 9999); err == nil {
		t.Fatal("ReviewTx(9999) = nil error, want not-found")
	}
}

func TestDbViewUnsetPathIsTypedError(t *testing.T) {
	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	a := &App{cfg: cfg}

	if _, err := a.ListSessions(context.Background(), 0); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("ListSessions err = %v, want ErrDBNotConfigured", err)
	}
	if _, err := a.SessionStats(context.Background(), "x"); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("SessionStats err = %v, want ErrDBNotConfigured", err)
	}
	if _, err := a.TxHistory(context.Background(), "x", 0); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("TxHistory err = %v, want ErrDBNotConfigured", err)
	}
	if _, err := a.ReviewTx(context.Background(), 1); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("ReviewTx err = %v, want ErrDBNotConfigured", err)
	}
}

func TestDbViewMissingDBTypedAndNoFileCreated(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", "sessions.db")
	a := dbViewApp(t, missing)

	_, err := a.ListSessions(context.Background(), 0)
	if !errors.Is(err, db.ErrDBNotFound) {
		t.Fatalf("err = %v, want db.ErrDBNotFound", err)
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != missing {
		t.Fatalf("err = %v, want *ConfigError naming the path", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read path created the database: stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(missing) + "-wal"); err == nil {
		t.Fatal("stray -wal file left behind")
	}
}

func TestDbViewContextCancel(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ListSessions(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDbPathAccessor(t *testing.T) {
	path := dbViewFixture(t)
	a := dbViewApp(t, path)
	if a.DBPath() != path {
		t.Fatalf("DBPath = %q, want %q", a.DBPath(), path)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 9, 9, 12, 1, 0, 0, time.UTC), "today 12:01"},
		{time.Date(2026, 9, 8, 17, 30, 0, 0, time.UTC), "yest 17:30"},
		{time.Date(2026, 9, 1, 8, 5, 0, 0, time.UTC), "09-01 08:05"},
	}
	for _, c := range cases {
		if got := FormatRelativeTime(nowFixture, c.t); got != c.want {
			t.Errorf("FormatRelativeTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
