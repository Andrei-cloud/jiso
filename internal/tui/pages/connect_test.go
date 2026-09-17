// connect_test.go covers the §E dialog's page-side contract: focus cycles
// skip disabled fields, input routes only to the focused enabled field
// (printable capture in text fields, radio adjustment with wrap in radios),
// SetState preserves/clamps focus across root pushes, and the rendered box
// fits 120/90/70 without escapes in ascii mode. Root-side rules/attempt
// tests live in internal/tui/root_connect_test.go.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// connectTestState builds the §E form with root-style data: mode radio
// (Caller), caller target enabled, bind disabled (caller mode), header
// radio at binary2 (not visa → station disabled), unsolicited, TLS.
func connectTestState() ConnectFormState {
	field := func(key, label string, kind FieldKind, value string, enabled bool, options ...string) FormField {
		f := FormField{
			Key: key, Label: label, Value: value, Kind: kind, Enabled: enabled,
			Options: options,
		}
		if kind == FieldRadio {
			f.Selected = optionIndex(options, value, 0)
		}
		switch key {
		case ConnectFieldIP:
			f.Note, f.NoteKind = "(caller only)", NoteInfo
		case ConnectFieldStation:
			f.Note, f.NoteKind = "(visa header only)", NoteInfo
		}

		return f
	}

	return ConnectFormState{Fields: []FormField{
		field(ConnectFieldMode, "Mode", FieldRadio, "Caller", true, ConnectModeOptions...),
		field(ConnectFieldIP, "IP", FieldText, "10.0.0.5", true),
		field(ConnectFieldPort, "Port", FieldText, "8080", true),
		field(ConnectFieldHeader, "Header", FieldPicker, "binary2", true, ConnectHeaderOptions...),
		field(ConnectFieldStation, "Station ID", FieldText, "", false),
		field(ConnectFieldUnsolicited, "Unsolicited", FieldRadio, "No", true, ConnectYesNoOptions...),
		field(ConnectFieldTLS, "TLS", FieldText, "tls_config.json", true),
	}}
}

func connectDialog(t *testing.T, st ConnectFormState) *ConnectDialog {
	t.Helper()

	d := NewConnectDialog(asciiTheme(t))
	d.SetState(st)
	_, _ = d.Update(windowSize(120, 32))

	return d
}

func pressKey(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text}
}

// special builds a named key press (tab, esc, enter, arrows, backspace).
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// modKey builds a modified key press (ctrl/shift) with no printable text.
func modKey(code rune, m tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: m}
}

func TestConnectFocusCycleSkipsDisabled(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldMode {
		t.Fatalf("initial focus %q, want mode", got)
	}

	// mode → ip → port → header → (station disabled, skipped).
	for _, want := range []string{ConnectFieldIP, ConnectFieldPort, ConnectFieldHeader} {
		_, _ = d.Update(special(tea.KeyTab))
		if got := d.State().Fields[d.Focus()].Key; got != want {
			t.Fatalf("tab focus %q, want %q", got, want)
		}
	}
	_, _ = d.Update(special(tea.KeyTab))
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldUnsolicited {
		t.Fatalf("tab focus %q, want unsolicited (station must be skipped)", got)
	}
}

func TestConnectFocusWrapBothWays(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	_, _ = d.Update(modKey(tea.KeyTab, tea.ModShift)) // from mode, wrap back
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldTLS {
		t.Fatalf("shift+tab wrap focus %q, want tls", got)
	}
	for range 7 {
		_, _ = d.Update(special(tea.KeyTab))
	}
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldMode {
		t.Fatalf("forward wrap focus %q, want mode", got)
	}
}

