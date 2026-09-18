// txfilespec_test.go pins CountTransactionsWithoutSpec: the count must
// mirror the collection loader's entry semantics (spec / spec_file declare
// a specification; a legacy plain-array file makes every entry a specless
// transaction; datasets, scenarios and mock routes never count), and a
// file the loader could not even parse must surface as an error.
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCountFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

func TestCountTransactionsWithoutSpecCombinedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeCountFile(t, dir, "combined.json", `[
	 {"type":"transaction","name":"Speced","description":"d","fields":{"0":"0800"},"spec":"specs/flex.json"},
	 {"type":"transaction","name":"SpecFiled","description":"d","fields":{"0":"0800"},"spec_file":"specs/visa.json"},
	 {"type":"transaction","name":"Plain","description":"d","fields":{"0":"0800"}},
	 {"name":"Typeless","description":"d","fields":{"0":"0200"}},
	 {"type":"dataset","name":"pool","data":[{"2":"426"}]},
	 {"type":"scenario","name":"sc","steps":[{"name":"s1","use_transaction_id":"Plain"}]},
	 {"type":"mock_route","name":"rt","response_mti":"0810"}
	]`)

	n, err := CountTransactionsWithoutSpec(path)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2 (Plain + type-less; spec/spec_file entries never count)", n)
	}
}

func TestCountTransactionsWithoutSpecLegacyArray(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeCountFile(t, dir, "legacy.json", `[
	 {"name":"Old A","description":"d","fields":{"0":"0800"}},
	 {"name":"Old B","description":"d","fields":{"0":"0200"},"dataset":[{"2":"426"}]}
	]`)

	n, err := CountTransactionsWithoutSpec(path)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2 (a legacy plain array is every-entry specless)", n)
	}
}

func TestCountTransactionsWithoutSpecAllDeclared(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeCountFile(t, dir, "all.json", `[
	 {"type":"transaction","name":"A","description":"d","fields":{"0":"0800"},"spec":"specs/flex.json"},
	 {"type":"transaction","name":"B","description":"d","fields":{"0":"0800"},"spec_file":"specs/visa.json"},
	 {"type":"dataset","name":"pool","data":[{"2":"426"}]}
	]`)

	n, err := CountTransactionsWithoutSpec(path)
	if err != nil {
		t.Fatalf("count: %d, err %v", n, err)
	}
	if n != 0 {
		t.Fatalf("count = %d, want 0 (every entry declares a spec)", n)
	}
}

func TestCountTransactionsWithoutSpecEmptyFile(t *testing.T) {
	t.Parallel()

	path := writeCountFile(t, t.TempDir(), "empty.json", `[]`)

	n, err := CountTransactionsWithoutSpec(path)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
}

func TestCountTransactionsWithoutSpecUnparsable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bad := writeCountFile(t, dir, "bad.json", "not json at all")

	if _, err := CountTransactionsWithoutSpec(bad); err == nil {
		t.Fatal("a file both loader shapes reject must return an error")
	}
	if _, err := CountTransactionsWithoutSpec(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("a missing file must return an error")
	}
}
