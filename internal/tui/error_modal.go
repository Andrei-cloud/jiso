// error_modal.go is the root-owned error screen: one modal box over the
// current page that makes a failed action's whole error readable — long
// lines wrap at the box's inner width, longer bodies page ten content rows
// at a time. Flow files call openErrorModal; the screen is pure display
// state (no page mutation, no cmds).

package tui

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

const (
	// errModalContentRows is the viewport in BODY rows — "10 lines at a
	// time". The title and hint line are chrome outside it.
	errModalContentRows = 10
	// boxChromeRows are the box lines around the viewport: two rules, the
	// title, and the hint line.
	boxChromeRows = 4
	// errModalPageStep is the pgup/pgdn stride in body rows.
	errModalPageStep = 10
	// errModalFallbackBody is what a nil or empty error renders instead
	// of an empty box.
	errModalFallbackBody = "unknown error"
	// defaultFrameWidth is frame.Props' fallback width (80×24): the width
	// a modal wraps at before its first View sizes it.
	defaultFrameWidth = 80
)

// errorModal is the error screen: title (the failed action), wrapped body,
// ten-row windowed viewport, one hint line. nil on RootModel.errModal
// means closed.
type errorModal struct {
	th    *theme.Theme
	title string
	body  string
	keys  errorModalKeys

	// wrapW is the inner width the body is currently wrapped at and
	// availH the body rows the canvas can fit; bodyLines is that wrap.
	// relayout rebuilds on a size change and re-clamps the stale offset
	// the way helpOverlay.SetHeight does.
	wrapW     int
	availH    int
	bodyLines []string
	scrollOff int
}

// errorModalKeys are the screen's bindings. UpdateKey matches on these,
// and the §M group and footer entries derive from them — never a
// hand-copied key list.
type errorModalKeys struct {
	ok, cancel       key.Binding // enter ok; esc close
	lineUp, lineDown key.Binding // k up, j down (content direction)
	pageUp, pageDown key.Binding // pgup/pgdown ten rows
}

// newErrorModalKeys spells the bindings the way the other root keymaps
// do; the help keys are the footer entries' text.
func newErrorModalKeys() errorModalKeys {
	return errorModalKeys{
		ok:       key.NewBinding(key.WithKeys(theme.KeyEnter), key.WithHelp(theme.KeyEnter, "ok")),
		cancel:   key.NewBinding(key.WithKeys(theme.KeyEsc), key.WithHelp(theme.KeyEsc, "close")),
		lineUp:   key.NewBinding(key.WithKeys("k")),
		lineDown: key.NewBinding(key.WithKeys("j")),
		pageUp:   key.NewBinding(key.WithKeys("pgup")),
		pageDown: key.NewBinding(key.WithKeys("pgdown")),
	}
}

// newErrorModal builds the screen for one failed action; a fresh open
// starts at the top of the body. Before its first View it lays out at the
// default frame's box (80×24).
func newErrorModal(title, body string) *errorModal {
	e := &errorModal{
		title:  title,
		body:   body,
		keys:   newErrorModalKeys(),
		wrapW:  modalInnerWidth(defaultFrameWidth),
		availH: errModalContentRows,
	}
	e.bodyLines = wrapErrorBody(e.body, e.wrapW)

	return e
}

// openErrorModal opens the error screen over the current page. The title
// names the failed action; a nil or empty error renders the honest
// fallback message instead of err.Error(). A newer error replaces the
// screen that is already open.
func (m *RootModel) openErrorModal(title string, err error) {
	body := errModalFallbackBody
	if err != nil && err.Error() != "" {
		body = err.Error()
	}

	e := newErrorModal(title, body)
	e.th = m.themeOrNil()
	m.errModal = e
	m.debug.logf("error modal open title=%q", title)
}

// modalInnerWidth is the content width inside the modal box drawn at
// total width w (the box spends one border column per side).
func modalInnerWidth(w int) int {
	return max(modalBoxWidth(w)-2, 8)
}

// relayout re-wraps the body for a canvas of width w and keeps the rows
// the canvas can fit; same size, same layout (View runs again per frame).
func (e *errorModal) relayout(w, h int) {
	inner, avail := modalInnerWidth(w), max(h-boxChromeRows, 1)
	if inner == e.wrapW && avail == e.availH {
		return
	}
	e.wrapW, e.availH = inner, avail
	e.bodyLines = wrapErrorBody(e.body, inner)
	e.clamp()
}

// viewport is how many body rows the pane shows: the fixed ten, fewer
// when the body is shorter or the canvas cannot fit ten rows plus the
// box's own chrome (no padding rows either way).
func (e *errorModal) viewport() int {
	return min(errModalContentRows, len(e.bodyLines), e.availH)
}

