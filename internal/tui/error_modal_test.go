// error_modal_test.go pins the root-owned error screen: the wrapped body,
// the ten-row paged viewport, keyboard ownership over the frozen page, the
// footer swap, and the mouse scroll/close hits.
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// errModalFixture is the modal's standard test body: n numbered content
// lines "line 01".."line NN", joined with newlines (no trailing one).
func errModalFixture(n int) string {
	ls := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ls = append(ls, fmt.Sprintf("line %02d", i))
	}

	return strings.Join(ls, "\n")
}

// newErrModal builds a modal on the pinned ASCII theme (escape-free,
// "+" borders — same theme oracle as the help goldens).
func newErrModal(title, body string) *errorModal {
	e := newErrorModal(title, body)
	e.th = helpGoldenTheme(colorprofile.ASCII)

	return e
}

// errModalAt opens the modal over §B on a fresh 80×24 root (ASCII theme,
// no app wired).
func errModalAt(t *testing.T, title, body string) *RootModel {
	t.Helper()

	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(ch('2'))

	m.openErrorModal(title, errors.New(body))
	if m.errModal == nil {
		t.Fatal("openErrorModal must leave the modal open")
	}

	return m
}

// mustShow/mustHide assert one cell is drawn/absent in a rendered frame.
func mustShow(t *testing.T, frame string, cells ...string) {
	t.Helper()
	for _, c := range cells {
		if !strings.Contains(frame, c) {
			t.Errorf("frame must show %q:\n%s", c, frame)
		}
	}
}

func mustHide(t *testing.T, frame string, cells ...string) {
	t.Helper()
	for _, c := range cells {
		if strings.Contains(frame, c) {
			t.Errorf("frame must not show %q:\n%s", c, frame)
		}
	}
}

// a 300-char error wraps at the box's inner width: every rendered row
// fits the 60-col box, no rune is lost, and the ASCII profile stays
// 7-bit and escape-free.
func TestErrorModalWrapsLongError(t *testing.T) {
	long := strings.Repeat("x", 300)
	e := newErrModal("cannot load transaction file", long)

	view := e.View(80, 24)
	rows := strings.Split(view, "\n")
	for _, r := range rows {
		if w := lipgloss.Width(r); w > 60 {
			t.Fatalf("rendered row %q is %d cells, over the 60-col box", r, w)
		}
	}
	// The 300 chars cut into 58-cell rows: six content rows below the
	// title, then the hint line between the box rules.
	if len(rows) != 10 {
		t.Fatalf("300 hard-cut chars render 6 content rows + title + hint + 2 rules, got %d rows:\n%s", len(rows), view)
	}
	var joined strings.Builder
	for _, r := range rows[2 : len(rows)-2] {
		inner := ansi.Cut(r, 1, lipgloss.Width(r)-1)
		joined.WriteString(strings.TrimRight(ansi.Strip(inner), " "))
	}
	if joined.String() != long {
		t.Fatalf("wrap lost content: kept %d of 300 chars", joined.Len())
	}
	for _, b := range []byte(view) {
		if b > 0x7f {
			t.Fatalf("ASCII-profile render carries byte %#x", b)
		}
	}
}

// an ordinary spaced error wraps at word runs: lines fill toward the
// inner width without splitting words, and every word survives in order.
func TestErrorModalWrapsAtWordRuns(t *testing.T) {
	body := "open /Users/op/jiso/transaction.json: " + strings.Repeat("unparsable field ", 16)
	e := newErrModal("cannot load transaction file", body)
	_ = e.View(80, 24)

	lines := e.Lines()
	if len(lines) < 4 {
		t.Fatalf("a 312-char error must wrap into several rows, got %d", len(lines))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 58 {
			t.Fatalf("wrapped row %q is %d cells, over the box inner width 58", l, w)
		}
	}
	if lipgloss.Width(lines[0]) < 40 {
		t.Fatalf("wrap must fill toward the inner width, first row is %d cells: %q", lipgloss.Width(lines[0]), lines[0])
	}
	want := strings.Fields(body)
	got := strings.Fields(strings.Join(lines, " "))
	if len(got) != len(want) {
		t.Fatalf("wrap must preserve the words: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("word %d after wrap: got %q, want %q", i, got[i], want[i])
		}
	}
}

