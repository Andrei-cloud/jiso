package frame

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// asciiTheme pins the colorless profile so matrix assertions see raw text
// (escapes would break substring checks only in spirit, but ASCII also
// selects the "-" rule glyphs and "+" corners we assert on).
func asciiTheme(t *testing.T) *theme.Theme {
	t.Helper()

	return theme.NewWith(colorprofile.ASCII, true)
}

// matrixProps is one fixture whose long chip set forces top-rule chip
// elision below the full breakpoint.
func matrixProps(th *theme.Theme, width int) Props {
	return Props{
		Theme: th, Width: width, Height: 24,
		App: "jiso", Version: "2.0.0",
		Conn:    Segment{Kind: theme.KindError, Text: "offline"},
		Target:  "10.0.0.5:8080",
		Spec:    "visa-purchase.json",
		TxFile:  "pool.json",
		TxCount: 12,
		Workers: 3,
		Hints: []KeyHint{
			{Key: "j/k", Desc: "move"},
			{Key: "enter", Desc: "open", Primary: true},
			{Key: ":", Desc: "cmd", Primary: true},
			{Key: "?", Desc: "help", Primary: true},
			{Key: "q", Desc: "quit", Primary: true},
		},
		Content: "page body\nsecond line",
	}
}

// TestFooterOverflowMarker: UAT round 5 — whatever the width pressure
// hides must be COUNTED: the narrow footer carries a dim "~+N" tail (a
// narrow terminal used to drop the right-most keys silently, stranding
// the user). The surviving primaries stay; the hidden entry's text
// does not.
func TestFooterOverflowMarker(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	wide := Render(matrixProps(th, 120))
	if strings.Contains(wide, "+1") {
		t.Errorf("a footer with room must show no overflow marker:\n%s", wide)
	}
	if !strings.Contains(wide, "j/k") {
		t.Error("wide footer must keep the non-primary hint")
	}

	narrow := Render(matrixProps(th, 60))
	if strings.Contains(narrow, "j/k") {
		t.Error("narrow footer must still drop the non-primary hint")
	}
	if !strings.Contains(narrow, "~+1") {
		t.Errorf("narrow footer must count the dropped entry with ~+1:\n%s", narrow)
	}
	for _, want := range []string{": cmd", "? help", "q quit"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("narrow footer lost primary hint %q:\n%s", want, narrow)
		}
	}
}

// TestResponsiveMatrix asserts chrome presence/absence per the contract
// breakpoints at 120/90/70/60 columns.
func TestResponsiveMatrix(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	cases := []struct {
		name  string
		width int
		// expectations
		wantTopText  bool // top rule carries the app label
		wantAllChips bool // full chip set in the top rule
		wantConnChip bool // target+state chip survives
		wantAllHints bool // footer keeps non-primary hints
	}{
		{"w120-full", 120, true, true, true, true},
		{"w90-medium", 90, true, false, true, true},
		// Narrow keeps the identity label + connection chip (UAT round 6
		// QA), drops the informational chips and the non-primary hints.
		{"w70-narrow", 70, true, false, true, false},
		{"w60-narrow", 60, true, false, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Render(matrixProps(th, tc.width))
			lines := strings.Split(out, "\n")

			if len(lines) != 24 {
				t.Fatalf("height: got %d lines, want 24", len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w != tc.width {
					t.Errorf("line %d width %d, want %d: %q", i, w, tc.width, l)
				}
			}

			top := lines[0]
			if got := strings.Contains(top, "jiso 2.0.0"); got != tc.wantTopText {
				t.Errorf("top rule label presence %v, want %v: %q", got, tc.wantTopText, top)
			}
			if tc.wantTopText {
				if !strings.HasPrefix(top, "+-") || !strings.HasSuffix(top, "+") {
					t.Errorf("top rule corners: %q", top)
				}
			}
			if got := strings.Contains(top, "pool.json (12)"); got != tc.wantAllChips {
				t.Errorf("tx chip presence %v, want %v: %q", got, tc.wantAllChips, top)
			}
			if got := strings.Contains(top, "10.0.0.5:8080"); got != tc.wantConnChip {
				t.Errorf("conn chip presence %v, want %v: %q", got, tc.wantConnChip, top)
			}
			if !tc.wantAllChips && tc.wantTopText && !strings.Contains(top, "offline") {
				t.Errorf("elided top rule must keep the conn chip: %q", top)
			}

			// Content starts right after the top rule.
			if !strings.Contains(lines[1], "page body") {
				t.Errorf("content must start after the top rule, got %q", lines[1])
			}

			footer := lines[len(lines)-2]
			if !strings.Contains(footer, "│") && !strings.Contains(footer, "|") {
				t.Errorf("footer must sit inside the side rules: %q", footer)
			}
			if tc.wantAllHints && !strings.Contains(footer, "j/k move") {
				t.Errorf("footer lost non-primary hint: %q", footer)
			}
			if !tc.wantAllHints {
				if strings.Contains(footer, "j/k") {
					t.Errorf("narrow footer must keep primary keys only: %q", footer)
				}
				if !strings.Contains(footer, "q quit") {
					t.Errorf("narrow footer lost a primary key: %q", footer)
				}
			}
			if bottom := lines[len(lines)-1]; !strings.HasPrefix(bottom, "+") || !strings.HasSuffix(bottom, "+") {
				t.Errorf("bottom rule corners: %q", bottom)
			}
		})
	}
}

