// ctf_test.go covers the §K page contract: field-focus cycling inside
// the PARAMETERS pane (Tab/shift-Tab local while the page claims the
// keyboard), draft editing over root-committed values, the client-side
// session filter, Enter yielding the generate message with the
// committed params, the preview overlay's Esc/w ownership (Esc closes
// first, a second Esc pops; w yields the write message; a new
// PreviewID re-arms), the SUMMARY line content, the ascii fallback,
// and the narrow stacked layout. Fixtures are fixed display strings —
// no clock, no absolute paths.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// ctfFixtureState is the §K snapshot in theme-appropriate
// glyphs (goldens and units share it).
func ctfFixtureState(th *theme.Theme) CtfState {
	sep := joinSep(th)

	return CtfState{
		DBPath:     "./sessions.db",
		SelectedID: "9f3ca1e2b7d84455a1",
		Sessions: []CtfSessionRow{
			{ID: "9f3ca1e2b7d84455a1", ShortID: "9f3c..a1", When: "today 12:01", Approved: "148 approved"},
			{ID: "77b255c9e4d3", ShortID: "77b2..d3", When: "today 09:55", Approved: "22 approved"},
		},
		Params: CtfParams{CIB: "400129", Batch: "1", OutPath: "./out/CTF_001.dat"},
		SummaryLine: "148 tx" + sep + "$ 12,450.00 total" + sep +
			"header/trailer dates auto",
	}
}

// ctfPage builds an ascii §K page loaded with the fixture at size.
func ctfPage(t *testing.T, w, h int) *Ctf {
	t.Helper()
	c := NewCtf(asciiTheme(t))
	c.SetState(ctfFixtureState(c.th))
	_, _ = c.Update(windowSize(w, h))

	return c
}

func TestCtfFieldFocusCycling(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	c.Update(PaneFocusMsg{}) // router Tab: list -> params

	if c.Pane() != CtfPaneParams || !c.ClaimsKeyboard() {
		t.Fatalf("pane = %d claims = %v, want params focused and claiming", c.Pane(), c.ClaimsKeyboard())
	}
	for want := 1; want <= 4; want++ {
		c.Update(special(tea.KeyTab))
		if got := c.FieldFocus(); got != want%FormFieldCount {
			t.Fatalf("focus = %d, want %d", got, want%FormFieldCount)
		}
	}
	c.Update(modKey(tea.KeyTab, tea.ModShift))
	if got := c.FieldFocus(); got != FormFieldCount-1 {
		t.Fatalf("shift-Tab focus = %d, want %d", got, FormFieldCount-1)
	}
}

func TestCtfFormEditsOverrideCommitted(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	c.Update(PaneFocusMsg{})

	c.Update(press('7')) // types into the CIB field (focus 0)
	c.Update(special(tea.KeyBackspace))
	c.Update(modKey(tea.KeyTab, tea.ModShift)) // wrap to Output path
	c.Update(press('x'))

	params := c.CommittedParams()
	if params.CIB != "400129" || params.OutPath != "./out/CTF_001.datx" {
		t.Fatalf("params = %+v, want CIB restored and out path edited", params)
	}
	if c.FieldValue(FieldBin) != "" {
		t.Fatalf("blank BIN must stay blank (all)")
	}
}

func TestCtfFilterNarrowsSessions(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	_, _ = c.Update(press('/'))

	if !c.ClaimsKeyboard() {
		t.Fatalf("filter mode must claim the keyboard")
	}
	for _, r := range "77b2" {
		c.Update(press(r))
	}
	if len(c.view) != 1 || c.view[0].ID != "77b255c9e4d3" {
		t.Fatalf("filter left %d rows, want the 77b2 session only", len(c.view))
	}
	c.Update(special(tea.KeyEscape))
	if len(c.view) != 2 || c.filter != "" {
		t.Fatalf("esc must clear the filter, got %d rows filter %q", len(c.view), c.filter)
	}
}

func TestCtfEnterYieldsGenerateWithParams(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	_, cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmd().(CtfGenerateMsg)
	if !ok {
		t.Fatalf("Enter yielded %T, want CtfGenerateMsg", cmd())
	}
	if msg.SessionID != "9f3ca1e2b7d84455a1" || msg.Params.CIB != "400129" ||
		msg.Params.OutPath != "./out/CTF_001.dat" {
		t.Fatalf("generate = %+v, want the selected session + committed params", msg)
	}
}