// a 25-line body pages ten content rows at a time: the viewport shows the
// first ten, pgdn steps ten rows, the ends clamp.
func TestErrorModalViewportPagesTenRows(t *testing.T) {
	e := newErrModal("cannot load transaction file", errModalFixture(25))

	view := e.View(80, 24)
	mustShow(t, view, "line 01", "line 10")
	mustHide(t, view, "line 11", "line 25")
	if got := len(strings.Split(e.View(80, 24), "\n")); got != 14 {
		t.Fatalf("windowed box must keep its pane: %d rows, want 14", got)
	}

	e.ScrollBy(10) // pgdn: rows 11–20
	view = e.View(80, 24)
	mustShow(t, view, "line 11", "line 20")
	mustHide(t, view, "line 10", "line 21")

	e.ScrollBy(10) // clamps at the end: rows 16–25 stay drawn
	view = e.View(80, 24)
	mustShow(t, view, "line 16", "line 25")
	mustHide(t, view, "line 15")

	e.ScrollBy(-100) // clamps at the top: row 01 first again
	view = e.View(80, 24)
	mustShow(t, view, "line 01", "line 10")
	mustHide(t, view, "line 11")
}

// a pathological long title clips at the box width (with the theme's own
// elision marker) instead of corrupting the box.
func TestErrorModalLongTitleClips(t *testing.T) {
	e := newErrModal(strings.Repeat("t", 80), "connection refused")

	rows := strings.Split(e.View(80, 24), "\n")
	for _, r := range rows {
		if w := lipgloss.Width(r); w > 60 {
			t.Fatalf("clip failed, row is %d cells: %q", w, r)
		}
	}
	mustShow(t, rows[1], strings.Repeat("t", 57))
	if strings.Contains(rows[1], strings.Repeat("t", 58)) {
		t.Errorf("the title must carry the elision marker: %q", rows[1])
	}
}

// a body shorter than the viewport renders whole: no padding rows, and
// ScrollBy has nothing to move.
func TestErrorModalShortBodyRendersWhole(t *testing.T) {
	e := newErrModal("cannot connect", "connection refused")

	view := e.View(80, 24)
	mustShow(t, view, "cannot connect", "connection refused", "enter ok")
	if got := len(strings.Split(view, "\n")); got != 5 {
		t.Fatalf("a one-line body renders title + body + hint + 2 rules = 5 rows, got %d:\n%s", got, view)
	}

	before := e.View(80, 24)
	e.ScrollBy(10)
	if e.scrollOff != 0 {
		t.Fatalf("a fitting body must not scroll: scrollOff=%d", e.scrollOff)
	}
	if e.View(80, 24) != before {
		t.Fatal("ScrollBy on a fitting body must not change the render")
	}
}

// the in-body hint line badges enter/esc through Theme.Key and separates
// with the theme separator.
func TestErrorModalBodyHintLineBadged(t *testing.T) {
	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.TrueColor)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.openErrorModal("cannot load transaction file", errors.New("bad spec"))

	const boldOpen = "\x1b[1;38;2;68;147;248m"
	const reset = "\x1b[m"

	view := m.errModal.View(80, 24)
	for _, k := range []string{"enter", "esc"} {
		if want := boldOpen + k + reset; !strings.Contains(view, want) {
			t.Errorf("hint line lacks the bold-accent Theme.Key badge for %q (%q)", k, want)
		}
	}
	if !strings.Contains(ansi.Strip(view), "enter ok") || !strings.Contains(ansi.Strip(view), "esc close") {
		t.Errorf("hint line must read enter ok / esc close:\n%s", ansi.Strip(view))
	}
}

