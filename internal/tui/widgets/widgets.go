// Package widgets holds the reusable, state-in/state-out widgets the M5
// screens are assembled from (TUI-406): a virtualized List, a truncating
// sortable Table, and a non-blocking ConfirmDialog.
//
// Contract for everything here:
//   - Pure: Update consumes a tea.Msg and returns state + a tea.Cmd; no
//     I/O, no blocking, no os.Exit. Tests drive Update with literal
//     messages and assert on View() strings — no running program needed.
//   - Theme-driven: all colour/style comes from internal/tui/theme token
//     styles, unmodified (deriving attributes like Bold would re-introduce
//     escape codes under a colorless profile). Widgets define no new
//     colors; glyphs that theme does not own are constants below with
//     explicit ASCII fallbacks selected by theme.ASCII.
//   - Leaf-level: widgets never import internal/tui/frame, internal/cli,
//     internal/command, or cobra (enforced by imports_guard_test.go).
//     Frame composes widgets, not the other way round.
//
// Why not bubbles/v2 components: bubbles' list hardcodes its "No items"
// empty text (the ticket requires a configurable, theme-styled empty
// state), drags in filtering/pagination/help surface the screens do not
// want, and its delegate API styles rows through io.Writer instead of
// theme tokens; bubbles' table has no sort indicator, no virtualization,
// and no truncate-never-wrap guarantee. All three widgets are therefore
// hand-rolled on top of tea + lipgloss + x/ansi, keeping the public
// surface as small as the ticket allows.
package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// Glyphs owned by widgets (theme owns the status/selector set). Each has
// an ASCII fallback used when theme.ASCII is on.
// Glyphs owned by widgets: the sort markers. The ellipsis is NOT here -- the
// theme owns that glyph (Theme.Ellipsis), and a second copy in this package is
// how two ASCII spellings of the same marker ("~" and "...") got into the TUI.
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

// LabelWithHint lays out one selectable list row: the label padded to column
// cells, with the hotkey badge after it. Without it every badge trails its own
// title at a different cell ("Disconnect  (c)", "Send transaction  (s)",
// "View last send"), so the keys the operator is scanning for do not line up.
// Padding is computed on display width, so styled spans and wide runes cannot
// shift the column. An empty badge returns the label untouched.
func LabelWithHint(th *theme.Theme, label, badge string, column int) string {
	if badge == "" {
		return label
	}

	pad := column - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}

	return label + strings.Repeat(" ", pad) + th.HotKey.Render("("+badge+")")
}

// HintColumn is the label column width that makes a list's badges line up: the
// widest label in the list. A filterable list passes the whole registry rather
// than the filtered subset, so the column does not move while the operator types.
func HintColumn(labels []string) int {
	w := 0
	for _, l := range labels {
		if n := lipgloss.Width(l); n > w {
			w = n
		}
	}

	return w
}
