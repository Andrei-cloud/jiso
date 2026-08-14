package session

import (
	"path/filepath"
	"testing"

	"jiso/internal/config"
	"jiso/internal/db"
)

func TestSessionManagerLifecycle(t *testing.T) {
	mgr := GetManager()

	config.GetConfig().Reset()
	s1, err := mgr.StartSession("specs/spec.json", "transactions/transactions.json")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	if s1 == "" {
		t.Fatalf("Expected non-empty session ID")
	}

	if mgr.GetCurrentSessionID() != s1 {
		t.Fatalf("Expected session ID %s, got %s", s1, mgr.GetCurrentSessionID())
	}

	s2, err := mgr.RotateSession("specs/spec.json", "transactions/transactions.json")
	if err != nil {
		t.Fatalf("RotateSession failed: %v", err)
	}

	if s2 == "" || s2 == s1 {
		t.Fatalf("Expected new unique session ID after rotation, got %s (old %s)", s2, s1)
	}

	if mgr.GetCurrentSessionID() != s2 {
		t.Fatalf("Expected current session ID %s, got %s", s2, mgr.GetCurrentSessionID())
	}
}

func TestSessionRotationOnSpecAndTxChange(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "session_rotation_test.db")

	config.GetConfig().Reset()
	config.GetConfig().SetDbPath(dbPath)

	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	mgr := GetManager()

	// Initial session
	s1, err := mgr.StartSession("specs/spec.json", "transactions/transaction.json")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	rec1, err := db.GetSessionByID(s1)
	if err != nil || rec1 == nil {
		t.Fatalf("Failed to fetch session %s from DB: %v", s1, err)
	}
	if rec1.SpecName != "spec.json" || rec1.TxFileName != "transaction.json" || rec1.Status != "active" {
		t.Errorf("Unexpected rec1 state: %+v", rec1)
	}

	// Change spec to visa.json -> should create new session s2
	s2, err := mgr.RotateSession("specs/visa.json", "transactions/transaction.json")
	if err != nil {
		t.Fatalf("RotateSession on spec change failed: %v", err)
	}
	if s2 == s1 {
		t.Errorf("Expected new session ID on spec change, got same %s", s2)
	}

	// Old session s1 should be marked reloaded
	rec1Updated, err := db.GetSessionByID(s1)
	if err != nil || rec1Updated.Status != "reloaded" {
		t.Errorf("Expected rec1 status 'reloaded', got %+v", rec1Updated)
	}

	// New session s2 should be active with visa.json
	rec2, err := db.GetSessionByID(s2)
	if err != nil || rec2.SpecName != "visa.json" || rec2.Status != "active" {
		t.Errorf("Unexpected rec2 state: %+v", rec2)
	}

	// Change tx file to purchase.json -> should create new session s3
	s3, err := mgr.RotateSession("specs/visa.json", "transactions/purchase.json")
	if err != nil {
		t.Fatalf("RotateSession on tx change failed: %v", err)
	}
	if s3 == s2 || s3 == s1 {
		t.Errorf("Expected new session ID on tx change, got %s", s3)
	}

	// New session s3 should be active with purchase.json
	rec3, err := db.GetSessionByID(s3)
	if err != nil || rec3.TxFileName != "purchase.json" || rec3.SpecName != "visa.json" || rec3.Status != "active" {
		t.Errorf("Unexpected rec3 state: %+v", rec3)
	}
}