// maxScroll is the deepest offset that keeps the pane full; 0 while the
// body fits.
func (e *errorModal) maxScroll() int {
	return max(0, len(e.bodyLines)-e.viewport())
}

func (e *errorModal) clamp() {
	if e.scrollOff < 0 {
		e.scrollOff = 0
	}
	if maxOff := e.maxScroll(); e.scrollOff > maxOff {
		e.scrollOff = maxOff
	}
}

// ScrollBy moves the body window by d rows (d>0 scrolls DOWN toward later
// lines — the app-wide content direction), clamped to 0..maxScroll. A body
// that fits has nothing to scroll, so the offset stays 0.
func (e *errorModal) ScrollBy(d int) {
	e.scrollOff += d
	e.clamp()
}

// Lines reports the wrapped body rows at the last View's inner width.
func (e *errorModal) Lines() []string { return e.bodyLines }

// UpdateKey applies one key to the screen and reports whether it closes.
// enter/esc close; j/k step one row and pgup/pgdown ten (clamped); every
// other key — key releases included — is swallowed while the screen owns
// the keyboard.
func (e *errorModal) UpdateKey(msg tea.KeyMsg) (closes bool) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}

	switch {
	case key.Matches(km, e.keys.ok), key.Matches(km, e.keys.cancel):
		return true
	case key.Matches(km, e.keys.lineUp):
		e.ScrollBy(-1)
	case key.Matches(km, e.keys.lineDown):
		e.ScrollBy(1)
	case key.Matches(km, e.keys.pageUp):
		e.ScrollBy(-errModalPageStep)
	case key.Matches(km, e.keys.pageDown):
		e.ScrollBy(errModalPageStep)
	}

	return false
}

// footerHints are the screen's own footer entries: the global legend plus
// these two (and only these) fill the strip while it is open. Primary so
// even the narrow footer keeps the way out.
func (e *errorModal) footerHints() []frame.KeyHint {
	ok, cancel := e.keys.ok.Help(), e.keys.cancel.Help()

	return []frame.KeyHint{
		{Key: ok.Key, Desc: ok.Desc, Primary: true},
		{Key: cancel.Key, Desc: cancel.Desc, Primary: true},
	}
}

// View renders the modal box (same modalBox as the confirms and the
// picker): title line, the ten-row window of the body, the badged hint
// line, all inside the box rules; the caller centers it. h is the canvas
// height: a canvas shorter than the ten-row box caps the window the way
// helpOverlay.SetHeight does, so the box never outgrows the frame.
func (e *errorModal) View(w, h int) string {
	e.relayout(w, h)

	hi := len(e.bodyLines)
	if win := e.scrollOff + e.viewport(); win < hi {
		hi = win
	}
	lo := min(max(e.scrollOff, 0), hi) // defensive against a stale offset

	lines := make([]string, 0, e.viewport()+3)
	lines = append(lines, e.th.Truncate(e.th.StatusError.Render(e.title), e.wrapW))
	lines = append(lines, e.bodyLines[lo:hi]...)
	lines = append(lines, e.th.Truncate(e.hintLine(), e.wrapW))

	return modalBox(e.th, modalBoxWidth(w), strings.Join(lines, "\n"))
}

// hintLine carries the screen's keys in-body (overlay parity): badged via
// Theme.Key, described via the muted token, joined by the theme's own
// separator so the ASCII profile stays 7-bit.
func (e *errorModal) hintLine() string {
	ok, cancel := e.keys.ok.Help(), e.keys.cancel.Help()
	sep := e.th.Deemphasized.Render(e.th.Separator())

	return e.th.Key(ok.Key) + " " + e.th.Deemphasized.Render(ok.Desc) +
		sep + e.th.Key(cancel.Key) + " " + e.th.Deemphasized.Render(cancel.Desc)
}

// wrapErrorBody lays one error message into rows at most w cells wide.
// Normal lines pack at word runs; a single word longer than the box is
// hard-cut at the width — a modal exists to make the whole error
// readable, so nothing may overflow or hide behind an ellipsis.
func wrapErrorBody(body string, w int) []string {
	if w <= 0 {
		return nil
	}

	var out []string
	for _, para := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if para == "" {
			out = append(out, "")

			continue
		}

		cur := ""
		for _, word := range strings.Fields(para) {
			for ansi.StringWidth(word) > w {
				if cur != "" {
					out = append(out, cur)
					cur = ""
				}
				out = append(out, ansi.Cut(word, 0, w))
				word = ansi.Cut(word, w, ansi.StringWidth(word))
			}
			switch {
			case cur == "":
				cur = word
			case ansi.StringWidth(cur)+1+ansi.StringWidth(word) <= w:
				cur += " " + word
			default:
				out = append(out, cur)
				cur = word
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}

	return out
}
