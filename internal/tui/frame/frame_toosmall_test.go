package frame

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestTooSmallState asserts the sub-MinWidth contract at 47/40/10 cols:
// truthful "terminal too small" state, no header/footer chrome, exact line
// count, no panic, and the message survives the narrowest truncation
// without layout corruption.
func TestTooSmallState(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	for _, width := range []int{MinWidth - 1, 40, 10} {
		out := Render(matrixProps(th, width))
		lines := strings.Split(out, "\n")

		if len(lines) != 24 {
			t.Fatalf("width %d: got %d lines, want 24", width, len(lines))
		}
		if width >= 24 {
			if !strings.Contains(out, "terminal too small") {
				t.Fatalf("width %d: no too-small state:\n%s", width, out)
			}
			if !strings.Contains(out, strconv.Itoa(MinWidth)) {
				t.Errorf("width %d: state does not name the minimum", width)
			}
		}
		if !strings.HasPrefix(lines[0], "[x] termin") {
			t.Errorf("width %d: first line %q is not the error state", width, lines[0])
		}
		if strings.Contains(out, "jiso 2.0.0") || strings.Contains(out, "quit") {
			t.Errorf("width %d: chrome leaked into too-small frame:\n%s", width, out)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > width {
				t.Fatalf("width %d: line %q exceeds the terminal", width, l)
			}
		}
	}
}

// TestTooSmallHeightFloors asserts no panic and >=1 line at absurd heights.
func TestTooSmallHeightFloors(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	for _, height := range []int{0, 1, 2} {
		p := matrixProps(th, 40)
		p.Height = height
		out := Render(p)
		if out == "" {
			t.Fatalf("height %d: empty frame", height)
		}
	}
}
