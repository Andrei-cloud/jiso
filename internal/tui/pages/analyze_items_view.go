// analyze_items_view.go renders the §J generated-item picker (UAT round
// 6: after a run the operator chooses WHICH generated transaction types
// land in the file — this overlay replaces the old dry-run preview
// text). Left: the roster with a ✓/· inclusion marker, the name and the
// config kind; right: the item under the cursor exactly as it lands in
// the file (indented JSON). space toggles, a all/none, Enter applies,
// Esc closes. Below frame.FullWidth the panes stack.
package pages

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
)

const (
	analyzeItemsListW = 46 // roster column width at full width
	analyzeItemsKindW = 12 // kind column inside the roster
)

// itemsPaneH is the picker's pane budget: render clips the overlay to
// h-2 lines and the overlay's own two-line head consumes two of them.
// UAT round 8 finding 8: the panes used to build h-2 rows — two past
// that budget — so their last two lines were always clipped away and
// stayed unreachable even at the preview's bottom scroll clamp (the
// keys-side itemsWindow has long conceded the shrink with h-6 rows).
func itemsPaneH(contentH int) int { return max(contentH-4, 4) }

// itemsWindow is the picker geometry shared by the renderer and the
// scroll keys: the roster column width and the visible roster rows.
func (a *Analyze) itemsWindow() (listW, rows int) {
	_, h := frame.ContentSize(a.width, a.height)

	return analyzeItemsListW, max(h-6, 4)
}

// itemsOverlay composes the picker body for the content area.
func (a *Analyze) itemsOverlay(w, h int) string {
	included := 0
	for _, on := range a.itemSel {
		if on {
			included++
		}
	}
	sep := a.th.Separator()
	head := titleLine(a.th, "ITEMS  "+strconv.Itoa(included)+" of "+strconv.Itoa(len(a.state.Items))+
		" included") + "\n" +
		a.th.Dim.Render("space toggle"+sep+"a all/none"+sep+"enter apply"+sep+"esc close")

	bodyH := itemsPaneH(h)
	var body string
	if w >= frame.FullWidth {
		lw := min(analyzeItemsListW, max(w/2, 24))
		gap := " "
		pw := max(w-lw-len([]rune(gap)), 24)
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			a.itemsRoster(lw, bodyH), gap,
			a.itemsPreview(pw, bodyH))
	} else {
		lh := max(bodyH/2, 4)
		ph := max(bodyH-lh, 3)
		body = a.itemsRoster(w, lh) + "\n" + a.itemsPreview(w, ph)
	}

	return head + "\n" + body
}

// itemsRoster draws the selectable roster rows: cursor cell, inclusion
// marker, name and kind.
func (a *Analyze) itemsRoster(w, h int) string {
	// Stable columns — [cursor+mark gutter][NAME nameW][gap][KIND kindW].
	// The name is PADDED to nameW so the KIND column lines up on every
	// row AND with the header (UAT round 6 QA: the kind column was
	// ragged because names were truncated but never padded, and the
	// header used a 2-col gutter while rows use a 4-col cursor+mark one).
	const gutter, gap = 4, 2
	nameW := max(w-gutter-analyzeItemsKindW-gap, 10)
	head := padRight("", gutter) + padRight("NAME", nameW) + padRight("", gap) +
		padRight("KIND", analyzeItemsKindW)
	lines := make([]string, 0, h+1)
	lines = append(lines, a.th.Deemphasized.Render(head))
	hidden := 0
	for i, it := range a.state.Items {
		if i < a.itemOff || i >= a.itemOff+max(h-1, 1) {
			if i >= a.itemOff+h {
				hidden++
			}
			continue
		}
		on := i < len(a.itemSel) && a.itemSel[i]
		cell, mark := "  ", "· "
		if i == a.itemCursor {
			cell = a.pick("\u25b8", ">") + " "
		}
		if on {
			mark = a.pick("\u2713", "x") + " "
		}
		name := padRight(truncateCells(it.Name, nameW, clipTail(a.th)), nameW)
		line := cell + mark + name + strings.Repeat(" ", gap) + padRight(it.Kind, analyzeItemsKindW)
		if i == a.itemCursor {
			lines = append(lines, a.th.Accent.Render(clipCells(line, w, clipTail(a.th))))
		} else {
			lines = append(lines, a.th.TextPrimary.Render(clipCells(line, w, clipTail(a.th))))
		}
	}
	if hidden > 0 {
		lines = append(lines, a.th.Dim.Render("  +"+strconv.Itoa(hidden)+" more"))
	}

	return strings.Join(lines, "\n")
}

// previewBodyH is the preview pane's text-row budget: the pane height
// minus its one title line (the single policy shared by the renderer
// and the ScrollPreview clamp).
func previewBodyH(paneH int) int { return max(paneH-1, 1) }

// previewWindow is the preview pane's visible row count for the current
// terminal size — the exact geometry itemsOverlay hands itemsPreview —
// so ScrollPreview clamps against what the operator actually sees.
func (a *Analyze) previewWindow() int {
	w, h := frame.ContentSize(a.width, a.height)
	bodyH := itemsPaneH(h)
	if w >= frame.FullWidth {
		return previewBodyH(bodyH)
	}

	return previewBodyH(max(bodyH-max(bodyH/2, 4), 3))
}

// previewContentHeight is the total row count of the previewed item's
// file form (what ScrollPreview clamps against).
func (a *Analyze) previewContentHeight() int {
	if a.itemCursor >= len(a.state.Items) {
		return 0
	}

	return len(strings.Split(plainBlock(a.state.Items[a.itemCursor].Preview), "\n"))
}

// itemsPreview draws the item under the cursor exactly as it lands in
// the file (the JSON the write stores), PAN-masked upstream when the
// security toggle is on. UAT round 8 finding 8: a preview taller than
// the pane is a WINDOW, not a clip — the body starts at previewOff and
// the title carries the window position while the content overflows;
// the pane title takes the accent only while the preview sub-pane holds
// the picker focus (the paneTitle convention).
func (a *Analyze) itemsPreview(w, h int) string {
	if a.itemCursor >= len(a.state.Items) {
		return a.th.Dim.Render("preview")
	}
	it := a.state.Items[a.itemCursor]
	rows := previewBodyH(h)
	lines := strings.Split(plainBlock(it.Preview), "\n")
	off := min(a.previewOff, max(len(lines)-rows, 0)) // defensive: a pushed state can shrink the content
	body := strings.Join(lines[off:min(off+rows, len(lines))], "\n")

	title := "PREVIEW  " + it.Name + " (" + it.Kind + ")"
	if len(lines) > rows {
		title += "  " + strconv.Itoa(off+1) + "-" + strconv.Itoa(min(off+rows, len(lines))) +
			"/" + strconv.Itoa(len(lines))
	}
	head := paneTitle(a.th, title, a.previewFocused)

	return head + "\n" + clipBlockStyled(a.th, plainBlock(body), rows, w)
}

// truncateCells cuts s to n cells appending the theme ellipsis.
func truncateCells(s string, n int, ell string) string {
	if n <= 0 {
		return ""
	}
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)

	return string(r[:max(n-len([]rune(ell)), 0)]) + ell
}
