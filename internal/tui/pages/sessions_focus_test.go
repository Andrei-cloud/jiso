// sessions_focus_test.go covers the UAT round 9 (F-9f) cursor-following
// detail leg: arrowing the SESSIONS list cursor onto a different session
// yields SessionsFocusMsg so root loads that session's stats + tx
// history into the detail panes as a live preview (the §K cursor-follow
// pattern), the load stays quiet while another mode owns the keyboard,
// and DetailWait renders the loading marker instead of the false
// "no transactions"/"select a session" empty states.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSessionsCursorMoveEmitsFocus UAT round 9 (F-9f): arrowing the list
// cursor onto a different session yields SessionsFocusMsg (root loads
// that session's stats + tx history into the detail panes as a live
// preview, the §K cursor-follow pattern); a move clamped on the same row
// must not re-fire the load (no load loop).
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

// TestSessionsNoFocusWhileModesOwnKeys UAT round 9 (F-9f): the cursor-
// following load stays quiet while the review overlay, the narrow drill,
// or filter mode own the keyboard — those modes own the keys (and the
// drill's arrows belong to the history table).
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

	// Filter mode: arrows move the filtered cursor but the load waits
	// until Enter yields the keyboard.
	f := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 120, 40)
	_, _ = f.Update(press('/'))
	if _, cmd := f.Update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatalf("down while filtering yielded %v, want nil", cmd())
	}
}

// TestSessionsDetailWaitLoadingText UAT round 9 (F-9f): while root's
// detail load is in flight (DetailWait), the STATS and TX HISTORY panes
// carry the loading marker — not the false "select a session to see its
// stats" / "no transactions recorded" empty states.
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
