package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Frame integration through RootModel.View: the four-section
// chrome must track resizes and page changes with no stale layout.

// mustRoot names the model Update returned: the compiler only sees
// tea.Model, so a non-root return must fail with its type, not panic.
func mustRoot(t *testing.T, m tea.Model) *RootModel {
	t.Helper()

	rm, ok := m.(*RootModel)
	if !ok {
		t.Fatalf("Update = %T, want *RootModel", m)
	}

	return rm
}

// TestResizeDuringRunNoStaleLayout drives the model through a
// tea.WindowSizeMsg sequence and asserts every View matches the *current*
// window: exact height, no line wider than the window, and the header level
// appropriate to the width (never the previous size's layout).
func TestResizeDuringRunNoStaleLayout(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	sizes := []struct{ w, h int }{{120, 32}, {90, 24}, {70, 24}, {60, 20}, {120, 32}}

	for _, s := range sizes {
		next, cmd := m.Update(tea.WindowSizeMsg{Width: s.w, Height: s.h})
		m = mustRoot(t, next)
		// Settle the resize coalescer: the trailing flush relayouts the
		// stack with the latest size (the program runs it ~32 ms later;
		// a mid-burst View legitimately still holds the previous
		// layout — the frame would clip the newest rows of the
		// priority stack, so the assertions need the settled
		// layout).
		queue := flattenMsgs(cmd)
		for i := 0; i < 8 && len(queue) > 0; i++ {
			next, cmd = m.Update(queue[0])
			m = mustRoot(t, next)
			queue = append(queue[1:], flattenMsgs(cmd)...)
		}

		content := m.View().Content
		lines := strings.Split(content, "\n")
		if len(lines) != s.h {
			t.Fatalf("%dx%d: view is %d lines", s.w, s.h, len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > s.w {
				t.Errorf("%dx%d line %d: width %d exceeds window: %q", s.w, s.h, i, w, l)
			}
		}

		switch {
		case s.w >= 80: // top rule embeds the label AND the conn chip
			if !strings.Contains(lines[0], "jiso") {
				t.Errorf("%dx%d: top rule lost the label: %q", s.w, s.h, lines[0])
			}
			if !strings.Contains(lines[0], "offline") {
				t.Errorf("%dx%d: top rule must carry the conn chip: %q", s.w, s.h, lines[0])
			}
		default: // narrow: top rule keeps label + conn chip, footer primary keys only
			if !strings.Contains(lines[0], "jiso") {
				t.Errorf("%dx%d: narrow top rule must keep the identity label: %q", s.w, s.h, lines[0])
			}
			if !strings.Contains(lines[0], "offline") {
				t.Errorf("%dx%d: narrow top rule must keep the conn chip: %q", s.w, s.h, lines[0])
			}
		}

		if !strings.Contains(content, "QUICK ACTIONS") {
			t.Errorf("%dx%d: dashboard page body missing", s.w, s.h)
		}
		if !strings.Contains(content, "q quit") {
			t.Errorf("%dx%d: global footer hint missing", s.w, s.h)
		}
	}
}

// TestFooterHintsFollowCurrentPage proves the footer is contextual: page
// hints swap with the stack top and palette mode flips the header mode chip.
func TestFooterHintsFollowCurrentPage(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if v := m.View().Content; !strings.Contains(v, "enter run") || strings.Contains(v, "esc close") {
		t.Fatalf("dashboard page footer wrong:\n%s", v)
	}

	// '?' opens the §M overlay (mode chip "help", HELP box);
	// esc closes it and the page footer returns.
	_, _ = m.Update(ch('?'))
	if v := m.View().Content; !strings.Contains(v, "HELP") || !strings.Contains(v, "context: Dashboard page") {
		t.Fatalf("help overlay must render with its context label:\n%s", v)
	}

	_, _ = m.Update(special(tea.KeyEscape))
	if v := m.View().Content; !strings.Contains(v, "enter run") {
		t.Fatalf("footer must return to the page after esc:\n%s", v)
	}

	_, _ = m.Update(ch(':'))
	v := m.View().Content
	if !strings.Contains(v, "matches") {
		t.Fatalf("palette panel must replace the content body while open:\n%s", v)
	}
}