// while the modal is open it owns the keyboard: j/k and pgup/pgdn move the
// viewport, enter/esc close, and page jumps 1–8 plus q stay inert.
func TestErrorModalOwnsKeysSwallowsPageJumps(t *testing.T) {
	m := errModalAt(t, "cannot load transaction file", errModalFixture(25))
	if got := m.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("fixture must sit on §B, got %q", got)
	}

	_, _ = m.Update(ch('j'))
	if m.errModal.scrollOff != 1 {
		t.Fatalf("j = scrollOff %d, want 1 (content down)", m.errModal.scrollOff)
	}
	_, _ = m.Update(ch('k'))
	if m.errModal.scrollOff != 0 {
		t.Fatalf("k = scrollOff %d, want 0", m.errModal.scrollOff)
	}
	_, _ = m.Update(special(tea.KeyPgDown))
	if m.errModal.scrollOff != 10 {
		t.Fatalf("pgdn = scrollOff %d, want 10", m.errModal.scrollOff)
	}
	mustShow(t, m.View().Content, "line 11", "line 20")
	_, _ = m.Update(special(tea.KeyPgUp))
	if m.errModal.scrollOff != 0 {
		t.Fatalf("pgup = scrollOff %d, want 0", m.errModal.scrollOff)
	}

	for d := '1'; d <= '8'; d++ {
		_, cmd := m.Update(ch(d))
		if isQuit(t, cmd) {
			t.Fatalf("%c must be swallowed by the modal", d)
		}
		if got := m.Current().ID(); got != pages.TransactionsPageID {
			t.Fatalf("%c moved the frozen page to %q", d, got)
		}
		if m.errModal == nil {
			t.Fatalf("%c must not close the modal", d)
		}
	}

	if _, cmd := m.Update(ch('q')); isQuit(t, cmd) {
		t.Fatal("q must be swallowed by the modal, never quit")
	}
	if got := m.StackDepth(); got != 1 {
		t.Fatalf("q popped the stack to depth %d", got)
	}

	_, cmd := m.Update(mod('c', tea.ModCtrl))
	if !isQuit(t, cmd) {
		t.Fatal("ctrl+c stays the graceful exit above the modal")
	}
}

// enter closes without popping the page; esc closes and eats the page's
// own esc-back; after the close the page jumps work again.
func TestErrorModalEnterAndEscClose(t *testing.T) {
	m := errModalAt(t, "cannot load transaction file", errModalFixture(25))

	_, _ = m.Update(special(tea.KeyEnter))
	if m.errModal != nil {
		t.Fatal("enter must close the modal")
	}
	if got := m.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("enter closed through the page: now on %q", got)
	}
	_, _ = m.Update(ch('1'))
	if got := m.Current().ID(); got != "dashboard" {
		t.Fatalf("page jumps must work after the close, got %q", got)
	}

	esc := errModalAt(t, "cannot load transaction file", errModalFixture(25))
	_, _ = esc.Update(special(tea.KeyEsc))
	if esc.errModal != nil {
		t.Fatal("esc must close the modal")
	}
	if got := esc.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("esc must stop at the modal, page escaped to %q", got)
	}
}

// a nil or empty error renders the honest fallback instead of a blank
// box: the title names the action, the body says unknown error.
func TestOpenErrorModalFallsBackOnNilError(t *testing.T) {
	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.openErrorModal("cannot load transaction file", nil)
	if m.errModal == nil {
		t.Fatal("a nil error must still open the modal")
	}
	view := m.View().Content
	mustShow(t, view, "cannot load transaction file", "unknown error")

	empty := NewRootModel(nil)
	empty.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = empty.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	empty.openErrorModal("cannot start server", errors.New(""))
	mustShow(t, empty.View().Content, "cannot start server", "unknown error")
}

// while open, the footer is the global legend group plus the modal's own
// badged keys; the page's context hints stay suppressed.
func TestErrorModalFooterSwapsInModalKeys(t *testing.T) {
	m := errModalAt(t, "cannot load transaction file", errModalFixture(25))

	hints := m.footerHints()

	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7", "8", ":", "?", "q"} {
		found := false
		for _, h := range hints {
			if h.Key == key {
				found = true
			}
		}
		if !found {
			t.Errorf("global legend entry %q dropped from the footer: %+v", key, hints)
		}
	}
	for _, want := range []KeyHint{
		{Key: "enter", Desc: "ok", Primary: true},
		{Key: "esc", Desc: "close", Primary: true},
	} {
		found := false
		for _, h := range hints {
			if h == want {
				found = true
			}
		}
		if !found {
			t.Errorf("footer lacks the modal entry %+v: %+v", want, hints)
		}
	}
	for _, ph := range m.Current().Hints() {
		for _, h := range hints {
			if h == ph {
				t.Errorf("page hint %+v leaked while the modal is open", ph)
			}
		}
	}

	closed := NewRootModel(nil)
	closed.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = closed.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = closed.Update(ch('2'))
	if got := len(closed.footerHints()); got != len(globalFooterHints(&closed.keys))+len(closed.Current().Hints()) {
		t.Fatalf("with the modal closed the footer must be legend + page hints, got %d entries", got)
	}
}

