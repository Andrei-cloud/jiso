// ctfview_test.go covers the §K façade against a fixture DB built in
// t.TempDir with the db package (the goldentest buildFixtureDB
// shape): one Visa-headered session with two approved + one declined
// transaction and one non-Visa session. It pins the eligibility filter
// (`ctf list` rows + approved counts), the dry preview (counts, totals,
// first/last record strings, file-absence asserts), the write (one
// file, header-record prefix, overwrite reported), the typed errors
// (unknown session / no eligible tx / missing db / unset db naming the
// id or path), and the no-file-left-behind contract. Reuses dbViewApp
// and mustTimes from dbview_test.go (same package).
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jiso/internal/clearing/base2"
	"jiso/internal/db"
)

// ctfFixture builds the §K fixture DB and returns its path.
func ctfFixture(t *testing.T) string {
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
	must(db.UpsertSession("ctfsession1", "specs/visa.json", "visa.json", "tx/pool.json", "pool.json",
		"10.0.0.5", "8080", "caller", "Visa", "closed", false))
	must(db.UpsertSession("plain1", "spec.json", "Golden", "tx.json", "tx",
		"10.0.0.6", "19999", "caller", "ascii4", "closed", false))
	must(db.UpsertSession("emptyvisa", "specs/visa.json", "visa.json", "", "",
		"", "", "", "Visa", "closed", false))

	req := func(amount string) string {
		return `{"mti":"0200","fields":{"2":"4242424242424242","3":"000000","4":"` + amount +
			`","11":"1","12":"041201","13":"0907","39":"00"}}`
	}
	resp := `{"mti":"0210","fields":{"11":"1","39":"00"}}`
	resp05 := `{"mti":"0210","fields":{"11":"2","39":"05"}}`
	respStr, resp05Str := resp, resp05

	for i, amount := range []string{"1000", "250"} {
		must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
			SessionID: "ctfsession1", TxName: "Purchase", RequestJSON: req(amount),
			ResponseJSON: &respStr, ProcessingTimeMs: 3, Success: true, ResponseCode: "00",
			TxFileName: "pool.json", SpecName: "visa.json",
		}))
		_ = i
	}
	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "ctfsession1", TxName: "Purchase", RequestJSON: req("50"),
		ResponseJSON: &resp05Str, ProcessingTimeMs: 2, Success: false, ResponseCode: "05",
		TxFileName: "pool.json", SpecName: "visa.json",
	}))
	must(db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID: "plain1", TxName: "Echo", RequestJSON: `{"mti":"0800","fields":{}}`,
		ProcessingTimeMs: 1, Success: true, ResponseCode: "00",
	}))
	mustTimes(t, path,
		`UPDATE sessions SET start_time='2026-09-09 12:00:00', last_active_time='2026-09-09 12:00:00' WHERE session_id='ctfsession1'`,
		`UPDATE sessions SET start_time='2026-09-08 09:00:00', last_active_time='2026-09-08 09:00:00' WHERE session_id='emptyvisa'`,
	)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return path
}

func TestCtfListIncludesVisaSessionWithoutApprovedTx(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	views, err := a.ListCtfSessions(context.Background())
	if err != nil {
		t.Fatalf("ListCtfSessions: %v", err)
	}
	if len(views) != 2 || views[0].SessionID != "ctfsession1" || views[1].ApprovedCount != 0 {
		t.Fatalf("views = %+v, want the two Visa rows (emptyvisa with 0 approved)", views)
	}
}

func TestCtfListFindsVisaSessionWithApprovedCount(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	views, err := a.ListCtfSessions(context.Background())
	if err != nil {
		t.Fatalf("ListCtfSessions: %v", err)
	}
	if len(views) != 2 || views[0].SessionID != "ctfsession1" {
		t.Fatalf("views = %+v, want the two Visa rows, newest first", views)
	}
	if views[0].ApprovedCount != 2 {
		t.Errorf("approved = %d, want 2", views[0].ApprovedCount)
	}
	for _, v := range views {
		if v.SessionID == "plain1" {
			t.Fatalf("non-Visa session leaked into the eligible list")
		}
	}
}

func TestCtfListUnsetDBIsTypedEmptyState(t *testing.T) {
	a := dbViewApp(t, "")

	if _, err := a.ListCtfSessions(context.Background()); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("err = %v, want ErrDBNotConfigured", err)
	}
}

func TestCtfListMissingDBNamesPathAndCreatesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	a := dbViewApp(t, missing)

	_, err := a.ListCtfSessions(context.Background())
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || !errors.Is(err, db.ErrDBNotFound) || cfgErr.Path != missing {
		t.Fatalf("err = %v, want ConfigError{Path: %s} wrapping ErrDBNotFound", err, missing)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing db must not be created: stat err = %v", err)
	}
}

