package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/widgets"
)

// TestTxPageInterface proves *Transactions implements Page without the
// pages package importing its parent.
var _ Page = (*Transactions)(nil)

// tx fixture: the §B sample in file order.
const (
	txPurchase = "Purchase"
	txReversal = "Reversal"
	txSignOn   = "Sign On"
	txEchoMC   = "Echo MC"
)

// populatedState is the §B snapshot (pool.json with four transactions).
func populatedState() TransactionsState {
	return TransactionsState{FileName: "pool.json", TxCount: 4, Rows: []TxRow{
		{ID: txPurchase, Name: "Purchase", MTI: "0200", Description: "Purchase authorization", Dataset: "card_pool", Spec: "(global)"},
		{ID: txReversal, Name: "Reversal", MTI: "0420", Description: "Reversal of purchase", Dataset: "card_pool", Spec: "(global)"},
		{ID: txSignOn, Name: "Sign On", MTI: "0800", Description: "Network management sign on"},
		{ID: txEchoMC, Name: "Echo MC", MTI: "0800", Description: "Echo test", Spec: "mastercard"},
	}}
}

// txPage builds an ascii-themed page with the state pushed and sized.
func txPage(t *testing.T, state TransactionsState, w, h int) *Transactions {
	t.Helper()

	p := NewTransactions(asciiTheme(t))
	p.SetState(state)
	_, _ = p.Update(windowSize(w, h))

	return p
}

// typeFilter drives the live filter: '/' then one press per rune.
func typeFilter(t *testing.T, p *Transactions, s string) {
	t.Helper()

	_, _ = p.Update(press('/'))
	for _, r := range s {
		_, _ = p.Update(press(r))
	}
}

// viewIDs is the page's recomposed view order (identity check).
func viewIDs(p *Transactions) []string {
	ids := make([]string, len(p.view))
	for i, r := range p.view {
		ids[i] = r.ID
	}

	return ids
}

// equalIDs compares a view order against a wanted ID sequence.
func equalIDs(t *testing.T, p *Transactions, want ...string) bool {
	t.Helper()

	got := viewIDs(p)
	if len(got) != len(want) {
		t.Errorf("view = %v, want %v", got, want)

		return false
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("view = %v, want %v", got, want)

			return false
		}
	}

	return true
}

// txBody renders the body lines joined for substring assertions.
func txBody(t *testing.T, p *Transactions) string {
	t.Helper()

	return strings.TrimRight(p.View().Content, "\n")
}

// TestTxIDAndSlot: the transactions page fills the "transactions" slot 2 so
// palette jumps, hotkey 2, and the program goldens keep working.
func TestTxIDAndSlot(t *testing.T) {
	t.Parallel()

	if got := NewTransactions(nil).ID(); got != "transactions" {
		t.Errorf("ID() = %q, want transactions", got)
	}
}

// TestTxEmptyStateText: no tx file loaded renders the line
// (ascii dash) plus the always-present §B title row.
func TestTxEmptyStateText(t *testing.T) {
	t.Parallel()

	p := txPage(t, TransactionsState{}, 120, 32)
	body := txBody(t, p)

	if !strings.Contains(body, "no tx file loaded - f to pick file") {
		t.Errorf("empty state lacks the wireframe line:\n%s", body)
	}
	if !strings.Contains(body, "TRANSACTIONS") || !strings.Contains(body, "- (0)") {
		t.Errorf("empty state lacks the title row:\n%s", body)
	}
	if strings.Contains(body, "NAME") {
		t.Errorf("empty state must not render the table:\n%s", body)
	}
}

// TestTxHeaderFileLabelAndSort: the header shows the file label with the
// count, the filter slot (empty renders the caret, "filter:
// ▏"/ascii "|"), and the sort indicator (default name asc).
func TestTxHeaderFileLabelAndSort(t *testing.T) {
	t.Parallel()

	body := txBody(t, txPage(t, populatedState(), 120, 32))

	for _, want := range []string{"pool.json (4)", "filter: |", "sort: name ^"} {
		if !strings.Contains(body, want) {
			t.Errorf("header lacks %q:\n%s", want, body)
		}
	}
}