func TestConnectTextTypesBackspacesIgnoresNonPrintable(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	_, _ = d.Update(special(tea.KeyTab)) // focus ip (text)

	for _, r := range "9" {
		_, _ = d.Update(pressKey(r, string(r)))
	}
	if v := d.State().Fields[1].Value; v != "10.0.0.59" {
		t.Fatalf("after type %q", v)
	}
	_, _ = d.Update(special(tea.KeyBackspace))
	if v := d.State().Fields[1].Value; v != "10.0.0.5" {
		t.Fatalf("after backspace %q", v)
	}
	// Non-printable chords (ctrl+e: empty Text) must not edit the value.
	_, _ = d.Update(modKey('e', tea.ModCtrl))
	if v := d.State().Fields[1].Value; v != "10.0.0.5" {
		t.Fatalf("after ctrl+e %q", v)
	}
	_, _ = d.Update(special(tea.KeyBackspace))
	_, _ = d.Update(special(tea.KeyBackspace))
	if v := d.State().Fields[1].Value; v != "10.0.0" {
		t.Fatalf("after two backspaces %q", v)
	}
}

func TestConnectJKTypeInTextButAdjustRadio(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	_, _ = d.Update(special(tea.KeyTab)) // ip (text): j must TYPE (SCR-502 lesson)
	_, _ = d.Update(pressKey('j', "j"))
	if v := d.State().Fields[1].Value; !strings.HasSuffix(v, "j") {
		t.Fatalf("j in text field %q: must type, not navigate", v)
	}

	d2 := connectDialog(t, connectTestState()) // focus mode (radio): j adjusts
	before := d2.State().Fields[0].Selected
	_, _ = d2.Update(pressKey('k', "k"))
	after := d2.State().Fields[0]
	if after.Selected != (before-1+len(after.Options))%len(after.Options) {
		t.Fatalf("k on mode radio selected %d, want wrap-back %d", after.Selected, before)
	}
	if after.Value != after.Options[after.Selected] {
		t.Fatalf("radio Value %q not mirrored to option %q", after.Value, after.Options[after.Selected])
	}
}

func TestConnectRadioAdjustWraps(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	st := d.State()
	st.Fields[5].Selected = 0 // unsolicited = Yes
	d.SetState(st)
	for range 4 {
		_, _ = d.Update(special(tea.KeyTab))
	} // mode→ip→port→header→unsolicited (station skipped)
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldUnsolicited {
		t.Fatalf("focus %q want unsolicited", got)
	}
	_, _ = d.Update(special(tea.KeyDown))
	if got := d.State().Fields[5].Selected; got != 1 {
		t.Fatalf("down selected %d want 1", got)
	}
	_, _ = d.Update(special(tea.KeyDown)) // wrap past end
	if got := d.State().Fields[5].Selected; got != 0 {
		t.Fatalf("down wrap selected %d want 0", got)
	}
	_, _ = d.Update(special(tea.KeyUp)) // wrap before start
	if got := d.State().Fields[5].Selected; got != 1 {
		t.Fatalf("up wrap selected %d want 1", got)
	}
}

func TestConnectArrowsInTextFieldDoNothing(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	_, _ = d.Update(special(tea.KeyTab)) // ip text
	_, _ = d.Update(special(tea.KeyUp))
	if got := d.Focus(); got != 1 {
		t.Fatalf("up in text moved focus to %d, must stay on target", got)
	}
	if v := d.State().Fields[1].Value; v != "10.0.0.5" {
		t.Fatalf("up in text edited value %q", v)
	}
}

func TestConnectInFlightFreezesKeys(t *testing.T) {
	t.Parallel()

	st := connectTestState()
	st.InFlight = true
	st.Progress = "attempt 2/3"
	st.Backoff = "backoff 1.5s"
	d := connectDialog(t, st)

	_, _ = d.Update(pressKey('x', "x"))
	_, _ = d.Update(special(tea.KeyTab))
	got := d.State()
	if got.Fields[1].Value != "10.0.0.5" {
		t.Fatalf("in-flight key edited value: %q", got.Fields[1].Value)
	}
	if got.Progress != "attempt 2/3" {
		t.Fatalf("progress changed: %q", got.Progress)
	}
	if !strings.Contains(d.View(), "attempt 2/3") || !strings.Contains(d.View(), "backoff 1.5s") {
		t.Fatalf("in-flight view lacks progress/backoff:\n%s", d.View())
	}
}