func TestCtfOverlayEscOwnershipAndWrite(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	st := c.state
	st.Preview = &CtfPreview{
		Headline: []string{"9f3c..a1 | 2 records | 2 monetary tx"},
		Records: []string{
			padRight("05004242424242424242 ARN0001 GOLDEN", 168),
			padRight("9204001290245 GOLDEN TRAILER", 168),
		},
		OutPath: "./out/CTF_001.dat",
	}
	st.PreviewID = 1
	c.SetState(st)

	if !c.PreviewOpen() || !c.ClaimsKeyboard() {
		t.Fatalf("preview push must open the overlay and claim the keyboard")
	}
	body := ansi.Strip(c.View().Content)
	for _, want := range []string{"RECORDS", "ARN0001 GOLDEN", "./out/CTF_001.dat", "w write"} {
		if !strings.Contains(body, want) {
			t.Errorf("overlay body lacks %q:\n%s", want, body)
		}
	}

	_, cmd := c.Update(press('w'))
	if _, ok := cmd().(CtfWriteMsg); !ok {
		t.Fatalf("w in overlay yielded %v, want CtfWriteMsg", cmd())
	}
	c.Update(special(tea.KeyEscape))
	if c.PreviewOpen() {
		t.Fatalf("esc must close the overlay first")
	}
	_, cmd = c.Update(special(tea.KeyEscape))
	if _, ok := cmd().(CtfPopMsg); !ok {
		t.Fatalf("esc after closing the overlay must pop the page")
	}
}

func TestCtfOverlayReopensOnNewPreviewID(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	st := ctfFixtureState(c.th)
	st.Preview = &CtfPreview{Headline: []string{"v1"}, OutPath: "./a"}
	st.PreviewID = 1
	c.SetState(st)
	c.Update(special(tea.KeyEscape))

	st.PreviewID = 2
	st.Preview = &CtfPreview{Headline: []string{"v2"}, OutPath: "./b"}
	c.SetState(st)
	if !c.PreviewOpen() || !strings.Contains(c.View().Content, "v2") {
		t.Fatalf("a new PreviewID must re-arm the overlay")
	}
	st.Preview = nil
	c.SetState(st)
	if c.PreviewOpen() {
		t.Fatalf("a cleared Preview must close the overlay")
	}
}

func TestCtfSummaryLineAndAsciiFallback(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	body := c.View().Content
	for _, want := range []string{
		"VISA BASE II - CTF EXPORT", "SESSIONS (Visa tx eligible)",
		"PARAMETERS", "CIB (interchange BIN)", "400129", "SUMMARY:",
		"148 tx" + joinSep(c.th) + "$ 12,450.00 total" + joinSep(c.th) + "header/trailer dates auto", "148 approved",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"—", "·", "▏", "→"} {
		if strings.Contains(body, bad) {
			t.Errorf("ascii fallback leaked %q", bad)
		}
	}
}

func TestCtfWriteLineReplacesHint(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	st := c.state
	st.WriteLine = "wrote 8 records to ./out/CTF_001.dat"
	st.WriteOK = true
	c.SetState(st)
	if body := c.View().Content; !strings.Contains(body, "wrote 8 records to ./out/CTF_001.dat") {
		t.Errorf("write result line missing:\n%s", body)
	}
}

func TestCtfNarrowStackedLayout(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 80, 24)
	body := c.View().Content
	i, j := strings.Index(body, "SESSIONS (Visa tx eligible)"), strings.Index(body, "PARAMETERS")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("narrow body must stack PARAMETERS below SESSIONS (i=%d j=%d)", i, j)
	}
}

func TestCtfEmptyStates(t *testing.T) {
	t.Parallel()

	c := NewCtf(asciiTheme(t))
	c.SetState(CtfState{})
	_, _ = c.Update(windowSize(120, 32))
	if body := c.View().Content; !strings.Contains(body, "database not configured") {
		t.Errorf("unset db empty state missing:\n%s", body)
	}

	c.SetState(CtfState{DBPath: "./sessions.db"})
	if body := c.View().Content; !strings.Contains(body, "no CTF-eligible sessions") {
		t.Errorf("empty-eligible state missing:\n%s", body)
	}
}

func TestCtfRefreshKey(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	_, cmd := c.Update(press('r'))
	if _, ok := cmd().(CtfRefreshMsg); !ok {
		t.Fatalf("r yielded %v, want CtfRefreshMsg", cmd())
	}
}

// TestCtfCursorMoveEmitsSelect: moving the list cursor
// onto a different session yields CtfSelectMsg (root recalculates the
// SUMMARY for the row under the cursor); a move that stays on the same
// row (clamped at the end) must not re-fire the dry leg.
func TestCtfCursorMoveEmitsSelect(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	_, cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	msg, ok := cmdMsg(t, cmd).(CtfSelectMsg)
	if !ok {
		t.Fatalf("down yielded %T, want CtfSelectMsg", cmd)
	}
	if msg.SessionID != "77b255c9e4d3" || msg.Params.CIB != "400129" {
		t.Fatalf("select = %+v, want the 2nd session + committed params", msg)
	}

	_, cmd = c.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // clamped at the last row
	if cmd != nil {
		t.Fatalf("same-row move yielded %v, want nil (no re-fire)", cmd)
	}
	_, cmd = c.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if msg, ok := cmdMsg(t, cmd).(CtfSelectMsg); !ok || msg.SessionID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("up yielded %T, want select of the first session", cmd)
	}
}

