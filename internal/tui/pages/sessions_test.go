// sessions_test.go covers the §I page contract (SCR-509): the empty
// states name the next action (no DB / no sessions / filter miss), the
// wireframe columns and cells render, pane focus cycles across
// SESSIONS ↔ TX HISTORY (the §C PaneFocusMsg scheme), Enter dispatches
// select/review by pane, [t]/[r] yield their messages, the review
// overlay owns Esc first, the narrow fallback drills into stats +
// history, and widths truncate without wrapping. Fixtures are fixed
// display strings — no clock, no terminal paths.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// sessionsFixtureState is the wireframe §I snapshot. The ShortID column is
// derived through the theme rather than typed out, because the typed version was
// wrong twice over: "77b2..c9" and "31a0..f4" are 8-rune ids, which the root prints
// in full, and the ".." spelling is not an elision any profile emits. Deriving it
// means the golden shows the cell the root would show, in the profile being pinned.
func sessionsFixtureState(th *theme.Theme) SessionsState {
	sessions := []SessionRow{
		{ID: "9f3ca1e2b7d84455a1", When: "today 12:01"},
		{ID: "77b255c9", When: "today 09:55"},
		{ID: "31a000f4", When: "yest 17:30"},
	}
	for i := range sessions {
		sessions[i].ShortID = th.ShortID(sessions[i].ID)
	}

	return SessionsState{
		DBPath:     "./sessions.db",
		SelectedID: "9f3ca1e2b7d84455a1",
		Sessions:   sessions,
		Stats: []SummaryKV{
			{Label: "total", Value: "150"},
			{Label: "ok", Value: "148 (98.7%)"},
			{Label: "fail", Value: "2"},
			{Label: "avg", Value: "3.4 ms"},
			{Label: "RC dist", Value: "00:148 96:2"},
		},
		History: []TxHistoryRow{
			{ID: 9, Time: "12:04:11", Name: "Purchase", MTI: "0210", RC: "00", Status: TxStatusOK, Latency: "3ms"},
			{ID: 8, Time: "12:04:09", Name: "Sign On", MTI: "0810", RC: "00", Status: TxStatusOK, Latency: "1ms"},
			// A timed-out row has no latency number at all (root leaves the cell
			// empty and the page dashes it). The fixture used to write "timeout"
			// here, which made the golden print the word twice in one row and read
			// as a production defect to anyone reviewing it.
			{ID: 7, Time: "12:03:58", Name: "Purchase", Status: TxStatusTimeout},
		},
	}
}

func sessionsPageAt(t *testing.T, st SessionsState, w, h int) *Sessions {
	t.Helper()
	p := NewSessions(asciiTheme(t))
	p.SetState(st)
	_, _ = p.Update(windowSize(w, h))

	return p
}

func sessionsBody(t *testing.T, p *Sessions) []string {
	t.Helper()

	return strings.Split(p.View().Content, "\n")
}

func sessionsBodyFits(t *testing.T, p *Sessions, width int) []string {
	t.Helper()
	lines := sessionsBody(t, p)
	for i, line := range lines {
		if n := lipgloss.Width(line); n > width {
			t.Fatalf("width %d line %d is %d cells (wrap/overflow): %q", width, i, n, line)
		}
	}

	return lines
}

func TestSessionsEmptyStateNoDB(t *testing.T) {
	t.Parallel()

	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, SessionsState{}, 120, 32)), "\n")
	for _, want := range []string{"SESSIONS", "database not configured", "pass --db"} {
		if !strings.Contains(joined, want) {
			t.Errorf("empty body lacks %q:\n%s", want, joined)
		}
	}
}

func TestSessionsEmptyStateNoSessions(t *testing.T) {
	t.Parallel()

	st := SessionsState{DBPath: "./sessions.db"}
	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, st, 120, 32)), "\n")
	if !strings.Contains(joined, "no sessions in ./sessions.db. Pass --db to enable logging.") {
		t.Errorf("wireframe empty state missing:\n%s", joined)
	}
}

func TestSessionsNoteLineRendersTypedError(t *testing.T) {
	t.Parallel()

	st := SessionsState{DBPath: "./gone.db", Note: "no sessions in ./gone.db. Pass --db to enable logging."}
	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, st, 120, 32)), "\n")
	if !strings.Contains(joined, "no sessions in ./gone.db") {
		t.Errorf("root-stamped note missing:\n%s", joined)
	}
}

