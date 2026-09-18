package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// txFixtureJSON is a two-transaction file (§B sample, trimmed).
const txFixtureJSON = `[
 {"type":"transaction","name":"Purchase","description":"Purchase authorization","fields":{"0":"0200","7":"auto"}},
 {"type":"transaction","name":"Sign On","description":"Network Management: Sign On","fields":{"0":"0800","70":1}}
]`

// txSpecDatasetFixtureJSON covers the §B DATASET/SPEC cell sources: a
// declared spec, three inline rows, a referenced pool with three rows, a
// dangling reference, and a bare entry (no declared spec despite the
// global one being set — the cell must stay honest).
const txSpecDatasetFixtureJSON = `[
 {"type":"transaction","name":"Echo","description":"Network Management: Echo","spec":"specs/flex.json","fields":{"0":"0800"}},
 {"type":"transaction","name":"Purchase","description":"Purchase authorization","dataset":[{"2":"4000000000000002"},{"2":"4000000000000003"},{"2":"4000000000000004"}],"fields":{"0":"0200"}},
 {"type":"transaction","name":"Reversal","description":"Reversal of purchase","dataset_name":"card_pool","fields":{"0":"0420"}},
 {"type":"transaction","name":"Sign On","description":"Network Management: Sign On","dataset_name":"missing_pool","fields":{"0":"0800","70":1}},
 {"type":"transaction","name":"Bare","description":"Nothing declared","fields":{"0":"0800"}},
 {"type":"dataset","name":"card_pool","data":[{"2":"1111222233334444"},{"2":"1111222233335555"},{"2":"1111222233336666"}]}
]`

// newTxFileApp builds a real app whose tx file holds the fixture (the
// lifecycle_helpers_test.go singleton idiom; never run in parallel).
func newTxFileApp(t *testing.T) *app.App {
	t.Helper()

	return newTxApp(t, txFixtureJSON)
}

// newTxApp builds a real app over the given tx-file JSON (hermetic state
// dir; never run in parallel).
func newTxApp(t *testing.T, txJSON string) *app.App {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir()) // last-connection writes stay hermetic
	txFile := t.TempDir() + "/pool.json"
	if err := os.WriteFile(txFile, []byte(txJSON), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetSpec("../../specs/spec.json")
	cfg.SetFile(txFile)

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	return a
}

// TestRootTxSlotWired: slot 2 of a nil-app root is the real transactions
// page and hotkey 2 lands on it (slot 2, footer label "tx").
func TestRootTxSlotWired(t *testing.T) {
	m := NewRootModel(nil)
	if _, ok := m.registry[1].(*pages.Transactions); !ok {
		t.Fatalf("registry slot 2 = %T, want *pages.Transactions", m.registry[1])
	}

	_, _ = m.Update(ch('2'))
	wantStack(t, m, "transactions")
	if title := m.Current().Hints(); title == nil {
		t.Fatal("transactions page returned no hints")
	}
}

// TestRootTxEmptyWithoutApp: no App at construction → empty snapshot →
// the §B empty state renders in the frame body.
func TestRootTxEmptyWithoutApp(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))

	content := m.View().Content
	if !strings.Contains(content, "no tx file loaded - f to pick file") {
		t.Errorf("frame lacks the empty state:\n%s", content)
	}
}

// TestRootTxStateFromApp: root derives the snapshot from the app's tx
// file — file label, MTI parsed from field 0, description; the page
// itself never touches the app.
func TestRootTxStateFromApp(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))

	content := m.View().Content
	for _, want := range []string{"pool.json (2)", "0200", "Purchase authorization", "0800"} {
		if !strings.Contains(content, want) {
			t.Errorf("frame lacks %q:\n%s", want, content)
		}
	}
}

