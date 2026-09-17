// sessions_focus_test.go covers the cursor-following detail leg: moving the
// list cursor emits SessionsFocusMsg, stays quiet while another mode owns
// the keys, and DetailWait shows a loading marker, not false empty states.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Arrowing onto a different session emits SessionsFocusMsg; a clamped
// same-row move must not re-fire the load.
func TestSessionsCursorMoveEmitsFocus(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	msg, ok := cmdMsg(t, cmd).(SessionsFocusMsg)
	if !ok {
		t.Fatalf("down yielded %T, want SessionsFocusMsg", cmd)
	}
	if msg.ID != "77b255c9" {
		t.Fatalf("focus id = %q, want 77b255c9 (the newly-focused row)", msg.ID)
	}

	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if msg, ok := cmdMsg(t, cmd).(SessionsFocusMsg); !ok || msg.ID != "31a000f4" {
		t.Fatalf("second down yielded %#v, want focus of the third session", cmd)
	}

	// Clamped at the last row: the cursor did not move, so nothing fires.
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("same-row move yielded %v, want nil (no load loop)", cmd())
	}

	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if msg, ok := cmdMsg(t, cmd).(SessionsFocusMsg); !ok || msg.ID != "77b255c9" {
		t.Fatalf("up yielded %#v, want focus of the second session", cmd)
	}
}

// No focus load while the review overlay, the drill, or filter mode owns
// the keyboard.
func TestSessionsNoFocusWhileModesOwnKeys(t *testing.T) {
	t.Parallel()

	// The review overlay owns the keyboard first.
	st := sessionsFixtureState(asciiTheme(t))
	st.Review = sessionsReviewFixture()
	p := sessionsPageAt(t, st, 120, 40)
	if _, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatalf("down in the review overlay yielded %v, want nil", cmd())
	}

	// The narrow drill: arrows drive the history table there.
	d := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 80, 24)
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // drills
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatalf("down in the drill yielded %v, want nil", cmd())
	}

	// Filter mode: arrows move the filtered cursor; the load waits for Enter.
	f := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 120, 40)
	_, _ = f.Update(press('/'))
	if _, cmd := f.Update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatalf("down while filtering yielded %v, want nil", cmd())
	}
}

// While DetailWait, the panes show the loading marker, not the false
// empty states.
func TestSessionsDetailWaitLoadingText(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	st := sessionsFixtureState(th)
	st.Stats, st.History = nil, nil
	st.DetailWait = true
	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, st, 124, 40)), "\n")
	if !strings.Contains(joined, "~ loading") {
		t.Errorf("loading marker missing while DetailWait:\n%s", joined)
	}
	for _, lie := range []string{"no transactions recorded", "select a session to see its stats"} {
		if strings.Contains(joined, lie) {
			t.Errorf("DetailWait must not show %q:\n%s", lie, joined)
		}
	}
}
