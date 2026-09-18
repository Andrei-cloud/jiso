// analyze_items_view.go renders the §J generated-item picker: left, the
// roster with inclusion markers, name and kind; right, the item under the
// cursor exactly as it lands in the file. Below frame.FullWidth the panes
// stack.
package pages

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

const (
	analyzeItemsListW = 46 // roster column width at full width
	analyzeItemsKindW = 12 // kind column inside the roster
)

// itemsPaneH is the picker's pane budget: render clips the overlay to h-2
// lines and its two-line head consumes two, so the panes get contentH-4 —
// never building past the clip, which would hide the last rows forever.
func itemsPaneH(contentH int) int { return max(contentH-4, 4) }

// itemsWindow is the visible roster rows shared by the renderer and the
// scroll keys.
func (a *Analyze) itemsWindow() int {
	_, h := frame.ContentSize(a.width, a.height)

	return max(h-6, 4)
}

// itemsBodyY is the content-relative row where the picker panes start (the
// rail line plus the two-line head) — the offset the wheel hit map records.
const itemsBodyY = 3

// itemsOverlay composes the picker body, recording the panes' layout rects
// for the wheel hit map.
func (a *Analyze) itemsOverlay(w, h int) string {
	included := 0
	for _, on := range a.itemSel {
		if on {
			included++
		}
	}
	head := titleLine(a.th, "ITEMS  "+strconv.Itoa(included)+" of "+strconv.Itoa(len(a.state.Items))+
		" included") + "\n" +
		hintSpans(a.th, a.th.Dim,
			"space", "toggle", "a", "all/none", "tab", "preview", "enter", "apply", "esc", "close")

	bodyH := itemsPaneH(h)
	var body string
	if w >= frame.FullWidth {
		lw := min(analyzeItemsListW, max(w/2, 24))
		gap := " "
		pw := max(w-lw-len([]rune(gap)), 24)
		roster := a.itemsRoster(lw, bodyH)
		prev := a.itemsPreview(pw, bodyH)
		a.itemsRect = geom.Rect{X: 0, Y: itemsBodyY, W: lw, H: bodyH}
		a.previewRect = geom.Rect{X: lw + len([]rune(gap)), Y: itemsBodyY, W: pw, H: bodyH}
		body = lipgloss.JoinHorizontal(lipgloss.Top, roster, gap, prev)
	} else {
		lh := max(bodyH/2, 4)
		ph := max(bodyH-lh, 3)
		roster := a.itemsRoster(w, lh)
		prev := a.itemsPreview(w, ph)
		a.itemsRect = geom.Rect{X: 0, Y: itemsBodyY, W: w, H: lh}
		a.previewRect = geom.Rect{X: 0, Y: itemsBodyY + lh, W: w, H: ph}
		body = roster + "\n" + prev
	}

	return head + "\n" + body
}

// itemsRoster draws the selectable roster rows: cursor cell, inclusion
// marker, name and kind.
func (a *Analyze) itemsRoster(w, h int) string {
	// Stable columns — [cursor+mark gutter][NAME nameW][gap][KIND kindW];
	// the name is PADDED to nameW so the KIND column lines up on every row
	// AND with the header.
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
		// Record the drawn row for the click hit map: the roster is pinned
		// to content x 0 and every appended line is one pane row down.
		a.selRows = append(a.selRows, SelectRegion{
			ID:    RegionAnalyzeItems,
			Rect:  geom.Rect{X: 0, Y: itemsBodyY + len(lines) - 1, W: w, H: 1},
			Index: i,
		})
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

// previewWindow is the preview's visible row count — the exact geometry
// itemsOverlay hands itemsPreview, so ScrollPreview clamps against what the
// operator actually sees.
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

// itemsPreview draws the item under the cursor as it lands in the file,
// PAN-masked upstream. A preview taller than the pane is a WINDOW, not a
// clip: the body starts at previewOff and the title carries the position.
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