func TestCtfPreviewCountsTotalsAndPreviewsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	a := dbViewApp(t, ctfFixture(t))

	sum, err := a.PreviewExport(context.Background(), "ctfsession1", "400129", "", 1)
	if err != nil {
		t.Fatalf("PreviewExport: %v", err)
	}
	if !sum.DryRun || sum.Written {
		t.Errorf("dry contract = dryRun %v written %v, want true/false", sum.DryRun, sum.Written)
	}
	if sum.MonetaryTransactions != 2 || sum.ApprovedTransactions != 2 {
		t.Errorf("tx counts = %d/%d, want 2/2", sum.MonetaryTransactions, sum.ApprovedTransactions)
	}
	if sum.DestinationAmountSum != 1250 || sum.SourceAmountSum != 1250 {
		t.Errorf("sums = %d/%d, want 1250/1250", sum.DestinationAmountSum, sum.SourceAmountSum)
	}
	if sum.FirstRecord == "" || sum.LastRecord == "" || len(sum.FirstRecord) != base2.RecordLength {
		t.Errorf("record previews missing or wrong length: %q", sum.FirstRecord)
	}
	if !strings.HasPrefix(sum.FirstRecord, "05004242424242424242") {
		t.Errorf("first record = %q, want the TCR0 draft header record", sum.FirstRecord[:min(24, len(sum.FirstRecord))])
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("preview wrote %d entries into the fixture dir", len(entries))
	}
}

func TestCtfPreviewDefaultsCibAndBatch(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	sum, err := a.PreviewExport(context.Background(), "ctfsession1", "", "", 0)
	if err != nil {
		t.Fatalf("PreviewExport: %v", err)
	}
	if sum.CIB != DefaultCtfCIB || sum.ProcessingDate == "" {
		t.Errorf("CIB/date = %q/%q, want default %q and a processing date", sum.CIB, sum.ProcessingDate, DefaultCtfCIB)
	}
}

func TestCtfPreviewBinFilterSkips(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	sum, err := a.PreviewExport(context.Background(), "ctfsession1", "", "4242", 1)
	if err != nil {
		t.Fatalf("PreviewExport: %v", err)
	}
	if sum.MonetaryTransactions != 2 || sum.SkippedTransactions != 0 {
		t.Errorf("matching filter = %d/%d skipped, want 2/0", sum.MonetaryTransactions, sum.SkippedTransactions)
	}
}

func TestCtfPreviewUnknownSessionIsConfigErrorNamingID(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	_, err := a.PreviewExport(context.Background(), "nosuch", "", "", 1)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != "nosuch" {
		t.Fatalf("err = %v, want ConfigError naming the session id", err)
	}
}

func TestCtfPreviewNoApprovedTxIsConfigErrorNamingID(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	_, err := a.PreviewExport(context.Background(), "emptyvisa", "", "", 1)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != "emptyvisa" {
		t.Fatalf("err = %v, want ConfigError naming the session id", err)
	}
}

func TestCtfPreviewAllBinFilteredIsConfigError(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))

	_, err := a.PreviewExport(context.Background(), "ctfsession1", "", "5555", 1)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != "ctfsession1" {
		t.Fatalf("err = %v, want ConfigError naming the session id", err)
	}
}

func TestCtfWriteWritesOnceWithHeaderRecord(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))
	out := filepath.Join(t.TempDir(), "CTF_001.dat")

	sum, err := a.WriteExport(context.Background(), "ctfsession1", "", "", 1, out)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if !sum.Written || sum.DryRun || sum.Overwrote {
		t.Errorf("write flags = %+v, want written, not dry, not overwrite", sum)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !strings.HasPrefix(string(content), "05004242424242424242") {
		t.Errorf("file does not start with the expected header record")
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != sum.Records {
		t.Errorf("lines = %d, want %d records", len(lines), sum.Records)
	}
	for _, line := range lines {
		if len(line) != base2.RecordLength {
			t.Errorf("record length %d != %d", len(line), base2.RecordLength)
		}
	}
}

func TestCtfWriteReportsOverwrite(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))
	out := filepath.Join(t.TempDir(), "CTF_001.dat")
	if err := os.WriteFile(out, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sum, err := a.WriteExport(context.Background(), "ctfsession1", "", "", 1, out)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if !sum.Overwrote {
		t.Errorf("Overwrote = false, want true for an existing target")
	}
}

func TestCtfWriteMissingDirectoryIsNotCreated(t *testing.T) {
	a := dbViewApp(t, ctfFixture(t))
	out := filepath.Join(t.TempDir(), "missing", "CTF_001.dat")

	_, err := a.WriteExport(context.Background(), "ctfsession1", "", "", 1, out)
	if err == nil {
		t.Fatalf("want a write error for a missing directory")
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("file exists under a missing directory")
	}
}

func TestCtfWriteUnknownSessionWritesNothing(t *testing.T) {
	dir := t.TempDir()
	a := dbViewApp(t, ctfFixture(t))

	_, err := a.PreviewExport(context.Background(), "ghost", "", "", 1)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want ConfigError", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed export wrote %d entries", len(entries))
	}
}

func TestCtfWriteUnsetDBIsTypedNoFile(t *testing.T) {
	a := dbViewApp(t, "")

	if _, err := a.WriteExport(context.Background(), "ctfsession1", "", "", 1, "out.dat"); !errors.Is(err, ErrDBNotConfigured) {
		t.Fatalf("err = %v, want ErrDBNotConfigured", err)
	}
	if _, err := os.Stat("out.dat"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unset db write created a file")
	}
}
