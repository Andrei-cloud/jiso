// the router's Tab/shift-Tab convert to PaneFocusMsg and cycle the §F
// page's focused pane.
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// Tab turns into a PaneFocusMsg and reaches the real §F page, cycling
// focus list → steps; shift-Tab cycles back.
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