func TestSessionsPanesColumnsAndValues(t *testing.T) {
	t.Parallel()

	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)), "\n")
	// TRANSACTION is the flex column: at 120 its header carries the clip
	// tail, so match the visible prefix.
	for _, want := range []string{"SESSIONS", "STATS", "TX HISTORY", "SESSION", "WHEN", "TIME", "TRANSAC", "MTI", "RC", "STATUS", "LATENCY"} {
		if !strings.Contains(joined, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	for _, want := range []string{"9f3c~a1", "today 12:01", "yest 17:30", "150", "148 (98.7%)", "3.4 ms", "00:148 96:2", "12:04:11", "Purchase", "0210"} {
		if !strings.Contains(joined, want) {
			t.Errorf("body lacks cell %q:\n%s", want, joined)
		}
	}
}

func TestSessionsStatusSymbolPlusWord(t *testing.T) {
	t.Parallel()

	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)), "\n")
	for _, want := range []string{"[ok] ok", "[x] timeout"} {
		if !strings.Contains(joined, want) {
			t.Errorf("status cell %q missing (symbol+word contract):\n%s", want, joined)
		}
	}
}

func TestSessionsFilterNarrowsList(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, _ = p.Update(press('/'))
	if !p.ClaimsKeyboard() {
		t.Fatal("filter mode must claim the keyboard")
	}
	for _, r := range "77b2" {
		_, _ = p.Update(press(r))
	}
	if n := len(p.view); n != 1 || p.view[0].ID != "77b255c9" {
		t.Fatalf("filtered view = %+v, want only 77b255c9", p.view)
	}
	// Enter leaves filter mode with the filter applied.
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.ClaimsKeyboard() {
		t.Fatal("enter must yield the keyboard")
	}
	if f, _ := p.Filter(); f != "77b2" {
		t.Fatalf("filter = %q, want 77b2", f)
	}
	// Esc clears.
	_, _ = p.Update(press('/'))
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f, _ := p.Filter(); f != "" || p.ClaimsKeyboard() {
		t.Fatalf("esc must clear the filter, got %q filtering=%v", f, p.ClaimsKeyboard())
	}
}

func TestSessionsFilterNoMatchText(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, _ = p.Update(press('/'))
	_, _ = p.Update(press('z'))
	joined := strings.Join(sessionsBody(t, p), "\n")
	if !strings.Contains(joined, "no sessions match filter") {
		t.Errorf("filter miss state missing:\n%s", joined)
	}
}

func TestSessionsPaneFocusCycle(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	if p.Pane() != paneSessions {
		t.Fatalf("initial pane = %d, want sessions", p.Pane())
	}
	_, _ = p.Update(PaneFocusMsg{})
	if p.Pane() != paneHistory {
		t.Fatalf("after tab pane = %d, want history", p.Pane())
	}
	_, _ = p.Update(PaneFocusMsg{})
	if p.Pane() != paneSessions {
		t.Fatalf("after second tab pane = %d, want sessions", p.Pane())
	}
	_, _ = p.Update(PaneFocusMsg{Reverse: true})
	if p.Pane() != paneHistory {
		t.Fatalf("shift-tab pane = %d, want history", p.Pane())
	}
}

func TestSessionsHistoryCursorMoves(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, _ = p.Update(PaneFocusMsg{})
	if got := p.SelectedTxID(); got != 9 {
		t.Fatalf("cursor tx = %d, want 9", got)
	}
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := p.SelectedTxID(); got != 8 {
		t.Fatalf("after down tx = %d, want 8", got)
	}
}

func TestSessionsEnterSelectsSession(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd()
	sel, ok := msg.(SessionsSelectMsg)
	if !ok || sel.ID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("enter msg = %#v, want SessionsSelectMsg for the cursor row", msg)
	}
	// Down then enter selects the second row.
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2, ok := cmd().(SessionsSelectMsg)
	if !ok {
		t.Fatalf("enter after down = %#v (%T), want SessionsSelectMsg", cmd(), cmd())
	}
	if m2.ID != "77b255c9" {
		t.Fatalf("enter after down = %q, want 77b255c9", m2.ID)
	}
}

func TestSessionsReviewKeysDispatch(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	// [t] reviews the selected tx from either pane.
	_, cmd := p.Update(press('t'))
	if m, ok := cmd().(SessionsReviewMsg); !ok || m.TxID != 9 {
		t.Fatalf("t msg = %#v, want SessionsReviewMsg{9}", cmd())
	}
	// Enter in the history pane reviews the cursor tx.
	_, _ = p.Update(PaneFocusMsg{})
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m, ok := cmd().(SessionsReviewMsg); !ok || m.TxID != 8 {
		t.Fatalf("enter-in-history msg = %#v, want SessionsReviewMsg{8}", cmd())
	}
}

func TestSessionsRefreshKeyDispatches(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, cmd := p.Update(press('r'))
	if _, ok := cmd().(SessionsRefreshMsg); !ok {
		t.Fatalf("r msg = %#v, want SessionsRefreshMsg", cmd())
	}
}

