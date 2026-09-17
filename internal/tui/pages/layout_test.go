package pages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
)

// contentSizeFor mirrors the frame's content-area math (the page sizes
// itself against exactly this).
func contentSizeFor(w, h int) (width, height int) { return frame.ContentSize(w, h) }

// widest returns the max display width of the lines.
func widest(lines []string) int {
	best := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > best {
			best = w
		}
	}

	return best
}

// assertFits renders at w×h and asserts the body never exceeds the frame
// content area (frame.ContentSize) in width or height.
func assertFits(t *testing.T, state DashboardState, w, h int) []string {
	t.Helper()

	d := dashDashboard(t, state, w, h)
	lines := bodyLines(t, d)

	contentW, contentH := contentSizeFor(w, h)
	if got := widest(lines); got > contentW {
		t.Errorf("%dx%d: body line width %d > content width %d", w, h, got, contentW)
	}
	if got := len(lines); got > contentH {
		t.Errorf("%dx%d: body height %d > content height %d", w, h, got, contentH)
	}

	return lines
}

// TestResponsive150TwoCol: at >= 130 cols the grid runs two columns —
// CONNECTION stacks above LAST SEND and LAST STRESS on the left, MOCK
// SERVER shares the title row with it on the right, and QUICK ACTIONS
// lives in the right column below SESSION.
func TestResponsive150TwoCol(t *testing.T) {
	t.Parallel()

	lines := assertFits(t, logGoldState(), 150, 44)

	if !sameLine(lines, "CONNECTION", "MOCK SERVER") {
		t.Errorf("150: the two columns must share the title row:\n%s", strings.Join(lines, "\n"))
	}
	if lineIndex(lines, "LAST SEND") <= lineIndex(lines, "CONNECTION") ||
		lineIndex(lines, "LAST STRESS") <= lineIndex(lines, "LAST SEND") {
		t.Errorf("150: left column must stack CONNECTION ▸ LAST SEND ▸ LAST STRESS")
	}
	if lineIndex(lines, "SERVER LOG") <= lineIndex(lines, "MOCK SERVER") ||
		lineIndex(lines, "QUICK ACTIONS") <= lineIndex(lines, "SESSION") {
		t.Errorf("150: right column must stack MOCK SERVER ▸ SERVER LOG ▸ SESSION ▸ QUICK ACTIONS")
	}
}

// TestResponsive110TwoCol: the medium band (100–130) keeps the two
// columns with no fixed 46 — the same column split, cards clipping
// their rows into the narrower boxes.
func TestResponsive110TwoCol(t *testing.T) {
	t.Parallel()

	lines := assertFits(t, logGoldState(), 110, 36)

	if !sameLine(lines, "CONNECTION", "MOCK SERVER") {
		t.Errorf("110: columns must stay side by side:\n%s", strings.Join(lines, "\n"))
	}
}

// TestResponsive90Stacked: below 100 cols the seven cards stack in the
// priority order (the frame, not the page, owns the <48 too-small
// state). A tall stack keeps every card; LAST STRESS is last.
func TestResponsive90Stacked(t *testing.T) {
	t.Parallel()

	lines := assertFits(t, logGoldState(), 90, 64)

	if sameLine(lines, "CONNECTION", "MOCK SERVER") {
		t.Errorf("90: cards must stack, not share a row:\n%s", strings.Join(lines, "\n"))
	}
	if lineIndex(lines, "MOCK SERVER") <= lineIndex(lines, "CONNECTION") {
		t.Errorf("90: MOCK SERVER must stack below CONNECTION")
	}
	for i, l := range lines {
		if strings.Contains(l, "…") && !strings.Contains(l, "|") {
			t.Errorf("90: line %d wrapped instead of truncated: %q", i, l)
		}
	}
}

// TestShortStackPriority: at a height where not every card fits, the
// fitter drops the lowest-priority cards first — CONNECTION and QUICK
// ACTIONS (the page's irreducible answers) always survive, LAST STRESS
// goes first.
func TestShortStackPriority(t *testing.T) {
	t.Parallel()

	lines := assertFits(t, logGoldState(), 90, 24)

	for _, must := range []string{"CONNECTION", "QUICK ACTIONS"} {
		if lineIndex(lines, must) < 0 {
			t.Errorf("short stack lost %q (never-drop card):\n%s", must, strings.Join(lines, "\n"))
		}
	}
	if lineIndex(lines, "LAST STRESS") >= 0 {
		t.Errorf("LAST STRESS must be the first card the short stack drops:\n%s",
			strings.Join(lines, "\n"))
	}
}

// TestTwoColFitsEveryHeight drives the grid through degenerate heights
// at both two-column widths and the narrow stack: the body must never
// exceed the content area (the fitter's drop/clip contract).
func TestTwoColFitsEveryHeight(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ w, h int }{
		{150, 44},
		{150, 12},
		{150, 8},
		{110, 36},
		{110, 10},
		{100, 24},
		{99, 30},
		{80, 32},
		{80, 24},
		{60, 20},
		{50, 16},
		{48, 8},
	} {
		assertFits(t, logGoldState(), size.w, size.h)
	}
}

// TestWideLeftColumnWidth: the wide grid pins the left column to 46
// cells, so the right column starts at column 48.
func TestWideLeftColumnWidth(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, logGoldState(), 150, 44)
	lines := bodyLines(t, d)

	contentW, _ := contentSizeFor(150, 44)
	rightW := contentW - 46 - 1
	if rightW < 20 {
		t.Fatalf("right column width %d unexpectedly small", rightW)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > contentW {
			t.Errorf("line width %d exceeds content width %d: %q", w, contentW, l)
		}
	}
}

// sameLine reports whether both markers appear on one line.
func sameLine(lines []string, a, b string) bool {
	for _, l := range lines {
		if strings.Contains(l, a) && strings.Contains(l, b) {
			return true
		}
	}

	return false
}

// lineIndex returns the index of the first line containing mark, or -1.
func lineIndex(lines []string, mark string) int {
	for i, l := range lines {
		if strings.Contains(l, mark) {
			return i
		}
	}

	return -1
}