// TestTxDashesForEmptyFields: unknown dataset/spec cells render the dash.
func TestTxDashesForEmptyFields(t *testing.T) {
	t.Parallel()

	body := txBody(t, txPage(t, populatedState(), 120, 32))
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Sign On") && !strings.Contains(line, "-") {
			t.Errorf("Sign On row must dash its unknown fields:\n%s", body)
		}
	}
}

// DatasetCell renders the DATASET display string (D-10G): a resolved dataset
// shows "name (N)" with its row count, a referenced-but-missing one shows the
// bare name (inventing a count would lie), and "" is left for dashIf.
func TestDatasetCell(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		rows int
		want string
	}{
		{"pool", 3, "pool (3)"},
		{"inline", 2, "inline (2)"},
		{"gone", -1, "gone"},
		{"", 0, ""},
	} {
		if got := DatasetCell(c.name, c.rows); got != c.want {
			t.Errorf("DatasetCell(%q, %d) = %q, want %q", c.name, c.rows, got, c.want)
		}
	}
}

// TestTxDatasetSpecCells: DATASET/SPEC cells render the strings root
// pre-derived verbatim — "pool (3)", "inline (2)", a bare "gone" (missing
// dataset, no count) — and empty cells render the dash. Columns are widened
// past the shipped widths here so no cell truncates at the asserted strings.
func TestTxDatasetSpecCells(t *testing.T) {
	t.Parallel()

	p := NewTransactions(asciiTheme(t))
	p.table.SetColumns([]widgets.Column{
		{Title: "NAME", Width: 10},
		{Title: "MTI", Width: 6},
		{Title: "DESCRIPTION", Width: 22, Flex: true},
		{Title: "DATASET", Width: 12},
		{Title: "SPEC", Width: 12},
	})
	p.SetState(TransactionsState{FileName: "pool.json", TxCount: 3, Rows: []TxRow{
		{ID: "alpha", Name: "Alpha", MTI: "0200", Description: "declared both", Dataset: "pool (3)", Spec: "flex.json"},
		{ID: "bravo", Name: "Bravo", MTI: "0200", Description: "inline rows", Dataset: "inline (2)"},
		{ID: "charlie", Name: "Charlie", MTI: "0400", Description: "no dataset", Dataset: "gone"},
	}})
	_, _ = p.Update(windowSize(120, 32))
	body := txBody(t, p)

	for _, want := range []string{"pool (3)", "inline (2)", "gone", "flex.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("cells lack %q:\n%s", want, body)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "Bravo") {
			continue
		}
		if !strings.Contains(line, "inline (2)") || strings.Contains(line, "flex.json") {
			t.Errorf("Bravo row must show its dataset and dash its spec:\n%s", line)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "Charlie") {
			continue
		}
		if strings.Contains(line, "flex.json") {
			t.Errorf("Charlie row must dash its spec:\n%s", line)
		}
	}
}

// TestTxFilterNarrowsLive: '/' + typing filters as you type (substring,
// case-insensitive) and the header echoes the filter text with the caret.
func TestTxFilterNarrowsLive(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "ECH")

	if !equalIDs(t, p, txEchoMC) {
		t.Fatalf("filter ECH: %v", viewIDs(p))
	}
	if !strings.Contains(txBody(t, p), "filter: ECH|") {
		t.Errorf("header lacks the live filter text+caret:\n%s", txBody(t, p))
	}
}

// The filter narrows the sorted view and keeps the sort order.
func TestTxFilterComposesWithSort(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('o')) // name desc
	typeFilter(t, p, "ar")      // card_pool + mastercard rows only

	equalIDs(t, p, txReversal, txPurchase, txEchoMC)
}

// TestTxSelectionPreservedAcrossFilter: the selected row keeps selection
// when it still matches a new filter.
func TestTxSelectionPreservedAcrossFilter(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j'))
	_, _ = p.Update(press('j')) // name asc: Echo MC, Purchase, Reversal, Sign On
	if p.selectedID != txReversal {
		t.Fatalf("cursor = %q, want %q", p.selectedID, txReversal)
	}

	typeFilter(t, p, "420") // only the Reversal matches
	if p.selectedID != txReversal {
		t.Errorf("selection dropped by filter: %q", p.selectedID)
	}
}