func TestSessionsEscPops(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(SessionsPopMsg); !ok {
		t.Fatalf("esc msg = %#v, want SessionsPopMsg", cmd())
	}
}

func sessionsReviewFixture() *TxReviewState {
	return &TxReviewState{
		TxID:     7,
		Headline: []string{"Purchase | id 7 | RC 96 | 2ms | [x]", "session 9f3ca1e2b7d84455a1"},
		Request: &TxReviewMessage{
			HEX:      "30 32 30 30 82 34 50 00 00 00\n38 30 38 30 30 30 30 30 30 30 30 30",
			Describe: "MTI ....\n  2 (PAN) 4242424242424242\n  39 (RC) 96",
		},
		Response: &TxReviewMessage{
			HEX:         "30 32 31 30",
			Describe:    "MTI ....\n  39 (RC) 96",
			RawFallback: true,
		},
	}
}

func TestSessionsReviewOverlayOpenAndEsc(t *testing.T) {
	t.Parallel()

	st := sessionsFixtureState(asciiTheme(t))
	st.Review = sessionsReviewFixture()
	p := sessionsPageAt(t, st, 120, 40)

	if !p.ReviewOpen() {
		t.Fatal("pushing a new Review must open the overlay")
	}
	joined := strings.Join(sessionsBody(t, p), "\n")
	for _, want := range []string{"REQUEST HEX", "REQUEST FIELDS", "RESPONSE HEX", "raw hex fallback", "4242424242424242", "esc close", "Purchase | id 7"} {
		if !strings.Contains(joined, want) {
			t.Errorf("overlay lacks %q:\n%s", want, joined)
		}
	}
	// The overlay owns the keyboard: t dispatches nothing.
	if _, cmd := p.Update(press('t')); cmd != nil {
		t.Fatal("t while overlay open must be swallowed")
	}
	// Esc closes the overlay and does NOT pop the page.
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatalf("first Esc must only close the overlay, got %v", cmd())
	}
	if p.ReviewOpen() {
		t.Fatal("overlay still open after Esc")
	}
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(SessionsPopMsg); !ok {
		t.Fatalf("second Esc = %#v, want SessionsPopMsg", cmd())
	}
}

// TestSessionsReviewScroll: UAT round 5 — a review taller than the
// window used to be silently unreachable (the render flattened
// everything to h lines with no scroll state). j/k scroll one line,
// pgup/pgdn page, the offset clamps at both ends, and the geometry
// never changes while scrolling.
func TestSessionsReviewScroll(t *testing.T) {
	t.Parallel()

	st := sessionsFixtureState(asciiTheme(t))
	st.Review = sessionsReviewFixture()
	// Short window: the fixture's ~19-line review cannot fit.
	p := sessionsPageAt(t, st, 120, 14)

	if !p.ReviewOpen() {
		t.Fatal("pushing a new Review must open the overlay")
	}
	top := sessionsBody(t, p)
	joinedTop := strings.Join(top, "\n")
	if !strings.Contains(joinedTop, "REQUEST HEX") {
		t.Fatalf("top of window must show REQUEST HEX:\n%s", joinedTop)
	}
	if strings.Contains(joinedTop, "esc close") {
		t.Fatal("the bottom hint must be clipped at scroll 0 (precondition)")
	}

	// k scrolls up (no-op at the top); j scrolls down one line.
	if _, cmd := p.Update(press('k')); cmd != nil {
		t.Fatalf("k in overlay must be swallowed, got %v", cmd())
	}
	_, _ = p.Update(press('j'))
	mid := strings.Join(sessionsBody(t, p), "\n")
	if !strings.Contains(mid, "REQUEST FIELDS") {
		t.Fatalf("after j the window must have moved:\n%s", mid)
	}

	// pgdn pages to the bottom: the esc hint becomes reachable.
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	bottom := sessionsBody(t, p)
	joinedBottom := strings.Join(bottom, "\n")
	if !strings.Contains(joinedBottom, "esc close") {
		t.Fatalf("pgdn must reach the bottom hint:\n%s", joinedBottom)
	}

	// Clamped: further paging does not move past the bottom…
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	clamped := strings.Join(sessionsBody(t, p), "\n")
	if clamped != joinedBottom {
		t.Fatal("scroll must clamp at the bottom")
	}
	// …and pgup clamps at the top.
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	backTop := strings.Join(sessionsBody(t, p), "\n")
	if !strings.Contains(backTop, "REQUEST HEX") || strings.Contains(backTop, "esc close") {
		t.Fatalf("pgup must clamp at the top:\n%s", backTop)
	}

	// Geometry is stable while scrolling (same line count everywhere).
	if len(bottom) != len(top) {
		t.Errorf("line count changed while scrolling: %d vs %d", len(bottom), len(top))
	}

	// A new review identity resets the scroll.
	_, _ = p.Update(press('j'))
	_, _ = p.Update(press('j'))
	st2 := sessionsFixtureState(asciiTheme(t))
	st2.Review = sessionsReviewFixture()
	st2.Review.TxID = 11
	p.SetState(st2)
	fresh := strings.Join(sessionsBody(t, p), "\n")
	if strings.Contains(fresh, "esc close") {
		t.Fatal("a new review must start at the top")
	}
}

