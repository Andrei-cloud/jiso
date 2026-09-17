package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// The §M help overlay is a frame-level modal above the page stack (same
// mechanism as the palette and the confirm dialogs, §N1 stacking): it
// replaces the content body, owns the keyboard while open (`?` toggles,
// Esc closes first), and is NOT a page in PageIDs.

const (
	helpBoxMaxWidth  = 60 // §M modal box width
	helpBoxMinWidth  = 40 // below the terminal's width the frame clips
	helpCompactInner = 36 // narrower content switches to compact lines
)

// helpOverlay renders the context-sensitive keymap box for one page.
type helpOverlay struct {
	th      *theme.Theme
	context string
	groups  []helpGroup
	width   int
	// height is the pane budget in CONTENT lines between the box rules;
	// 0 (the default) means unbounded and renders the whole keymap.
	// scrollOff is the wheel window offset; newHelpOverlay builds at 0.
	height    int
	scrollOff int
}

// newHelpOverlay snapshots the CURRENT page's registry plus the global
// group; the overlay is pure display state (no page mutation, no cmds).
// A fresh overlay is a fresh open: scrollOff starts at 0.
func newHelpOverlay(th *theme.Theme, p Page, km *globalKeyMap, width int) *helpOverlay {
	return &helpOverlay{
		th:      th,
		context: helpContextName(p.ID()),
		groups:  helpGroupsFor(p, km),
		width:   width,
	}
}

// openHelp opens the §M overlay over the CURRENT page (context label and
// page-actions block follow the stack top). The palette's "show help"
// action lands here too; opening while open never duplicates.
func (m *RootModel) openHelp() {
	if m.help != nil {
		return
	}
	m.help = newHelpOverlay(m.themeOrNil(), m.Current(), &m.keys, m.innerWS().Width)
	// Give the box its real pane budget (the content canvas minus the
	// box's own two rules) so the wheel has a window to move in when
	// the keymap outgrows the pane; a fitting keymap renders as before.
	m.help.SetHeight(max(m.innerWS().Height-2, 1))
	m.debug.logf("help open context=%s", m.Current().ID())
}

// boxWidth sizes the box for the terminal width: margined at full size,
// near-edge-to-edge when the terminal itself is narrow.
func (h *helpOverlay) boxWidth() int {
	switch {
	case h.width < helpBoxMinWidth+4:
		return max(h.width-2, 12)
	case h.width-4 > helpBoxMaxWidth:
		return helpBoxMaxWidth
	default:
		return h.width - 4
	}
}

// SetHeight tells the overlay how many content lines fit between its
// rules; 0 (default) means unbounded and renders the whole keymap. It
// re-clamps a stale scroll offset the way Table.SetHeight does.
func (h *helpOverlay) SetHeight(height int) {
	if height >= 1 {
		h.height = height
	}
	if maxOff := h.maxScroll(); h.scrollOff > maxOff {
		h.scrollOff = maxOff
	}
}

// ScrollBy moves the content window by d lines (d>0 scrolls DOWN toward
// later groups), clamped to 0..max(0, contentH-paneH). With no pane
// height set the whole keymap already renders, so there is nothing to
// scroll and the offset stays 0.
func (h *helpOverlay) ScrollBy(d int) {
	h.scrollOff += d
	if h.scrollOff < 0 {
		h.scrollOff = 0
	}
	if maxOff := h.maxScroll(); h.scrollOff > maxOff {
		h.scrollOff = maxOff
	}
}

// maxScroll is the deepest offset that keeps the pane full; 0 while no
// pane height is set or the content still fits.
func (h *helpOverlay) maxScroll() int {
	total := len(h.contentLines())
	visible := total
	if h.height > 0 && h.height < total {
		visible = h.height
	}
	return max(0, total-visible)
}

// contentLines lays out the box's inner lines (between the rules), each
// padded to the inner width and wrapped in the side borders. View
// windows this slice and ScrollBy clamps against its length, so both see
// one layout.
func (h *helpOverlay) contentLines() []string {
	boxW := h.boxWidth()
	innerW := max(boxW-4, 8)
	compact := innerW < helpCompactInner

	labelW := 0
	for _, g := range h.groups {
		if len(g.Title) > labelW {
			labelW = len(g.Title)
		}
	}

	vbar := "│"
	if h.th.ASCII {
		vbar = "|"
	}
	box := lipgloss.NewStyle().Foreground(h.th.Border.GetBorderTopForeground())

	var out []string
	for _, g := range h.groups {
		for _, l := range h.groupLines(g, innerW, labelW, compact) {
			pad := strings.Repeat(" ", max(innerW-lipgloss.Width(l), 0))
			out = append(out, box.Render(vbar)+" "+l+pad+" "+box.Render(vbar))
		}
	}

	return out
}

