// settingsapply_order_test.go pins the one hard ordering rule of a
// two-key patch: the spec must resolve BEFORE the tx-file is built, so a
// file the OLD spec rejects at load time composes against the spec chosen
// in the same patch (specFirstKeys; the load is the bind — a later
// SetSpec swap cannot undo a wrongly-ordered build).
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplySettingsTwoKeyPatchSpecBindsLoad(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()
	oldSpec := filepath.Join("..", "..", "specs", "spec.json")
	chosen := filepath.Join("..", "..", "specs", "flex.json")

	// Field 31 is String/ASCII.Fixed(9) in spec.json and absent from
	// flex.json: the one-character value loads only against flex.
	tx := filepath.Join(dir, "tx31.json")
	if err := os.WriteFile(tx, []byte(`[{"type":"transaction","name":"Alpha","description":"d","fields":{"0":"0200","31":"1"}}]`), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	if errs := a.ApplySettings(t.Context(), map[string]string{"spec": oldSpec}); errs != nil {
		t.Fatalf("apply old spec: %v", errs)
	}
	// Counterfactual first: the old spec really refuses this file at
	// load, so a clean combined patch can only come from spec-first.
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": tx}); len(errs) == 0 {
		t.Fatal("the old spec must reject the file at load (the ordering discriminator is live)")
	}

	if errs := a.ApplySettings(t.Context(), map[string]string{"spec": chosen, "tx-file": tx}); errs != nil {
		t.Fatalf("combined patch must build the file against the new spec: %v", errs)
	}
	if got := a.Config().GetFile(); got != tx {
		t.Fatalf("config file = %q, want %q", got, tx)
	}
	if names := a.Transactions().ListNames(); len(names) != 1 || names[0] != "Alpha" {
		t.Fatalf("loaded transactions = %v, want [Alpha] bound to the chosen spec", names)
	}
}