// TestCtfFormEditEmitsSelect: typing into the focused
// field (the focus ring starts on CIB) re-runs the dry leg with the
// edited values.
func TestCtfFormEditEmitsSelect(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	c.Update(PaneFocusMsg{}) // list -> params
	_, cmd := c.Update(press('7'))
	msg, ok := cmdMsg(t, cmd).(CtfSelectMsg)
	if !ok {
		t.Fatalf("edit yielded %T, want CtfSelectMsg", cmd)
	}
	if msg.Params.CIB != "4001297" || msg.SessionID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("select = %+v, want the edited CIB for the cursor row", msg)
	}
}

// TestCtfPlaceholderNoFakeCaret: the unfocused BIN
// placeholder must not wear a caret — the borrowed caret made the
// unfocused row look like the editable one.
func TestCtfPlaceholderNoFakeCaret(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	body := ansi.Strip(c.View().Content)
	if !strings.Contains(body, "(blank = all)") {
		t.Fatalf("placeholder missing:\n%s", body)
	}
	if strings.Contains(body, cursorGlyph(c.th)+"(blank = all)") {
		t.Errorf("unfocused placeholder still fakes a caret:\n%s", body)
	}
}

// TestCtfRulerMarkersEveryTen pins the ruler contract: a digit marker
// every 10 record positions, a + tick at each half-decade, - between —
// and that a scrolled window re-anchors to the TRUE positions.
func TestCtfRulerMarkersEveryTen(t *testing.T) {
	t.Parallel()

	if got := ctfRuler(0, 26); got != "0----+----1----+----2----+" {
		t.Errorf("ruler at 0 = %q", got)
	}
	if got := ctfRuler(50, 21); got != "5----+----6----+----7" {
		t.Errorf("ruler at 50 = %q, want re-anchored true positions (21 cells)", got)
	}
	if got := ctfRuler(90, 61); got[60] != '5' { // 0-based pos 150: the tens digit of 150
		t.Errorf("digit at position 151 = %q, want 5", got[60])
	}
}

// TestCtfViewerWalkAndColumns pins the record viewer's window math: the
// record cursor walks and clamps, the column window shifts and clamps
// at the record end, and the rulers sit behind the 6-column gutter so
// their digits align with the record text.
func TestCtfViewerWalkAndColumns(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	st := c.state
	long := padRight("05004242424242424242 ARN0001 GOLDEN", 168)
	st.Preview = &CtfPreview{
		Headline: []string{"9f3c..a1 | 3 records"},
		Records:  []string{long, padRight("0200 ARN0002", 168), padRight("9204 TRAILER", 168)},
		OutPath:  "./out/CTF_001.dat",
	}
	st.PreviewID = 1
	c.SetState(st)

	_, _ = c.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // record 2
	if body := ansi.Strip(c.View().Content); !strings.Contains(body, "rec 2/3") {
		t.Fatalf("record cursor did not walk:\n%s", body)
	}
	for i := 0; i < 5; i++ { // walk past the end: clamps at 3
		_, _ = c.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if body := ansi.Strip(c.View().Content); !strings.Contains(body, "rec 3/3") {
		t.Fatalf("record cursor must clamp at the last record:\n%s", body)
	}

	// Column window: one shift shows the true range; further shifts
	// clamp so the window's right edge never passes the record end.
	// Rulers align behind the gutter: 6 columns of gutter/cursor cell,
	// then the digit markers — asserted at colOff 0, where the ruler
	// starts with its 0-digit marker.
	if at0 := ansi.Strip(c.View().Content); !strings.Contains(at0, "      0----+----1") {
		t.Fatalf("ruler row must sit behind the 6-column gutter:\n%s", at0)
	}

	_, _ = c.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	body := ansi.Strip(c.View().Content)
	if !strings.Contains(body, "cols 54-159 of 168") {
		t.Fatalf("column window after right = want cols 54-159:\n%s", body)
	}
	_, _ = c.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, _ = c.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if body := ansi.Strip(c.View().Content); !strings.Contains(body, "cols 63-168 of 168") {
		t.Fatalf("column window must clamp to the record end:\n%s", body)
	}
}

// TestCtfNoDuplicateNoDBLine QA: the CTF page repeats the
// sessions empty-state pattern, so the same rule applies — the full
// "database not configured - pass --db…" sentence appears once (the root
// note), never twice.
func TestCtfNoDuplicateNoDBLine(t *testing.T) {
	t.Parallel()

	c := NewCtf(asciiTheme(t))
	c.SetState(CtfState{Note: EmptyTextNoSessionDB})
	_, _ = c.Update(windowSize(120, 40))
	if n := strings.Count(c.View().Content, "pass --db to enable session logging"); n != 1 {
		t.Errorf("the no-database sentence shows %d times, want once:\n%s", n, c.View().Content)
	}
}
