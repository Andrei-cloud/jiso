package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// Wireframe §global chrome: "Modals (connect, confirm, forms) render
// centered over the page, esc closes" and "Toasts bottom-right, 3s".
// The overlays are LAYERS over the unchanged page body, never
// replacements for it (the page underneath keeps rendering).

// modalBoxWidth is the wireframe dialog width target; narrow terminals
// clamp to w-8 so the box keeps a margin inside the content area.
func modalBoxWidth(innerW int) int {
	return max(min(60, innerW-8), 20)
}

// modalBox wraps content in the subtle rounded border (ascii "+" under
// the ascii theme), total width width.
func modalBox(th *theme.Theme, width int, content string) string {
	border := lipgloss.RoundedBorder()
	if th.ASCII {
		border = lipgloss.NormalBorder()
	}

	return th.SubtleBorder.Border(border).Width(width).Render(content)
}

// overlayCenter composites box centered over base. base is padded to the
// baseW×baseH canvas; box keeps its own size and overwrites the canvas
// columns it covers. A box taller than the canvas clips at the bottom.
func overlayCenter(base, box string, baseW, baseH int) string {
	rows := canvasRows(base, baseW, baseH)
	bl := strings.Split(strings.TrimRight(box, "\n"), "\n")
	bw := boxWidth(bl)
	x := max((baseW-bw)/2, 0)
	y := max((baseH-len(bl))/2, 0)
	for i, b := range bl {
		if y+i >= len(rows) {
			break
		}
		rows[y+i] = splice(rows[y+i], padRight(b, bw), x, baseW)
	}

	return strings.Join(rows, "\n")
}

// overlayBottomRight composites box in the canvas's bottom-right corner.
// Each box row may carry left padding (right-aligned stacks); only the
// box's own columns overwrite the canvas, so page text to the left of the
// box survives.
func overlayBottomRight(base, box string, baseW, baseH int) string {
	rows := canvasRows(base, baseW, baseH)
	bl := strings.Split(strings.TrimRight(box, "\n"), "\n")
	bw := boxWidth(bl)
	y := max(baseH-len(bl), 0)
	for i, b := range bl {
		if y+i >= len(rows) {
			continue
		}
		x := max(baseW-bw, 0)
		rows[y+i] = splice(rows[y+i], padRight(b, bw), x, baseW)
	}

	return strings.Join(rows, "\n")
}

// canvasRows splits s into lines, clips each to baseW cells, and pads to
// baseH lines.
func canvasRows(s string, baseW, baseH int) []string {
	src := strings.Split(strings.TrimRight(s, "\n"), "\n")
	rows := make([]string, baseH)
	for i := range rows {
		line := ""
		if i < len(src) {
			line = ansi.Cut(src[i], 0, baseW)
		}
		rows[i] = padRight(line, baseW)
	}

	return rows
}

// splice replaces x..x+width(box) of line with box.
func splice(line, box string, x, baseW int) string {
	lw := ansi.StringWidth(line)
	pre := ansi.Cut(line, 0, min(x, lw))
	pre = padRight(pre, x)
	suf := ""
	if end := x + ansi.StringWidth(box); end < lw {
		suf = padLeft(ansi.Cut(line, end, lw), baseW-end)
	}

	return pre + box + suf
}

func boxWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		if cw := ansi.StringWidth(l); cw > w {
			w = cw
		}
	}

	return w
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + s
}