func TestConnectSetStatePreservesAndClampsFocus(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	_, _ = d.Update(special(tea.KeyTab)) // ip
	st := d.State()
	st.Fields[1].Enabled = false // root just disabled the IP (mode flip)
	st.Fields[0].Value = ConnectModeListener
	d.SetState(st)
	if d.Focus() == 1 {
		t.Fatalf("focus must leave disabled field %q", st.Fields[1].Key)
	}
	// Values (page-owned edits) survive a root push.
	st2 := d.State()
	if st2.Fields[2].Value != "8080" {
		t.Fatalf("port value lost across SetState: %q", st2.Fields[2].Value)
	}
}

func TestConnectPickerOpensFiltersAndPicks(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	for range 3 {
		_, _ = d.Update(special(tea.KeyTab)) // focus header (picker)
	}
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldHeader {
		t.Fatalf("focus %q, want header", got)
	}

	_, _ = d.Update(special(tea.KeySpace)) // space opens the overlay
	if !d.PickerOpen() {
		t.Fatal("space must open the header picker")
	}
	view := d.View()
	for _, want := range []string{"HEADER", "2-digit binary", "visa station id", "enter pick"} {
		if !strings.Contains(view, want) {
			t.Fatalf("open picker lacks %q:\n%s", want, view)
		}
	}

	for _, r := range "vi" { // filter narrows to visa
		_, _ = d.Update(pressKey(r, string(r)))
	}
	_, _ = d.Update(special(tea.KeyEnter))
	if d.PickerOpen() {
		t.Fatal("enter must close the picker")
	}
	if v := d.State().Fields[3].Value; v != "visa" {
		t.Fatalf("picked %q, want visa", v)
	}

	// Esc closes keeping the old value.
	d.OpenPicker()
	for _, r := range "asc" {
		_, _ = d.Update(pressKey(r, string(r)))
	}
	_, _ = d.Update(special(tea.KeyEsc))
	if d.PickerOpen() {
		t.Fatal("esc must close the picker")
	}
	if v := d.State().Fields[3].Value; v != "visa" {
		t.Fatalf("esc changed the value to %q, must keep visa", v)
	}
}

func TestConnectViewFitsWidths(t *testing.T) {
	t.Parallel()

	for _, w := range []int{120, 90, 70} {
		d := NewConnectDialog(asciiTheme(t))
		d.SetState(connectTestState())
		_, _ = d.Update(windowSize(w, 24))
		for i, line := range strings.Split(strings.TrimRight(d.View(), "\n"), "\n") {
			if lw := cellWidth(line); lw > w-2 {
				t.Fatalf("w=%d line %d width %d exceeds %d: %q", w, i, lw, w-2, line)
			}
		}
	}
}

func TestConnectAsciiViewPlainAndMarks(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	view := d.View()
	for _, r := range view {
		if r > 127 {
			t.Fatalf("ascii view contains non-ASCII rune %q", r)
		}
	}
	if !strings.Contains(view, "(*) Caller") || !strings.Contains(view, "( ) Listener") {
		t.Fatalf("ascii radio glyphs missing:\n%s", view)
	}
	if !strings.Contains(view, "[Enter] connect") || !strings.Contains(view, "[Esc] cancel") {
		t.Fatalf("footer keys missing:\n%s", view)
	}
	// Notes ride along (caller only / visa header only).
	if !strings.Contains(view, "(caller only)") || !strings.Contains(view, "(visa header only)") {
		t.Fatalf("field notes missing:\n%s", view)
	}
}

func TestConnectTruecolorKeepsRichGlyphs(t *testing.T) {
	t.Parallel()

	d := NewConnectDialog(testTheme(t, colorprofile.TrueColor))
	d.SetState(connectTestState())
	_, _ = d.Update(windowSize(120, 24))
	if !strings.Contains(d.View(), "● Caller") || !strings.Contains(d.View(), "○ Listener") {
		t.Fatalf("truecolor radio glyphs missing:\n%s", d.View())
	}
}

