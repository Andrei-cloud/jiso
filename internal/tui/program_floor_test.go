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