// the modal's footer keys render with the Theme.Key badge, so the strip
// reads as hotkeys, not prose.
func TestErrorModalFooterKeysAreBadged(t *testing.T) {
	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.TrueColor)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(ch('2'))
	m.openErrorModal("cannot load transaction file", errors.New("bad spec"))

	const boldOpen = "\x1b[1;38;2;68;147;248m"
	const reset = "\x1b[m"

	frame := m.View().Content
	for _, k := range []string{"enter", "esc"} {
		if want := boldOpen + k + reset; !strings.Contains(frame, want) {
			t.Errorf("footer lacks the bold-accent badge for %q (%q)", k, want)
		}
	}
	if !strings.Contains(ansi.Strip(frame), "esc close") {
		t.Errorf("footer must read esc close:\n%s", ansi.Strip(frame))
	}
}

// the modal reports into the modalOpen gate, its box publishes a wheel
// region, and a click on dead space outside the box replays esc (close).
func TestErrorModalHitMapWheelAndOutsideClose(t *testing.T) {
	m := errModalAt(t, "cannot load transaction file", errModalFixture(25))
	_ = m.View()

	if !m.modalOpen() {
		t.Fatal("the error modal must report modalOpen while it is up")
	}

	hm := m.buildHitMap()
	rect := m.errModalHitRect()
	inner := m.innerWS()
	ox, oy := m.contentOrigin()

	act, ok := hm.resolve(rect.X+rect.W/2, rect.Y+rect.H/2)
	if !ok || act.kind != hitScroll || act.region != regionErrModal {
		t.Fatalf("box centre = %+v,%v, want a scroll hit on %q", act, ok, regionErrModal)
	}

	// The 14-row box cannot cover the canvas corner: there the close hit
	// answers, spelling esc.
	if rect.Contains(ox, oy) {
		t.Fatalf("fixture: the box must not cover the canvas corner (%d,%d)", ox, oy)
	}
	if oy+13 >= inner.Height {
		t.Fatalf("fixture: canvas too short to leave dead space (inner %d)", inner.Height)
	}
	act2, ok2 := hm.resolve(ox, oy)
	if !ok2 || act2.kind != hitKey || act2.key != theme.KeyEsc {
		t.Fatalf("dead-space click = %+v,%v, want a key hit spelling %q", act2, ok2, theme.KeyEsc)
	}
	// Replaying the resolved hit closes the modal without popping the page.
	_, _ = m.Update(act2.cmd()())
	if m.errModal != nil {
		t.Fatal("the resolved close hit must close the modal")
	}
	if got := m.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("the close click escaped to page %q", got)
	}
	if m.modalOpen() {
		t.Fatal("modalOpen must fall back to false once closed")
	}
}

// the wheel region dispatches content-direction (no negation) and a
// straggler after the close stays inert.
func TestErrorModalScrollMsgDispatch(t *testing.T) {
	m := errModalAt(t, "cannot load transaction file", errModalFixture(25))

	_, _ = m.Update(scrollMsg{region: regionErrModal, delta: 1})
	if m.errModal.scrollOff != 1 {
		t.Fatalf("wheel down = scrollOff %d, want 1", m.errModal.scrollOff)
	}
	_, _ = m.Update(scrollMsg{region: regionErrModal, delta: -1})
	if m.errModal.scrollOff != 0 {
		t.Fatalf("wheel up = scrollOff %d, want 0", m.errModal.scrollOff)
	}

	_, _ = m.Update(special(tea.KeyEsc))
	_, cmd := m.Update(scrollMsg{region: regionErrModal, delta: 1})
	if cmd != nil {
		t.Fatal("a straggler errmodal scrollMsg must be inert, not replayed")
	}
}

// the modal's keys are registered in the §M help registry for every page,
// derived from the same bindings UpdateKey matches on.
func TestErrorModalKeysInHelpRegistry(t *testing.T) {
	g := errorModalHelpGroup()
	if g.Title != helpGroupErrModal {
		t.Fatalf("group title = %q, want %q", g.Title, helpGroupErrModal)
	}

	want := map[string]string{
		"enter":       "ok / close",
		"esc":         "close",
		"j/k":         "scroll one line",
		"pgup/pgdown": "scroll ten lines",
	}
	got := map[string]string{}
	for _, e := range g.Entries {
		got[e.Keys] = e.Note
	}
	for k, note := range want {
		if got[k] != note {
			t.Errorf("registry entry %q = %q, want %q", k, got[k], note)
		}
	}

	m := NewRootModel(nil)
	for _, p := range m.registry {
		found := false
		for _, hg := range helpGroupsFor(p, &m.keys) {
			if hg.Title == helpGroupErrModal {
				found = true
			}
		}
		if !found {
			t.Errorf("page %s registry lacks the %q group", p.ID(), helpGroupErrModal)
		}
	}
}