func TestConnectErrorLineRenders(t *testing.T) {
	t.Parallel()

	st := connectTestState()
	st.Error = "dial tcp 10.0.0.5:8080: connect: connection refused"
	d := connectDialog(t, st)
	view := d.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("error line missing:\n%s", view)
	}
	if !strings.Contains(view, theme.ASCIIError) {
		t.Fatalf("error line lacks error symbol: %q", view)
	}
	// The form stays rendered below the error (editable again).
	if !strings.Contains(view, "(*) Caller") {
		t.Fatalf("form not editable after failure:\n%s", view)
	}
}

func TestConnectHeaderIsVisaIdentification(t *testing.T) {
	t.Parallel()

	for _, o := range ConnectHeaderOptions {
		if got := ConnectHeaderIsVisa(o); got != strings.EqualFold(o, "visa") {
			t.Fatalf("ConnectHeaderIsVisa(%q)=%v", o, got)
		}
	}
	if !ConnectHeaderIsVisa("VISA") || !ConnectHeaderIsVisa(" visa ") {
		t.Fatal("visa identification must be case-insensitive (utils.SelectLength idiom)")
	}
}

// TestConnectFooterHotKeys: UAT round 4 — the dialog footer's bracketed
// key glyphs render in the HotKey style (bold + accent, matching the
// frame footer's accented keys), the surrounding copy keeps the dialog's
// dim base, and stripping preserves the wireframe text exactly.
func TestConnectFooterHotKeys(t *testing.T) {
	t.Parallel()

	th := testTheme(t, colorprofile.TrueColor)
	d := NewConnectDialog(th)
	d.SetState(connectTestState())
	_, _ = d.Update(windowSize(120, 32))

	view := d.View()
	if hot := th.HotKey.Render("Enter"); !strings.Contains(view, hot) {
		t.Errorf("footer lacks the HotKey-styled Enter (%q):\n%q", hot, view)
	}
	if hot := th.HotKey.Render("Esc"); !strings.Contains(view, hot) {
		t.Errorf("footer lacks the HotKey-styled Esc (%q):\n%q", hot, view)
	}
	if !strings.Contains(view, "\x1b[1;") {
		t.Error("footer lacks the bold SGR around a key glyph")
	}
	// The base (dim, non-faint Deemphasized) must still cover the
	// action words between the glyphs.
	if base := th.Deemphasized.Render("] connect"); !strings.Contains(view, base) {
		t.Error("footer action words lost the dim base between hotkey glyphs")
	}
	if stripped := ansi.Strip(view); !strings.Contains(stripped, "[Enter] connect   [Esc] cancel") {
		t.Errorf("stripped footer lost the wireframe text:\n%s", stripped)
	}
}

// TestConnectFormSecondColumn: the form is a two-column table -- every row's note,
// and a picker's browse marker, starts at the same cell.
//
// They used to be appended straight after each value, so they began wherever the
// value happened to end: "(visa header only)" at 14 behind an empty value,
// "(caller only)" at 22 behind "10.0.0.5", "[ok] loaded" at 29. Three rows, three
// columns, and the notes are the cells that explain what a field means.
func TestConnectFormSecondColumn(t *testing.T) {
	t.Parallel()

	st := connectTestState()
	st.Fields[6].Note, st.Fields[6].NoteKind = "loaded", NotePass // the TLS row's status note

	lines := strings.Split(strings.TrimRight(connectDialog(t, st).View(), "\n"), "\n")

	col := -1
	for _, mark := range []string{"(caller only)", "(visa header only)", "[ok] loaded"} {
		at := -1
		for _, l := range lines {
			if j := strings.Index(l, mark); j >= 0 {
				at = j

				break
			}
		}

		if at < 0 {
			t.Fatalf("the form no longer shows %q:\n%s", mark, strings.Join(lines, "\n"))
		}

		if col >= 0 && at != col {
			t.Errorf("%q starts at cell %d, the other second-column marks at %d; a form has one column for these\n%s",
				mark, at, col, strings.Join(lines, "\n"))
		}

		col = at
	}

	// The picker row's browse marker belongs to the same column. It is looked for
	// in its own row, because the row cursor is rendered as ">" in this glyph set
	// and would match anywhere in the body.
	header := linesWith(lines, "Header")
	if at := strings.LastIndex(header, ">"); at != col {
		t.Errorf("the picker's browse marker sits at cell %d, the notes at %d; both are the row's second column\n%s",
			at, col, strings.Join(lines, "\n"))
	}
}

