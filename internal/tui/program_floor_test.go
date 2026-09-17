package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Layer-2 floor tests (TUI-407): real programs at degraded sizes. The
// frame must shed chrome per the responsive contract and never panic or
// emit a line wider than the terminal (layout corruption).

// assertNoCorruption: exact line count, every line within the width.
func assertNoCorruption(t *testing.T, r progResult, width, height int) {
	t.Helper()

	// finalFrame trims trailing blank lines, so the frame may be shorter
	// than the terminal; it must never be taller (that is corruption).
	lines := strings.Split(r.frame, "\n")
	if len(lines) > height {
		t.Fatalf("frame is %d lines, exceeds %d-row terminal:\n%s", len(lines), height, r.frame)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > width {
			t.Fatalf("line %d (%d cells) exceeds %d-col terminal: %q", i+1, w, width, l)
		}
	}
}

// frameBoxOpens reports a glyph that opens a section box at the content area's left edge.
func frameBoxOpens(r rune) bool {
	switch r {
	case '+', '|', '│', '┌', '└', '├', '╭', '╰':
		return true
	}

	return false
}

// assertSectionsFillFrameWidth: every line that opens a section box at the
// content left edge must close with box ink one cell inside the right rule;
// at least one box line must be seen (no vacuous pass).
func assertSectionsFillFrameWidth(t *testing.T, r progResult, width int) {
	t.Helper()

	lines := strings.Split(strings.TrimRight(r.frame, "\n"), "\n")
	boxes := 0

	for i, l := range lines {
		runes := []rune(l)
		if len(runes) != width || !frameBoxOpens(runes[2]) {
			continue // assertNoCorruption owns the exact-width contract
		}

		boxes++
		if edge := runes[width-3]; !strings.ContainsRune("+|│┐┘┤╮╯", edge) {
			t.Errorf("frame line %d: a box opens the content area but the line ends with %q one cell inside the right rule — trailing gap: %q",
				i+1, edge, l)
		}
	}

	if boxes == 0 {
		t.Errorf("frame shows no section box at %d cols — nothing to check:\n%s", width, r.frame)
	}
}

// progFillRun runs one real program at (width, 24) and applies the width-fill contract.
func progFillRun(t *testing.T, width int, keys string) progResult {
	t.Helper()

	s := newProgSession(t, width, 24)
	r := s.run(t, keys+"\x03")
	wantClean(t, r)
	assertNoCorruption(t, r, width, 24)

	return r
}

// progFillPages are the sectioned pages the width-fill contract pins.
var progFillPages = []struct {
	name string
	keys string
}{
	{"dash", ""},
	{"server", "4"},
	{"sessions", "6"},
}

func TestProgFloor80Cols(t *testing.T) {
	// [1:]: the §A dashboard at 80 is pinned by TestProgBootGolden
	for _, p := range progFillPages[1:] {
		t.Run(p.name, func(t *testing.T) {
			r := progFillRun(t, 80, p.keys)
			assertSectionsFillFrameWidth(t, r, 80)
			checkProgGolden(t, "floor_80_"+p.name, r.frame)
		})
	}
}

func TestProgFloor120Cols(t *testing.T) {
	for _, p := range progFillPages {
		t.Run(p.name, func(t *testing.T) {
			r := progFillRun(t, 120, p.keys)
			assertSectionsFillFrameWidth(t, r, 120)
			checkProgGolden(t, "floor_120_"+p.name, r.frame)
		})
	}
}

func TestProgFloor200Cols(t *testing.T) {
	for _, p := range progFillPages {
		t.Run(p.name, func(t *testing.T) {
			r := progFillRun(t, 200, p.keys)
			assertSectionsFillFrameWidth(t, r, 200)
			checkProgGolden(t, "floor_200_"+p.name, r.frame)
		})
	}
}

// TestProgFloor60Cols: at 60 cols (< NarrowWidth=80) the header drops
// entirely while page content, hard-status, and footer survive.
func TestProgFloor60Cols(t *testing.T) {
	s := newProgSession(t, 60, 24)
	r := s.run(t, "\x03")
	wantClean(t, r)

	if strings.HasPrefix(r.frame, "jiso dev") {
		t.Errorf("60-col frame still has the header:\n%s", r.frame)
	}
	if !strings.Contains(r.frame, "CONNECTION") {
		t.Errorf("60-col frame lost the page title:\n%s", r.frame)
	}
	// UAT round 6 QA: below NarrowWidth the top rule keeps the identity
	// label (only the informational chips drop, and the invented
	// hard-status clock is gone with the old header).
	if !strings.Contains(r.frame, "jiso") {
		t.Errorf("60-col top rule must keep the app label:\n%s", r.frame)
	}
	assertNoCorruption(t, r, 60, 24)
	assertSectionsFillFrameWidth(t, r, 60)
	checkProgGolden(t, "floor_60", r.frame)
}

// TestProgFloor40Cols: below frame.MinWidth the truthful "terminal too
// small" state replaces every section — no page body, no chrome.
func TestProgFloor40Cols(t *testing.T) {
	s := newProgSession(t, 40, 24)
	r := s.run(t, "\x03")
	wantClean(t, r)

	if !strings.Contains(r.frame, "terminal too small") {
		t.Fatalf("40-col frame lacks the too-small state:\n%s", r.frame)
	}
	if strings.Contains(r.frame, "Status") || strings.Contains(r.frame, "quit") {
		t.Errorf("chrome or page body leaked into the too-small frame:\n%s", r.frame)
	}
	assertNoCorruption(t, r, 40, 24)
	checkProgGolden(t, "floor_40", r.frame)
}
