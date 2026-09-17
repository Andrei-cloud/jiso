package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TestTxEnterYieldsDetailMsg: Enter on the selected row yields exactly
// TxDetailMsg{ID} (root owns the §C transition).
func TestTxEnterYieldsDetailMsg(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j')) // name asc: [Echo MC, Purchase, ...]

	_, cmd := p.Update(tea.KeyPressMsg{Code: '\r'})
	if cmd == nil {
		t.Fatal("enter returned no cmd")
	}
	if got, ok := cmd().(TxDetailMsg); !ok || got.ID != txPurchase {
		t.Errorf("enter dispatched %#v, want TxDetailMsg{Purchase}", cmd())
	}
}

// TestTxSendYieldsSendMsg: s yields TxSendMsg{ID} for the selected row
// (root owns the §D flow).
func TestTxSendYieldsSendMsg(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j'))

	_, cmd := p.Update(press('s'))
	if cmd == nil {
		t.Fatal("s returned no cmd")
	}
	if got, ok := cmd().(TxSendMsg); !ok || got.ID != txPurchase {
		t.Errorf("s dispatched %#v, want TxSendMsg{Purchase}", cmd())
	}
}

// f yields TxPickFileMsg even in the empty state.
func TestTxPickFileMsg(t *testing.T) {
	t.Parallel()

	for _, state := range []TransactionsState{{}, populatedState()} {
		_, cmd := txPage(t, state, 120, 32).Update(press('f'))
		if cmd == nil {
			t.Fatalf("f returned no cmd (state %+v)", state.FileName)
		}
		if _, ok := cmd().(TxPickFileMsg); !ok {
			t.Errorf("f dispatched %T, want TxPickFileMsg", cmd())
		}
	}
}

// TestTxEmptyViewNoRowMsgs: Enter/s with no rows are nil-cmd no-ops.
func TestTxEmptyViewNoRowMsgs(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "zzz")

	for _, key := range []rune{'\r', 's'} {
		_, cmd := p.Update(press(key))
		if cmd != nil {
			t.Errorf("key %q on empty view returned a cmd", key)
		}
	}
}

// TestTxNavKeysMoveCursor: j/k/arrows/pgup/pgdn drive the filtered view.
func TestTxNavKeysMoveCursor(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)

	_, _ = p.Update(press('j'))
	_, _ = p.Update(press('j'))
	if p.table.Cursor() != 2 {
		t.Fatalf("j j: cursor = %d, want 2", p.table.Cursor())
	}
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.table.Cursor() != 1 {
		t.Fatalf("up: cursor = %d, want 1", p.table.Cursor())
	}
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if p.table.Cursor() != 0 {
		t.Fatalf("home: cursor = %d, want 0", p.table.Cursor())
	}
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if p.table.Cursor() != 3 {
		t.Fatalf("end: cursor = %d, want 3", p.table.Cursor())
	}
	_, _ = p.Update(press('k'))
	if p.selectedID != txReversal {
		t.Errorf("k: selected = %q, want Reversal", p.selectedID)
	}
}

// TestTxNavInsideFilterStaysLive: arrows navigate while the filter owns
// the keyboard, and printable keys (j included) type into the filter
// instead of stealing navigation (a live filter must be typeable).
func TestTxNavInsideFilterStaysLive(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "e")
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if !p.filtering {
		t.Fatal("down must not exit filter mode")
	}
	if p.table.Cursor() != 1 {
		t.Errorf("cursor while filtering = %d, want 1", p.table.Cursor())
	}
	_, _ = p.Update(press('j'))
	if p.filter != "ej" || p.table.Cursor() != 0 {
		t.Errorf("j must type: filter=%q cursor=%d", p.filter, p.table.Cursor())
	}
}

// TestTxUnknownKeysIgnored: unbound keys and foreign messages leave the
// page untouched with nil commands (Update purity).
func TestTxUnknownKeysIgnored(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	_, _ = p.Update(press('j'))

	type stranger struct{ N int }
	// t is deliberately in the list: unbound on §B (picker moved to f),
	// so it must be an ignored key.
	for _, msg := range []tea.Msg{press('x'), press('q'), press('t'), stranger{1}, nil} {
		next, cmd := p.Update(msg)
		if cmd != nil {
			t.Errorf("Update(%T) returned a cmd", msg)
		}
		q, ok := next.(*Transactions)
		if !ok {
			t.Fatalf("Update(%T) = %T, want *Transactions", msg, next)
		}
		if q.table.Cursor() != 1 || len(q.view) != 4 || q.filtering {
			t.Errorf("Update(%T) mutated page state", msg)
		}
	}
}

// TestTxClaimsKeyboardOnlyWhileTyping: the claim flips on with '/', off
// with esc/enter; navigation never claims.
func TestTxClaimsKeyboardOnlyWhileTyping(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	if p.ClaimsKeyboard() {
		t.Fatal("claims keyboard before '/'")
	}
	_, _ = p.Update(press('/'))
	if !p.ClaimsKeyboard() {
		t.Fatal("must claim the keyboard in filter mode")
	}
	_, _ = p.Update(press('q')) // typed into the filter, not a quit
	if !p.ClaimsKeyboard() || p.filter != "q" {
		t.Errorf("q must type into the filter: filtering=%v filter=%q", p.filtering, p.filter)
	}
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.ClaimsKeyboard() {
		t.Error("esc must release the keyboard")
	}
}

// TestTxResponsiveWidths: at 120/90/70 columns the body never exceeds
// the content width and always carries the title row (truncate, never
// wrap; the DESCRIPTION flex column absorbs the shortfall).
func TestTxResponsiveWidths(t *testing.T) {
	t.Parallel()

	for _, w := range []int{120, 90, 70} {
		p := txPage(t, populatedState(), w, 24)
		lines := strings.Split(txBody(t, p), "\n")
		for i, line := range lines {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("w=%d line %d overflows (%d cells): %q", w, i, lw, line)
			}
		}
		if !strings.Contains(lines[0], "sort: name ^") || !strings.Contains(lines[0], "pool.json (4)") {
			t.Errorf("w=%d title row damaged: %q", w, lines[0])
		}
	}
}

// TestTxSetStatePreservesSelection: a re-pushed snapshot (root syncs on
// every Update) keeps filter, sort, and selection.
func TestTxSetStatePreservesSelection(t *testing.T) {
	t.Parallel()

	p := txPage(t, populatedState(), 120, 32)
	typeFilter(t, p, "ar")
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // j would type into the filter
	p.SetState(populatedState())

	if p.filter != "ar" || len(p.view) != 3 {
		t.Errorf("sync dropped filter: %q %v", p.filter, viewIDs(p))
	}
	if p.selectedID != txPurchase {
		t.Errorf("sync selection = %q, want Purchase", p.selectedID)
	}
}