func TestSessionsReviewReopensOnNewIdentity(t *testing.T) {
	t.Parallel()

	st := sessionsFixtureState(asciiTheme(t))
	st.Review = sessionsReviewFixture()
	p := sessionsPageAt(t, st, 120, 40)
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	p.SetState(st)
	if p.ReviewOpen() {
		t.Fatal("same review id must not re-open the overlay")
	}
	st2 := st
	st2.Review = sessionsReviewFixture()
	st2.Review.TxID = 11
	p.SetState(st2)
	if !p.ReviewOpen() {
		t.Fatal("a new review id must re-open the overlay")
	}
}

func TestSessionsNarrowDrill(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 80, 24)
	joined := strings.Join(sessionsBody(t, p), "\n")
	if strings.Contains(joined, "TX HISTORY") {
		t.Fatalf("narrow list mode must hide the history pane:\n%s", joined)
	}
	// Enter drills into stats+history and still asks root to select.
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	sel, ok := cmd().(SessionsSelectMsg)
	if !ok || sel.ID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("narrow enter = %#v, want SessionsSelectMsg", cmd())
	}
	if !p.Drill() {
		t.Fatal("narrow enter must drill into stats+history")
	}
	joined = strings.Join(sessionsBody(t, p), "\n")
	if !strings.Contains(joined, "STATS") || !strings.Contains(joined, "TX HISTORY") {
		t.Errorf("drill view lacks the stacked panes:\n%s", joined)
	}
	// [t] reviews the selected tx from the drill.
	_, cmd = p.Update(press('t'))
	if _, ok := cmd().(SessionsReviewMsg); !ok {
		t.Fatalf("drill t = %#v, want SessionsReviewMsg", cmd())
	}
	// Esc leaves the drill first (no pop).
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatalf("drill Esc = %v, want overlay-less close (nil cmd)", cmd())
	}
	if p.Drill() {
		t.Fatal("esc must leave the drill")
	}
}

func TestSessionsWidthsTruncateNeverWrap(t *testing.T) {
	t.Parallel()

	st := sessionsFixtureState(asciiTheme(t))
	for _, width := range []int{120, 100, 90, 70, 48} {
		p := sessionsPageAt(t, st, width, 32)
		lines := sessionsBodyFits(t, p, width)
		_, h := frame.ContentSize(width, 32)
		if len(lines) != h {
			t.Fatalf("width %d: body lines = %d, want the %d-line content area", width, len(lines), h)
		}
		if !strings.Contains(strings.Join(lines, "\n"), "SESSIONS") {
			t.Fatalf("width %d lost the page title", width)
		}
	}
}

func TestSessionsSetStateKeepsHistoryCursor(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 124, 40)
	_, _ = p.Update(PaneFocusMsg{})
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // -> tx 8
	p.SetState(sessionsFixtureState(asciiTheme(t)))
	if got := p.SelectedTxID(); got != 8 {
		t.Fatalf("history identity lost on re-push: got %d, want 8", got)
	}
}

func TestSessionsHints(t *testing.T) {
	t.Parallel()

	p := sessionsPageAt(t, SessionsState{}, 120, 32)
	prim := map[string]bool{}
	for _, h := range p.Hints() {
		if h.Primary {
			prim[h.Key] = true
		}
	}
	for _, k := range []string{"enter", "t", "tab"} {
		if !prim[k] {
			t.Fatalf("hint %s must be primary", k)
		}
	}
}

// TestSessionsNoDuplicateNoDBLine UAT round 6 QA: when root stamps the
// "database not configured…" note AND the page has no database, the full
// next-action sentence must show ONCE (the note), not twice (note + the
// page's own empty-hint line repeating it verbatim).
func TestSessionsNoDuplicateNoDBLine(t *testing.T) {
	t.Parallel()

	st := SessionsState{Note: EmptyTextNoSessionDB}
	joined := strings.Join(sessionsBody(t, sessionsPageAt(t, st, 120, 40)), "\n")
	if n := strings.Count(joined, "pass --db to enable session logging"); n != 1 {
		t.Errorf("the no-database sentence shows %d times, want once:\n%s", n, joined)
	}
}