// TestAsciiRenderNoEscapes pins contract §Accessibility: under the ascii
// profile the frame contains zero escape codes and only ASCII rules.
func TestAsciiRenderNoEscapes(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	for _, width := range []int{120, 90, 70, 60} {
		out := Render(matrixProps(th, width))
		if strings.ContainsAny(out, "\x1b\u009b") {
			t.Errorf("width %d: escape codes in ascii render", width)
		}
		for _, l := range strings.Split(out, "\n") {
			for _, r := range l {
				if r > 127 {
					t.Errorf("width %d: non-ASCII rune %q in %q", width, r, l)
					break
				}
			}
		}
	}
}

// TestHeightFloors walks the height ladder: chrome degrades in order
// (top rule → footer pair → bottom rule) and the content floor is honoured
// with no panic and no line taller than the window.
func TestHeightFloors(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	cases := []struct {
		height      int
		wantLines   int
		wantTop     bool // top rule text present
		wantContent bool
		wantFooter  bool
	}{
		{24, 24, true, true, true},
		{16, 16, true, true, true},
		{5, 5, true, true, true},   // top rule still fits (1 content line)
		{4, 4, false, true, true},  // top rule dropped first
		{3, 3, false, true, false}, // footer pair yields
		{2, 2, false, true, false},
		{1, 1, false, true, false}, // bottom rule drops last
		{0, 24, true, true, true},  // fallback 80×24
	}

	for _, tc := range cases {
		p := matrixProps(th, 120)
		p.Height = tc.height
		out := Render(p)
		lines := strings.Split(out, "\n")

		if len(lines) != tc.wantLines {
			t.Fatalf("height %d: got %d lines, want %d", tc.height, len(lines), tc.wantLines)
		}
		if got := strings.Contains(out, "jiso 2.0.0"); got != tc.wantTop {
			t.Errorf("height %d: top rule text presence %v, want %v", tc.height, got, tc.wantTop)
		}
		if got := strings.Contains(out, "page body"); got != tc.wantContent {
			t.Errorf("height %d: content presence %v, want %v", tc.height, got, tc.wantContent)
		}
		if got := strings.Contains(out, "q quit"); got != tc.wantFooter {
			t.Errorf("height %d: footer presence %v, want %v", tc.height, got, tc.wantFooter)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > 120 {
				t.Errorf("height %d line %d width %d exceeds window: %q", tc.height, i, w, l)
			}
		}
	}
}

// TestContentOverflowTruncatesNotWraps: an overlong page body is clipped to
// the content area (tables truncate, never wrap).
func TestContentOverflowTruncatesNotWraps(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	p := matrixProps(th, 120)
	p.Height = 10 // content area = 10 - 4 chrome = 6 lines
	p.Content = strings.Repeat("x", 500) + "\n" +
		strings.Repeat("tail\n", 40) + "ENDLINE"

	out := Render(p)
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 120 {
			t.Errorf("overflowed line width %d: %q", w, l)
		}
	}
	if strings.Contains(out, "ENDLINE") {
		t.Error("content beyond the content height must be clipped")
	}
	if !strings.Contains(out, "tail") {
		t.Error("content inside the content height must survive")
	}
}
