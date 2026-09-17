// inspector_view.go renders the §C body: the breadcrumb/tab line
// (Transactions > Purchase  Messages(1/1)  [fields] [bitmap] [packed]
// [raw json]) above the selected view's pane. Sizing is delegated to
// frame.ContentSize (transactions_view.go pattern); every body is
// flattened to exactly h lines of at most w cells.
package pages

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// titleInspector is the §C section title (doubles as the slot's
// frame-visible title, like titleTransactions).
const titleInspector = "INSPECTOR"

// inspectorEmptyHint is the §C empty state (the comparison table:
// “Select a transaction first.”).
const inspectorEmptyHint = "Select a transaction first."

// fieldsPaneTitle is the fields pane header (the interpolated/live
// marker from the design).
const fieldsPaneTitle = "FIELDS (interpolated"

// fieldsLive is the fields-pane title suffix after the arrow.
const fieldsLive = "live)"

// validationPrefix labels the §C validation line.
const validationPrefix = "validation:"

// View renders the §C body for the frame's content area.
func (in *Inspector) View() tea.View {
	w, h := frame.ContentSize(in.width, in.height)

	return tea.NewView(in.render(w, h))
}

// render lays out the crumb/tabs line plus the active view's pane,
// clipped to exactly h lines of at most w cells. At split width the
// FIELDS and right panes sit side by side with validation below.
func (in *Inspector) render(w, h int) string {
	in.sections = in.sections[:0] // redraw the section rects alongside the ink

	// The [packed] tab owns the full width: a standard hexdump line is
	// 78 cells and never fits a side pane at baseline widths (UAT 3b).
	if w >= inspectorSplitMinWidth && in.tab == viewPacked {
		block := in.crumbTabs(w) + "\n" +
			in.sectionW(rightPaneTitle(in.th, viewPacked), in.packedBody(w-4, max(h-5, 1)), 0, 1, w, max(h-2, 0)) + "\n" +
			in.validationLine()

		return clipBlockStyled(in.th, block, h, w)
	}
	if w >= inspectorSplitMinWidth {
		block := in.crumbTabs(w) + "\n" +
			in.splitBody(w, 1, max(h-2, 0)) + "\n" +
			in.validationLine()

		return clipBlockStyled(in.th, block, h, w)
	}
	body := in.stateBody(w, max(h-1, 0))

	return clipBlockStyled(in.th, in.crumbTabs(w)+"\n"+body, h, w)
}

// crumbTabs is line 1: "INSPECTOR  Transactions > Purchase  Messages(1/1)
// [fields] [bitmap] [packed] [raw json]". The active tab is bracketed and
// accented, inactive tabs are dim and unbracketed (never colour alone).
// At split width fields is always active (left pane) and the brackets
// mark the right pane's tab.
func (in *Inspector) crumbTabs(w int) string {
	parts := []string{titleLine(in.th, titleInspector)}
	parts = append(parts, in.breadcrumb())
	parts = append(parts, in.msgPos())

	split := w >= inspectorSplitMinWidth
	rightTab := in.rightTab()
	tabs := make([]string, 0, ViewTabCount)
	for i := 0; i < ViewTabCount; i++ {
		label := in.state.Views.label(i)
		active := i == in.tab
		if split && (i == viewFields || i == rightTab) {
			active = true
		}
		if active {
			tabs = append(tabs, in.th.Accent.Render("["+label+"]"))

			continue
		}
		tabs = append(tabs, in.th.Dim.Render(label))
	}

	return clipCells(strings.Join(append(parts, strings.Join(tabs, " ")), "  "), w, clipTail(in.th))
}

// breadcrumb renders "Transactions > <TxName>" (dash when no tx is
// inspected). The separator is the ▸, ASCII ">" in ascii mode.
func (in *Inspector) breadcrumb() string {
	tx := dashIf(in.th, in.state.TxName)

	return in.th.Deemphasized.Render("Transactions "+crumbSep(in.th)) + " " +
		in.th.TextPrimary.Render(tx)
}

// crumbSep is the breadcrumb separator glyph for th's glyph mode.
func crumbSep(th *theme.Theme) string {
	if th.ASCII {
		return ">"
	}

	return "▸"
}

// msgPos renders "Messages(i/N)"; an unknown total (0) renders dashes,
// never zeros (unknown ≠ zero).
func (in *Inspector) msgPos() string {
	n := dashIf(in.th, itoz(in.state.MsgTotal))
	i := n
	if in.state.MsgTotal > 0 {
		i = itoz(min(max(in.state.MsgIndex, 1), in.state.MsgTotal))
	}

	return in.th.Deemphasized.Render("Messages(" + i + "/" + n + ")")
}

// itoz formats a non-negative int; 0 maps to "" so callers dash it
// (MsgTotal 0 means "unknown", not "zero messages").
func itoz(n int) string {
	if n <= 0 {
		return ""
	}

	return strconv.Itoa(n)
}

// stateBody dispatches the active view's pane body.
func (in *Inspector) stateBody(w, h int) string {
	switch in.tab {
	case viewBitmap:
		return in.bitmapBody(w)
	case viewPacked:
		return in.packedBody(w, h)
	case viewRawJSON:
		return in.rawBody()
	default:
		return in.fieldsBody(w, h)
	}
}

// fieldsHeight is the scrollable rows window of the fields view: the
// body height minus the crumb line, the pane title, and the validation
// line.
func (in *Inspector) fieldsHeight() int {
	_, h := frame.ContentSize(in.width, in.height)

	return max(h-3, 0)
}

// fieldsBody computes the real rows window height (body - title -
// validation line) and renders title, scrolled rows, validation line.
func (in *Inspector) fieldsBody(w, h int) string {
	inner := max(h-2, 0)

	return in.fieldsPaneTitle() + "\n" + in.fieldsRows(w, inner) + "\n" + in.validationLine()
}

// fieldsPaneTitle renders the fields pane header line.
func (in *Inspector) fieldsPaneTitle() string {
	return in.th.Deemphasized.Render(fieldsPaneTitle+" ") +
		autoArrow(in.th) + " " + in.th.Deemphasized.Render(fieldsLive)
}

// validationLine renders "validation: ✓ ok · …" (symbol+word per
// ValidationRow; a dash when root ran no checks).
func (in *Inspector) validationLine() string {
	head := in.th.Deemphasized.Render(validationPrefix)
	if len(in.state.Validation) == 0 {
		return head + " " + dashIf(in.th, "")
	}
	parts := make([]string, 0, len(in.state.Validation))
	for _, v := range in.state.Validation {
		kind := theme.KindOK
		if !v.OK {
			kind = theme.KindError
		}
		text := dashIf(in.th, v.Text)
		parts = append(parts, in.th.Status(kind, text))
	}

	return head + " " + strings.Join(parts, in.th.Deemphasized.Render(" "+midDot(in.th)+" "))
}

// midDot is the validation-line separator glyph for th's glyph mode.
func midDot(th *theme.Theme) string {
	if th.ASCII {
		return "-"
	}

	return "·"
}

// window slices rows[from:from+n] (short slice tolerated).
func window[T any](rows []T, from, n int) []T {
	if from > len(rows) {
		from = len(rows)
	}
	to := min(from+n, len(rows))

	return rows[from:to]
}

// autoArrow is the "auto → value" arrow glyph for th's glyph mode.
func autoArrow(th *theme.Theme) string {
	if th.ASCII {
		return "->"
	}

	return "→"
}
