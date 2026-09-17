package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// TestRootInspectorSlotWired: the inspector is a drill-down registered
// after the eight hotkey slots (registry[8]); hotkey 3 lands
// on the §F scenarios page, and the inspector's empty state renders
// when it is entered directly.
func TestRootInspectorSlotWired(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	if _, ok := m.registry[8].(*pages.Inspector); !ok {
		t.Fatalf("registry[8] = %T, want *pages.Inspector", m.registry[8])
	}

	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	m.Push(m.inspector)
	content := m.View().Content
	for _, want := range []string{"INSPECTOR", "Select a transaction first."} {
		if !strings.Contains(content, want) {
			t.Errorf("frame lacks %q:\n%s", want, content)
		}
	}

	_, _ = m.Update(ch('3'))
	wantStack(t, m, "scenarios")
}

// TestRootEnterOnTransactionsPushesInspector: Enter on §B (via the
// TxDetailMsg round-trip) pushes the inspector with state for that tx;
// the breadcrumb names the transaction.
func TestRootEnterOnTransactionsPushesInspector(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))

	openInspectorViaEnter(t, m)
	wantStack(t, m, "transactions", "inspector")

	content := m.View().Content
	for _, want := range []string{"Transactions > Purchase", "Messages(1/1)", "0200"} {
		if !strings.Contains(content, want) {
			t.Errorf("inspector frame lacks %q:\n%s", want, content)
		}
	}
}

// TestRootInspectorEscPops: Esc on the pushed inspector pops back to
// the transactions page (root owns the stack).
func TestRootInspectorEscPops(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	openInspectorViaEnter(t, m)

	_, cmd := m.Update(special(tea.KeyEscape))
	if isQuit(t, cmd) {
		t.Fatal("esc quit instead of popping")
	}
	if cmd == nil {
		t.Fatal("esc must yield the pop request")
	}
	if _, cmd := m.Update(cmd()); cmd != nil {
		t.Fatalf("pop msg ran a cmd: %v", cmd())
	}
	wantStack(t, m, "transactions")
}

// TestRootInspectorPopAtDepth1: InspectorPopMsg at depth 1 (the
// drill-down replacing the stack) is a no-op — the stack never empties.
// TestRootInspectorPopAtDepth1: Esc on a
// hotkey-jumped page at depth 1 navigates home to the dashboard (the
// stack never empties; it unwinds).
func TestRootInspectorPopAtDepth1(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Replace(m.inspector)

	_, cmd := m.Update(pages.InspectorPopMsg{})
	if cmd != nil {
		t.Fatalf("pop at depth 1 ran a cmd: %v", cmd())
	}
	wantStack(t, m, "dashboard")
}

// TestRootInspectorStateFromApp: the snapshot is built on the
// compose-without-send path — spec field names, the auto marker, and the
// shared pre-send validation line all render without a connection.
func TestRootInspectorStateFromApp(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	openInspectorViaEnter(t, m)

	content := m.View().Content
	// UAT: the fields tab is the Describe output; the tree's auto marker
	// is gone, the spec descriptions and the validation line stay.
	for _, want := range []string{"Message Type Indicator", "ISO8583", "required field"} {
		if !strings.Contains(content, want) {
			t.Errorf("inspector frame lacks %q:\n%s", want, content)
		}
	}
	if m.app.IsConnected() {
		t.Fatal("inspector state build must not connect")
	}
}

// TestRootInspectorTabReachesPage: Tab on the inspector arrives as a
// PaneFocusMsg and cycles the view tab (the router's pane-focus channel
// doubles as the §C tab switch).
func TestRootInspectorTabReachesPage(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	openInspectorViaEnter(t, m)

	_, _ = m.Update(special(tea.KeyTab))
	// The bitmap tab is index 1 of the fixed four-tab cycle.
	if got := m.inspector.Tab(); got != 1 {
		t.Fatalf("inspector tab = %d, want bitmap (1)", got)
	}
}

// TestRootInspectorStateSurvivesJumps: the canonical instance keeps its
// built state across a pop and a pair of hotkey jumps out and back.
func TestRootInspectorStateSurvivesJumps(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	openInspectorViaEnter(t, m)

	_, _ = m.Update(special(tea.KeyEscape))
	_, _ = m.Update(ch('3')) // away to scenarios (slot 3)
	_, _ = m.Update(ch('2')) // back to transactions
	openInspectorViaEnter(t, m)
	content := m.View().Content
	if !strings.Contains(content, "Transactions > Purchase") {
		t.Errorf("inspector state lost across jumps:\n%s", content)
	}
}

// TestRootInspectorComposeFromPage: Enter on a leaf row of the real
// inspector yields TxComposeMsg, which root treats as a logged no-op
// for now.
func TestRootInspectorComposeFromPage(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	openInspectorViaEnter(t, m)

	_, cmd := m.Update(special('\r')) // cursor at row 0 = MTI (leaf)
	if cmd == nil {
		t.Fatal("enter yielded no cmd")
	}
	msg := cmd()
	if got, ok := msg.(pages.TxComposeMsg); !ok || got.ID != "Purchase" {
		t.Fatalf("enter dispatched %#v, want TxComposeMsg{Purchase}", msg)
	}
	_, cmd = m.Update(msg)
	if cmd != nil {
		t.Errorf("compose msg ran a cmd: %v", cmd())
	}
	wantStack(t, m, "transactions", "inspector")
}

// openInspectorViaEnter drives the §B → §C round-trip: Enter on the
// transactions page yields a TxDetailMsg cmd, which is fed back through
// root.Update exactly like the program would deliver it.
func openInspectorViaEnter(t *testing.T, m *RootModel) {
	t.Helper()

	_, cmd := m.Update(special('\r'))
	if cmd == nil {
		t.Fatal("enter on transactions yielded no cmd")
	}
	if _, cmd := m.Update(cmd()); cmd != nil {
		t.Fatalf("detail msg ran a cmd: %v", cmd())
	}
}
