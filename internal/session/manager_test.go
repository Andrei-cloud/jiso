package session

import (
	"testing"

	"jiso/internal/config"
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
