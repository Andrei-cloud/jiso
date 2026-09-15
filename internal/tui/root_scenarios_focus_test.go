// root_scenarios_focus_test.go pins the router→§F pane-focus leg: the
// global Tab/shift-Tab bindings convert to PaneFocusMsg and cycle the
// real Scenarios page's focused pane (UAT round 9 F-9e).
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// TestTabCyclesScenariosPane: UAT round 9 F-9e — the router's Tab turns
// into a PaneFocusMsg and reaches the real §F page, cycling the pane
// focus list → steps; shift-Tab cycles back (the
// TestTabForwardsPaneFocusMsg contract pinned on the real page).
func TestTabCyclesScenariosPane(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('3')) // hotkey slot 3 = §F scenarios
	if got := m.Current().ID(); got != pages.ScenariosPageID {
		t.Fatalf("after '3' current page = %q, want %q", got, pages.ScenariosPageID)
	}

	_, _ = m.Update(special(tea.KeyTab))
	if got := m.scenarios.Pane(); got != pages.ScenarioPaneSteps {
		t.Fatalf("after tab pane = %d, want the steps pane", got)
	}
	_, _ = m.Update(mod(tea.KeyTab, tea.ModShift))
	if got := m.scenarios.Pane(); got != pages.ScenarioPaneList {
		t.Fatalf("after shift-tab pane = %d, want the list pane", got)
	}
}