// View renders the box: title rule, the visible window of the content
// lines, closing rule. Lines are packed at segment boundaries and
// truncated, never wrapped. With no pane height set (the golden default)
// the window is the whole keymap.
func (h *helpOverlay) View() string {
	boxW := h.boxWidth()

	dash := "─"
	// four corners: the bottom rule must use boxBL (└) at the left and
	// boxBR (┘) at the right, never the other way round.
	boxTL, boxTR, boxBL, boxBR := "┌", "┐", "└", "┘"
	if h.th.ASCII {
		dash = "-"
		boxTL, boxTR, boxBL, boxBR = "+", "+", "+", "+"
	}

	box := lipgloss.NewStyle().Foreground(h.th.Border.GetBorderTopForeground())
	rule := lipgloss.NewStyle().Foreground(h.th.SubtleBorder.GetBorderTopForeground())

	content := h.contentLines()
	hi := len(content)
	if h.height > 0 && h.scrollOff+h.height < hi {
		hi = h.scrollOff + h.height
	}
	lo := min(max(h.scrollOff, 0), hi) // defensive against a stale offset

	lines := make([]string, 0, 1+(hi-lo)+1)
	lines = append(lines, h.titleLine(box, rule, dash, boxW, boxTL, boxTR))
	lines = append(lines, content[lo:hi]...)
	lines = append(lines, box.Render(boxBL+strings.Repeat(dash, boxW-2)+boxBR))

	return strings.Join(lines, "\n")
}

// titleLine renders the "┌─ HELP ── context: <page> ────┐" rule, clipped
// (never wrapped) when the context label is too long for the box.
func (h *helpOverlay) titleLine(box, rule lipgloss.Style, dash string, boxW int, tl, tr string) string {
	avail := boxW - 2

	title := dash + " " + h.th.Accent.Render("HELP") + " "
	// The styled title's plain width counted in CELLS: the box glyph is
	// multi-byte, so len would miscount the unicode rule's fill.
	titleW := lipgloss.Width(dash + " HELP ")

	// The plain and styled forms are the same width here, so one counter covers
	// the decision and the fill.
	context := dash + dash + " context: " + h.context + " "
	if titleW+lipgloss.Width(context) <= avail {
		title += rule.Render(context)
		titleW += lipgloss.Width(context)
	}

	// When the context does not fit, the title leaves it out rather than
	// cutting it short: a clipped page name plus fill dashes reads as
	// noise. A title that says only HELP is still a title.
	title += rule.Render(strings.Repeat(dash, max(avail-titleW, 0)))

	return box.Render(tl) + title + box.Render(tr)
}

// groupLines lays one group out: styled "keys note" segments joined by
// dim separators, packed into lines of inner width. Full mode aligns the
// first line behind the group-label column (continuation lines indent to
// the same column); compact mode (narrow) drops the label column and puts
// the label on its own line.
func (h *helpOverlay) groupLines(g helpGroup, innerW, labelW int, compact bool) []string {
	sep := h.th.Deemphasized.Render(h.th.Separator())

	labelPrefix, contPrefix := "", ""
	if !compact {
		labelPrefix = h.th.Deemphasized.Render(padHelp(g.Title, labelW)) + " "
		contPrefix = strings.Repeat(" ", labelW+1)
	}

	var lines []string

	if compact {
		lines = append(lines, h.th.Deemphasized.Render(g.Title))
	}

	row := make([]string, 0, len(g.Entries))

	var rowPlain string

	first := true

	flush := func() {
		prefix := contPrefix
		if first {
			prefix = labelPrefix
			first = false
		}

		lines = append(lines, h.fitLine(prefix+strings.Join(row, sep), innerW))
		row, rowPlain = nil, ""
	}

	for _, e := range g.Entries {
		seg := h.th.Key(e.Keys) + " " + h.th.TextPrimary.Render(e.Note)
		segPlain := e.Keys + " " + e.Note

		nextPlain := segPlain
		if rowPlain != "" {
			nextPlain = rowPlain + h.th.Separator() + segPlain
		}

		if len(row) > 0 && lipgloss.Width(nextPlain)+lipgloss.Width(labelPrefix) > innerW {
			flush()
			nextPlain = segPlain
		}

		row = append(row, seg)
		rowPlain = nextPlain
	}

	if len(row) > 0 {
		flush()
	}

	return lines
}

// fitLine clamps one content line to the inner width (truncate, no wrap).
func (h *helpOverlay) fitLine(line string, w int) string {
	return h.th.Truncate(line, w)
}

// padHelp right-pads a group label to the aligned label column.
func padHelp(s string, w int) string {
	if len(s) >= w {
		return s
	}

	return s + strings.Repeat(" ", w-len(s))
}
