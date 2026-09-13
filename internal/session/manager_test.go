package session

import (
	"path/filepath"
	"testing"

	"jiso/internal/config"
	"jiso/internal/db"
)

// TestStartSessionRecordsRow pins the one live Manager operation: it
// hands out the config session id and records the spec/tx-file context
// the session browser lists. (The rotate/update-or-rotate helpers were
// test-only dead code and were removed.)
func TestStartSessionRecordsRow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "session_start_test.db")

	config.GetConfig().Reset()
	config.GetConfig().SetDbPath(dbPath)

	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mgr := GetManager()

	s1, err := mgr.StartSession("specs/spec.json", "transactions/transaction.json")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if s1 == "" {
		t.Fatal("StartSession returned an empty session id")
	}
	if got := config.GetConfig().GetSessionID(); got != s1 {
		t.Fatalf("config session id = %s, want %s", got, s1)
	}

	rec, err := db.GetSessionByID(s1)
	if err != nil || rec == nil {
		t.Fatalf("GetSessionByID(%s): %v", s1, err)
	}
	if rec.SpecName != "spec.json" || rec.TxFileName != "transaction.json" || rec.Status != "active" {
		t.Errorf("recorded session row wrong: %+v", rec)
	}
}
