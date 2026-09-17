// Package widgets holds the reusable, state-in/state-out widgets the
// screens are assembled from: a virtualized List, a truncating sortable
// Table, and a non-blocking ConfirmDialog.
// Contract: Update is pure (a tea.Msg in, state + a tea.Cmd out, no
// I/O); all colour/style comes from internal/tui/theme token styles
// unmodified; glyphs theme does not own are constants below with ASCII
// fallbacks picked by theme.ASCII; the package never imports frame, cli,
// or cobra.
package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// Glyphs owned by widgets: the sort markers only (theme owns the
// status/selector set and the ellipsis — a local copy drifts). Each has
// an ASCII fallback used when theme.ASCII is on.
const (
	// GlyphSortAsc/GlyphSortDesc mark the sorted column ("▲"/"▼");
	// ASCII fallbacks "^"/"v".
	GlyphSortAsc  = "▲"
	GlyphSortDesc = "▼"
	ASCIISortAsc  = "^"
	ASCIISortDesc = "v"
)

// DefaultEmptyMessage is the empty-state line unless a widget overrides it.
const DefaultEmptyMessage = "no items"

// truncateTail returns the ellipsis glyph for th's glyph mode.
func truncateTail(th *theme.Theme) string { return th.Ellipsis() }

// sortGlyph returns the sort indicator for th's glyph mode.
func sortGlyph(th *theme.Theme, asc bool) string {
	switch {
	case th.ASCII && asc:
		return ASCIISortAsc
	case th.ASCII:
		return ASCIISortDesc
	case asc:
		return GlyphSortAsc
	default:
		return GlyphSortDesc
	}
}

// pad right-pads s with spaces to display width w (no-op when s is wider).
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// clip ANSI-safely truncates s to w cells (never wraps; newlines inside s
// are flattened first so a hostile cell cannot break the row).
func clip(s string, w int, tail string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, tail)
}

// LabelWithHint lays out one selectable list row: the label padded to
// column cells, with the hotkey badge after it, so badges line up across
// rows. Padding uses display width, so styled spans cannot shift it; an
// empty badge returns the label untouched.
func LabelWithHint(th *theme.Theme, label, badge string, column int) string {
	if badge == "" {
		return label
	}

	pad := column - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}

	return label + strings.Repeat(" ", pad) + th.Key("("+badge+")")
}

// HintColumn is the label column width that lines up a list's badges: the
// widest label. A filterable list passes the whole registry so the column
// does not move while the operator types.
func HintColumn(labels []string) int {
	w := 0
	for _, l := range labels {
		if n := lipgloss.Width(l); n > w {
			w = n
		}
	}

	return w
}
