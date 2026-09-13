// settingsapply_reload_test.go pins the live-reload fix: applying a new
// spec or tx-file must swap the LIVE collection/service spec (the send
// path composes from them), not just rewrite config values.
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTxFile(t *testing.T, dir, name, txName string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	body := `[{"type":"transaction","name":"` + txName + `","description":"d","fields":{"0":"0800"}}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	return path
}

func TestApplySettingsTxFileSwapsLiveCollection(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()
	tx1 := writeTxFile(t, dir, "tx1.json", "Alpha")
	tx2 := writeTxFile(t, dir, "tx2.json", "Bravo")

	if got := a.Transactions().ListNames(); len(got) != 0 {
		t.Fatalf("fresh app names = %v, want empty", got)
	}
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": tx1}); errs != nil {
		t.Fatalf("apply tx-file 1: %v", errs)
	}
	if got := a.Transactions().ListNames(); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("live collection after tx-file 1 = %v, want [Alpha]", got)
	}
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": tx2}); errs != nil {
		t.Fatalf("apply tx-file 2: %v", errs)
	}
	if got := a.Transactions().ListNames(); len(got) != 1 || got[0] != "Bravo" {
		t.Fatalf("live collection after tx-file 2 = %v, want [Bravo]", got)
	}
}

func TestApplySettingsRejectedTxFileKeepsCollection(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()
	tx1 := writeTxFile(t, dir, "tx1.json", "Alpha")
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": tx1}); errs != nil {
		t.Fatalf("apply tx-file: %v", errs)
	}

	// A directory is a valid path but not a loadable collection file.
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": dir}); len(errs) == 0 {
		t.Fatal("a non-file path must report a per-field error")
	}
	if got := a.Transactions().ListNames(); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("collection must survive the rejected patch: %v", got)
	}
	if got := a.Config().GetFile(); got != tx1 {
		t.Fatalf("config file must survive the rejected patch: %q", got)
	}
}

func TestApplySettingsSpecSwapsServiceAndCollection(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()
	tx1 := writeTxFile(t, dir, "tx1.json", "Alpha")
	if errs := a.ApplySettings(t.Context(), map[string]string{"tx-file": tx1}); errs != nil {
		t.Fatalf("apply tx-file: %v", errs)
	}

	spec := filepath.Join("..", "..", "specs", "spec.json")
	if errs := a.ApplySettings(t.Context(), map[string]string{"spec": spec}); errs != nil {
		t.Fatalf("apply spec: %v", errs)
	}
	if a.Service().GetSpec() == nil {
		t.Fatal("service spec must be loaded by the spec apply")
	}
	// The collection survives the spec swap (the new spec is its fallback).
	if got := a.Transactions().ListNames(); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("collection after spec apply = %v, want [Alpha]", got)
	}
}

func TestApplySettingsSpecThenTxFileUsesNewSpec(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()
	spec := filepath.Join("..", "..", "specs", "spec.json")
	tx1 := writeTxFile(t, dir, "tx1.json", "Alpha")

	// One patch, both keys: the loop must apply spec before tx-file
	// (map order would otherwise compose the file against the old spec).
	if errs := a.ApplySettings(t.Context(), map[string]string{"spec": spec, "tx-file": tx1}); errs != nil {
		t.Fatalf("combined patch: %v", errs)
	}
	if a.Service().GetSpec() == nil {
		t.Fatal("service spec must be swapped by the combined patch")
	}
	if got := a.Transactions().ListNames(); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("collection after combined patch = %v, want [Alpha]", got)
	}
}