// TestTxSelectionClampsWhenSelectedDropped: filtering out the selected
// row clamps the cursor to the last row of the new view.
func TestTxSelectionClampsWhenSelectedDropped(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j'))
	_, _ = p.Update(press('j'))
	_, _ = p.Update(press('j')) // Sign On (last)
	typeFilter(t, p, "purchase a")

	if p.selectedID != txPurchase {
		t.Errorf("clamp: selected = %q, want %q", p.selectedID, txPurchase)
	}
	if p.table.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", p.table.Cursor())
	}
}

// TestTxSelectionClampsEmptyView: a filter matching nothing clamps to an
// empty view with a distinct empty message.
func TestTxSelectionClampsEmptyView(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j'))
	typeFilter(t, p, "zzz")

	if len(p.view) != 0 || p.table.Cursor() != 0 || p.selectedID != "" {
		t.Errorf("empty view state: len=%d cursor=%d sel=%q", len(p.view), p.table.Cursor(), p.selectedID)
	}
	if !strings.Contains(txBody(t, p), "no transactions match filter") {
		t.Errorf("lacks filter-empty message:\n%s", txBody(t, p))
	}
}

// TestTxFilterEscClears: esc clears the filter and exits filter mode.
func TestTxFilterEscClears(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "zz")
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if p.filtering || p.filter != "" || len(p.view) != 4 {
		t.Errorf("esc: filtering=%v filter=%q view=%v", p.filtering, p.filter, viewIDs(p))
	}
}

// TestTxFilterBackspace: backspace drops one rune (rune-safe).
func TestTxFilterBackspace(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "pur")
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	if p.filter != "pu" {
		t.Errorf("filter = %q, want pu", p.filter)
	}
}

// TestTxFilterEnterKeepsFilter: enter applies the filter and releases
// the keyboard (ClaimsKeyboard false again).
func TestTxFilterEnterKeepsFilter(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "ech")
	_, _ = p.Update(tea.KeyPressMsg{Code: '\r'})

	if p.filtering || p.filter != "ech" || len(p.view) != 1 {
		t.Errorf("enter: filtering=%v filter=%q view=%v", p.filtering, p.filter, viewIDs(p))
	}
}

// o walks name asc → name desc → mti asc → mti desc → description asc →
// description desc → wraps to name asc.
func TestTxSortCycleOrderPerColumn(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)

	steps := []struct {
		col  int
		asc  bool
		want []string
	}{
		{0, true, []string{txEchoMC, txPurchase, txReversal, txSignOn}},
		{0, false, []string{txSignOn, txReversal, txPurchase, txEchoMC}},
		{1, true, []string{txPurchase, txReversal, txSignOn, txEchoMC}},
		{1, false, []string{txSignOn, txEchoMC, txReversal, txPurchase}},
		{2, true, []string{txEchoMC, txSignOn, txPurchase, txReversal}},
		{2, false, []string{txReversal, txPurchase, txSignOn, txEchoMC}},
		{0, true, []string{txEchoMC, txPurchase, txReversal, txSignOn}},
	}
	for i, s := range steps {
		if i > 0 {
			_, _ = p.Update(press('o'))
		}
		if p.table.SortCol() != s.col || p.table.SortAsc() != s.asc {
			t.Fatalf("step %d: sortCol=%d sortAsc=%v", i, p.table.SortCol(), p.table.SortAsc())
		}
		equalIDs(t, p, s.want...)
	}
}

// TestTxSortStable: equal keys keep file order (both 0800 rows).
func TestTxSortStable(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('o'))
	_, _ = p.Update(press('o')) // mti asc

	ids := viewIDs(p)
	if ids[2] != txSignOn || ids[3] != txEchoMC {
		t.Errorf("0800 group not stable: %v", ids)
	}
}
