// scenarios_focus_test.go pins the §F pane focus state machine (UAT
// round 9 F-9e): the router's Tab/shift-Tab PaneFocusMsg cycles the
// SCENARIOS list ↔ STEPS panes, the focused pane renders the accent
// border, nav keys route by pane, and the footer advertises the toggle.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// TestScenariosPaneFocusCycle: UAT round 9 F-9e — the router's Tab/
// shift-Tab PaneFocusMsg cycles the §F pane focus SCENARIOS ↔ STEPS and
// wraps (the §I TestSessionsPaneFocusCycle contract).
func TestScenariosPaneFocusCycle(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	if s.Pane() != ScenarioPaneList {
		t.Fatalf("initial pane = %d, want the list pane", s.Pane())
	}
	_, _ = s.Update(PaneFocusMsg{})
	if s.Pane() != ScenarioPaneSteps {
		t.Fatalf("after tab pane = %d, want the steps pane", s.Pane())
	}
	_, _ = s.Update(PaneFocusMsg{})
	if s.Pane() != ScenarioPaneList {
		t.Fatalf("after second tab pane = %d, want the list pane", s.Pane())
	}
	_, _ = s.Update(PaneFocusMsg{Reverse: true})
	if s.Pane() != ScenarioPaneSteps {
		t.Fatalf("shift-tab pane = %d, want the steps pane (wrap)", s.Pane())
	}
}

// TestScenariosStepsFocusKeepsListCursorInert: with the STEPS pane
// focused the navigation keys must NOT move the list cursor (the pane
// switch has effect; the keys now drive the step cursor, pinned by
// TestScenariosStepCursorMovesInStepsPane). Tab back
// restores list navigation, and Enter on the list pane still yields the
// run msg (semantics unchanged).
func TestScenariosStepsFocusKeepsListCursorInert(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	_, _ = s.Update(PaneFocusMsg{}) // focus STEPS

	before := s.SelectedID()
	for _, msg := range []tea.Msg{
		scenPress(tea.KeyDown), press('j'), press('G'), scenPress(tea.KeyEnd), press('k'),
	} {
		_, _ = s.Update(msg)
	}
	if s.SelectedID() != before {
		t.Fatalf("nav keys must not move the list cursor while STEPS is focused: %q -> %q",
			before, s.SelectedID())
	}

	_, _ = s.Update(PaneFocusMsg{Reverse: true}) // back to the list pane
	_, _ = s.Update(scenPress(tea.KeyDown))
	if s.SelectedID() == before {
		t.Fatal("the list cursor must move again once the list pane is focused")
	}

	_, cmd := s.Update(scenPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on the list pane must still yield a cmd")
	}
	if msg, ok := cmd().(ScenarioRunMsg); !ok || msg.ID != "Card update flow" {
		t.Fatalf("enter msg = %#v, want ScenarioRunMsg{Card update flow}", cmd())
	}
}

// TestScenariosFocusedPaneAccent: the focused pane accents its title and
// lights its border (the §I UAT-round-5 focus rendering contract, read
// through the theme's own styles so no escape bytes are hardcoded).
func TestScenariosFocusedPaneAccent(t *testing.T) {
	t.Parallel()

	tc := testTheme(t, colorprofile.TrueColor)
	s := NewScenarios(tc)
	s.SetState(scenListState())
	_, _ = s.Update(windowSize(120, 32))

	// A focused pane's title is the accent style rendered over itself;
	// an unfocused pane's title is the accent wrapping the muted style
	// (the §I paneTitle + Section double-render bytes).
	accented := func(title string) string { return tc.Accent.Render(tc.Accent.Render(title)) }
	muted := func(title string) string { return tc.Accent.Render(tc.TextMuted.Render(title)) }

	body := s.View().Content
	if !strings.Contains(body, accented(titleScenarios)) {
		t.Errorf("the focused list pane must accent its title:\n%s", body)
	}
	if !strings.Contains(body, muted(titleSteps)) {
		t.Errorf("the unfocused steps pane must mute its title:\n%s", body)
	}

	focusSeq := tc.BorderFocused().String()
	neutralSeq := tc.Border.String()
	if !strings.Contains(body, focusSeq) {
		t.Errorf("the focused pane must light its border with the accent foreground:\n%s", body)
	}
	if !strings.Contains(body, neutralSeq) {
		t.Error("the unfocused pane must keep the neutral border token")
	}

	_, _ = s.Update(PaneFocusMsg{}) // focus the steps pane
	body = s.View().Content
	if !strings.Contains(body, accented(titleSteps)) {
		t.Errorf("tab must accent the steps pane title:\n%s", body)
	}
	if !strings.Contains(body, muted(titleScenarios)) {
		t.Errorf("tab must mute the list pane title:\n%s", body)
	}
}

// TestScenariosHintsAdvertiseTab: the §F footer advertises the pane
// toggle as a primary hint (the §I/§K hint pattern; Task 9.7 Minor: the
// test must pin Primary:true, not just the key's presence — the narrow
// footer drops non-primary hints).
func TestScenariosHintsAdvertiseTab(t *testing.T) {
	t.Parallel()

	s := NewScenarios(asciiTheme(t))
	for _, h := range s.Hints() {
		if h.Key == theme.KeyTab {
			if !h.Primary {
				t.Fatalf("the tab pane hint must be primary (the narrow footer drops it): %+v", h)
			}

			return
		}
	}
	t.Fatalf("the footer must advertise the tab pane hint: %+v", s.Hints())
}
