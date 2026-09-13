package widgets

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// ToastKind selects the theme token a toast renders with: info the
// accent, success status.ok, error status.error.
type ToastKind int

const (
	// ToastInfo renders accent + bullet ("• text", "* text" ascii).
	ToastInfo ToastKind = iota
	// ToastSuccess renders theme.Status(KindOK, text).
	ToastSuccess
	// ToastError renders theme.Status(KindError, text).
	ToastError
)

// MaxToastStack caps visible toasts; Push drops the oldest overflow.
const MaxToastStack = 3

// toastGlyph is the info bullet with its ASCII fallback (the ok/error
// symbols are theme-owned via Status).
const (
	GlyphToastInfo  = "•"
	ASCIIToastInfo  = "*"
	toastInfoPrefix = " "
)

// toastEntry is one transient line; at is the OWNER-injected timestamp
// (the widget never reads the clock).
type toastEntry struct {
	text string
	kind ToastKind
	at   time.Time
}

// Toast is the shared transient status line (TUI-406b): Push appends
// (stack capped at MaxToastStack, oldest dropped), Prune removes
// entries at least ttl older than the now the OWNER passes — the
// widget owns no clock, the root's tick supplies it. View renders the
// active stack bottom-to-top as right-aligned lines (the owner places
// the block at the frame's bottom-right); an empty stack renders "".
type Toast struct {
	theme *theme.Theme
	width int // right-align width (the owner sets the content width)
	items []toastEntry
}

// NewToast builds an empty toast stack.
func NewToast(th *theme.Theme) *Toast { return &Toast{theme: th} }

// SetSize sets the right-align width for View.
func (t *Toast) SetSize(width int) {
	if width > 0 {
		t.width = width
	}
}

// Push appends a line stamped with the owner's now; overflow drops the
// oldest entry.
func (t *Toast) Push(text string, kind ToastKind, at time.Time) {
	if text == "" {
		return
	}
	t.items = append(t.items, toastEntry{text: text, kind: kind, at: at})
	if n := len(t.items); n > MaxToastStack {
		t.items = t.items[n-MaxToastStack:]
	}
}

// Prune drops entries whose age (now - at) reached ttl.
func (t *Toast) Prune(now time.Time, ttl time.Duration) {
	kept := t.items[:0]
	for _, e := range t.items {
		if now.Sub(e.at) < ttl {
			kept = append(kept, e)
		}
	}
	t.items = kept
}

// Len reports the active toast count.
func (t *Toast) Len() int { return len(t.items) }

// Lines returns the rendered toast lines oldest-first (tests and
// owners that compose themselves).
func (t *Toast) Lines() []string {
	out := make([]string, 0, len(t.items))
	for _, e := range t.items {
		out = append(out, t.render(e))
	}

	return out
}

// render maps kind to its theme token line (never color alone: every
// kind carries a symbol).
func (t *Toast) render(e toastEntry) string {
	switch e.kind {
	case ToastSuccess:
		return t.theme.Status(theme.KindOK, e.text)
	case ToastError:
		return t.theme.Status(theme.KindError, e.text)
	default:
		bullet := GlyphToastInfo
		if t.theme.ASCII {
			bullet = ASCIIToastInfo
		}

		return t.theme.Accent.Render(bullet + toastInfoPrefix + e.text)
	}
}

// View renders the active stack as right-aligned lines (oldest top),
// each at most the configured width; empty stack renders "".
func (t *Toast) View() string {
	if len(t.items) == 0 {
		return ""
	}
	lines := t.Lines()
	for i, l := range lines {
		lines[i] = rightPad(l, t.width)
	}

	return strings.Join(lines, "\n")
}

// rightPad left-pads s with spaces to display width w (no-op when s is
// wider; the owner clips overflow).
func rightPad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}

	return s
}
