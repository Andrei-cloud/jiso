// inspector_split.go renders the §C layout at wide terminals:
// the FIELDS pane and a right pane (packed/bitmap/raw — Tab retargets
// it) side by side, validation below. Below inspectorSplitMinWidth the
// page keeps the single-tab stack (inspector_view.go).
package pages

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// inspectorSplitMinWidth is the content width at which FIELDS and the
// right pane sit side by side (the baseline).
const inspectorSplitMinWidth = 100

// splitBody is the side-by-side body: FIELDS left, right pane right,
// one blank column between; h excludes the crumb and validation lines
// and y is the body's content-relative top line. The right pane keeps a
// fixed working width (45 inner fits 16-byte hex lines); the left pane
// gets the rest, never below the masked-row minimum (64).
func (in *Inspector) splitBody(w, y, h int) string {
	right := min(max(49, (w-3)*5/16), max(w-3-48, 33))
	// A packed right pane prefers the 82 columns a standard hexdump
	// line needs (8 offset + 2 + 47 byte pairs + 2 + 18 ASCII gutter +
	// borders); it still yields to a left pane floor of 48.
	if rightTab := in.rightTab(); rightTab == viewPacked {
		right = min(max(82, (w-3)*5/16), max(w-3-48, 33))
	}
	left := w - 3 - right

	rightTab := in.rightTab()
	inner := max(h-3, 1) // section title line + box borders

	leftSec := in.sectionW(in.fieldsPaneTitle(), in.fieldsRows(left-4, inner), 0, y, left, h)
	// The right pane starts where the FIELDS section's drawn lines end
	// plus the one blank column (measured, not nominal).
	rightSec := in.sectionW(rightPaneTitle(in.th, rightTab), in.paneBody(rightTab, right-4, inner), lipgloss.Width(leftSec)+1, y, right, h)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftSec, " ", rightSec)
}

// rightTab is the pane Tab retargets at split width: any non-fields tab
// as-is, fields meaning "right pane shows packed".
func (in *Inspector) rightTab() int {
	if in.tab == viewFields {
		return viewPacked
	}

	return in.tab
}

// paneBody dispatches the right pane's body.
func (in *Inspector) paneBody(tab, w, h int) string {
	switch tab {
	case viewBitmap:
		return in.bitmapBody(w)
	case viewRawJSON:
		return in.rawBody()
	default:
		return in.packedBody(w, h)
	}
}

// rightPaneTitle names the right pane box.
func rightPaneTitle(th *theme.Theme, tab int) string {
	switch tab {
	case viewBitmap:
		return titleLine(th, "BITMAP")
	case viewRawJSON:
		return titleLine(th, "RAW JSON")
	default:
		return titleLine(th, "PACKED MESSAGE")
	}
}

// sectionW draws a pre-styled title over a bordered box of total size
// w×h (h includes the title line) through the one shared
// widgets.Section (the §I sectionW convention; the title is NOT
// re-styled). The section's Rect is recorded on the page at its
// content-relative origin.
func (in *Inspector) sectionW(title, body string, x, y, w, h int) string {
	sec := widgets.NewSection(in.th, title)
	sec.TitlePreStyled = true
	out, _ := sec.Render(body, x, y, w, h)
	in.sections = append(in.sections, sectionRect(x, y, out))

	return out
}

// fieldsRows renders the windowed field rows (scroll offset clamped to
// the window) without the pane title or validation line; empty fields
// render the §C empty hint.
func (in *Inspector) fieldsRows(w, inner int) string {
	if dt := in.state.DescribeText; len(dt) > 0 {
		in.top = max(min(in.top, max(len(dt)-inner, 0)), 0)
		lines := make([]string, 0, inner)
		for i, ln := range window(dt, in.top, inner) {
			lines = append(lines, in.describeLine(ln, in.top+i == in.cursor, w))
		}

		return strings.Join(lines, "\n")
	}
	rows := in.visibleRows()
	in.top = max(min(in.top, max(len(rows)-inner, 0)), 0)
	if len(rows) == 0 {
		return in.th.TextMuted.Render(inspectorEmptyHint)
	}
	lines := make([]string, 0, inner)
	for i, vr := range window(rows, in.top, inner) {
		lines = append(lines, in.fieldLine(vr, in.top+i == in.cursor, w))
	}

	return strings.Join(lines, "\n")
}