// linesWith returns the first rendered line containing want.
func linesWith(lines []string, want string) string {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return l
		}
	}

	return ""
}

// optionIndex finds the option equal to want (case-insensitive), defaulting
// to def when no option matches. It lives here because only test fixtures
// build radio state with it.
func optionIndex(options []string, want string, def int) int {
	want = strings.TrimSpace(want)
	for i, o := range options {
		if strings.EqualFold(o, want) {
			return i
		}
	}
	if def >= 0 && def < len(options) {
		return def
	}

	return 0
}

// Navigate/edit state machine: typing into a text field enters edit mode
// (and types itself); esc leaves edit mode keeping focus; SetFocus lands in
// navigate mode; radios never enter edit mode.
func TestConnectDialogTwoModeEditing(t *testing.T) {
	t.Parallel()

	d := connectDialog(t, connectTestState())
	if d.Editing() {
		t.Fatal("the dialog must open in navigate mode")
	}

	d.SetFocus(1) // ip (text)
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldIP {
		t.Fatalf("SetFocus focus = %q, want ip", got)
	}
	_, _ = d.Update(pressKey('9', "9"))
	if v := d.State().Fields[1].Value; v != "10.0.0.59" {
		t.Fatalf("typing %q, want \"10.0.0.59\"", v)
	}
	if !d.Editing() {
		t.Fatal("typing into a text field must enter edit mode")
	}
	_, _ = d.Update(pressKey('f', "f")) // edit mode: f types literally
	if v := d.State().Fields[1].Value; !strings.HasSuffix(v, "9f") {
		t.Fatalf("f while editing %q, want the suffix \"9f\"", v)
	}

	_, _ = d.Update(special(tea.KeyEsc)) // first esc: leave the field
	if d.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	if d.Focus() != 1 {
		t.Fatalf("esc moved focus to %d, must stay on the field", d.Focus())
	}
	if v := d.State().Fields[1].Value; !strings.HasSuffix(v, "9f") {
		t.Fatalf("esc edited the value %q, must keep it", v)
	}
	_, _ = d.Update(special(tea.KeyEsc)) // second esc is the router's (close)
	if d.Focus() != 1 || d.Editing() {
		t.Fatal("the page side must leave the second esc to the router")
	}

	// Focus movement returns to navigate mode.
	_, _ = d.Update(pressKey('5', "5"))
	if !d.Editing() {
		t.Fatal("typing must re-enter edit mode")
	}
	_, _ = d.Update(special(tea.KeyTab))
	if d.Editing() {
		t.Fatal("tabbing to a new field must land in navigate mode")
	}

	// SetFocus clamps onto the nearest enabled field, in navigate mode.
	d.SetFocus(4) // station is disabled -> clamp forward to unsolicited
	if got := d.State().Fields[d.Focus()].Key; got != ConnectFieldUnsolicited {
		t.Fatalf("SetFocus(4) landed on %q, want unsolicited", got)
	}
	if d.Editing() {
		t.Fatal("SetFocus must land in navigate mode")
	}

	// A radio never enters edit mode: j adjusts the option, not a draft.
	d2 := connectDialog(t, connectTestState()) // focus 0 = mode radio
	_, _ = d2.Update(pressKey('j', "j"))
	if d2.Editing() {
		t.Fatal("j/k on a radio adjusts the option; it must not enter edit mode")
	}
	if got := d2.State().Fields[0].Value; got != ConnectModeListener {
		t.Fatalf("mode after j = %q, want the wrap to listener", got)
	}
}