// TestRootTxSpecDatasetCells: the snapshot builder pre-derives the DATASET
// and SPEC cells — basename of the declared spec (never the global
// fallback), "inline (N)" for inline rows, "name (N)" for a hit, bare
// "name" for a dangling reference, "" (dash) for nothing declared.
func TestRootTxSpecDatasetCells(t *testing.T) {
	m := NewRootModel(newTxApp(t, txSpecDatasetFixtureJSON))

	want := map[string][2]string{ // name → {dataset cell, spec cell}
		"Echo":     {"", "flex.json"},
		"Purchase": {"inline (3)", ""},
		"Reversal": {"card_pool (3)", ""},
		"Sign On":  {"missing_pool", ""},
		"Bare":     {"", ""},
	}
	rows := m.transactionsState().Rows
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for _, r := range rows {
		w, ok := want[r.Name]
		if !ok {
			t.Fatalf("unexpected row %q", r.Name)
		}
		if r.Dataset != w[0] {
			t.Errorf("%s: Dataset cell = %q, want %q", r.Name, r.Dataset, w[0])
		}
		if r.Spec != w[1] {
			t.Errorf("%s: Spec cell = %q, want %q", r.Name, r.Spec, w[1])
		}
	}
}

// on §B `f` opens the shared tx-file picker and `o` cycles the sort.
func TestTransactionsFOpenPickerOSort(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2')) // §B

	pumpKey(m, ch('f')) // f opens the tx-file picker (was t)
	if m.filePick == nil {
		t.Fatal("'f' must open the tx-file picker")
	}
	pumpKey(m, special(tea.KeyEscape))
	if m.filePick != nil {
		t.Fatal("esc must close the picker back to §B")
	}

	before := m.View().Content
	pumpKey(m, ch('o')) // o cycles the sort (was f)
	if m.View().Content == before {
		t.Fatal("'o' must cycle the sort")
	}
}

// TestRootTxRowMsgsAreNoOps: s/f (send/picker) and the §C compose msg
// reach root, change nothing, and run no command (the picker land
// later). Enter (TxDetailMsg) is wired and lives in
// root_inspector_test.go.
func TestRootTxRowMsgsAreNoOps(t *testing.T) {
	m := NewRootModel(nil)

	for _, msg := range []tea.Msg{
		pages.TxSendMsg{ID: "Purchase"},
		pages.TxPickFileMsg{},
		pages.TxComposeMsg{ID: "Purchase"},
	} {
		_, cmd := m.Update(msg)
		if isQuit(t, cmd) {
			t.Fatalf("%T quit the program", msg)
		}
		if cmd != nil {
			t.Errorf("%T ran a command: %v", msg, cmd())
		}
		wantStack(t, m, "dashboard")
	}
}

// TestRootFilterClaimsKeyboard: while the live filter is open, keys that
// collide with the global layer (2, q, :, ?) type into the filter; esc
// releases them again; ctrl+c stays global.
func TestRootFilterClaimsKeyboard(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(ch('2'))

	_, _ = m.Update(ch('/'))
	for _, c := range "q2:?" {
		_, _ = m.Update(ch(c))
	}
	wantStack(t, m, "transactions")
	if m.pal != nil {
		t.Error(": opened the palette while the filter claimed the keyboard")
	}
	if got, _ := m.tx.Filter(); got != "q2:?" {
		t.Errorf("filter = %q, want q2:?", got)
	}

	_, _ = m.Update(special(tea.KeyEscape))
	wantStack(t, m, "transactions")
	_, cmd := m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Error("q must arm the quit confirmation, not quit (UAT)")
	}
	if !isQuit(t, quitPump(t, m, 'y')) {
		t.Error("y must confirm the quit after esc releases the filter")
	}
}

// TestRootFilterSurvivesSync: root re-pushes the snapshot on every Update
// (dashboard pattern); the pushed state must not reset selection inside
// the filtered view.
func TestRootFilterSurvivesSync(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))

	_, _ = m.Update(ch('/'))
	_, _ = m.Update(ch('u')) // "u" matches only Purchase ("Purchase authorization")
	if got, _ := m.tx.Filter(); got != "u" {
		t.Fatalf("filter = %q, want u", got)
	}

	_, _ = m.Update(special(tea.KeyDown)) // stays clamped onto Purchase
	_, _ = m.Update(special(tea.KeyDown))
	_, _ = m.Update(special(tea.KeyRight)) // non-printable: nav, then a full root sync
	if got := m.tx.SelectedID(); got != "Purchase" {
		t.Errorf("sync dropped selection: %q", got)
	}
}
