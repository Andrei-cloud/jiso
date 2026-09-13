package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Decorative glyphs the TUI owns, each with the fallback the ASCII glyph set
// uses. Theme.ASCII selects between them (JISO_ASCII=1, or a terminal that
// cannot do Unicode).
//
// These live here, and only here, because the choice is invisible where it is
// used: a hardcoded "…" renders identically in a truecolor golden and in a
// JISO_ASCII=1 golden, so the mistake only surfaces as a non-ASCII byte in an
// ASCII golden — which TestASCIIGoldensAreSevenBit now fails on. Ask the theme
// for the glyph instead of writing the literal.
const (
	// GlyphEllipsis marks a clipped cell; "~" under the ASCII set.
	GlyphEllipsis = "…"
	// ASCIIEllipsis is the ASCII-profile elision marker.
	ASCIIEllipsis = "~"

	// GlyphSeparator joins spans on one line.
	GlyphSeparator = " · "
	// ASCIISeparator is the ASCII-profile separator. It is a pipe, not a dash:
	// the ASCII fallback for an unknown value is "-", and " - " between two
	// dashes reads as one unreadable run ("ID - - tx - - ok").
	ASCIISeparator = " | "
)

// Ellipsis returns the elision marker for this theme's glyph set. Use it
// wherever a value is clipped, so the marker degrades with the profile.
func (t *Theme) Ellipsis() string {
	if t.ASCII {
		return ASCIIEllipsis
	}

	return GlyphEllipsis
}

// Separator returns the inter-span separator for this theme's glyph set.
// It is unstyled: render the result with the token the span belongs to
// (Deemphasized for body copy, Dim for de-emphasised lines).
func (t *Theme) Separator() string {
	if t.ASCII {
		return ASCIISeparator
	}

	return GlyphSeparator
}

// Truncate clamps one styled line to w cells and marks the cut with
// Ellipsis. It never wraps: a line that does not fit is cut, and the full
// value belongs in a detail view (design contract: tables truncate, never
// wrap). Lines that already fit are returned untouched, and w <= 0 renders
// nothing.
func (t *Theme) Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}

	return ansi.Truncate(s, w, t.Ellipsis())
}

// ElideMiddle shortens an identifier the operator half-recognises, keeping its
// head and tail: "9f3ca1e2b7" becomes "9f3c~a1" under the ASCII set and
// "9f3c…a1" otherwise. Values no longer than head+tail+1 are returned verbatim,
// since eliding them would not save a cell. head and tail count runes.
//
// This is the only mid-elision in the TUI. The three places that spelled it by
// hand disagreed ("9f3c~a1", "9f3c...a1" via a ReplaceAll, and a masked PAN that
// had no ASCII case at all and so wrote a non-ASCII byte into ASCII mode), which
// is exactly the class of mistake the glyph policy is meant to prevent.
func (t *Theme) ElideMiddle(s string, head, tail int) string {
	r := []rune(s)
	if len(r) <= head+tail+1 {
		return s
	}

	return string(r[:head]) + t.Ellipsis() + string(r[len(r)-tail:])
}

// ShortID is the canonical short form of a session id: eight runes or fewer are
// already short and stay verbatim, longer ones keep their first four and last two
// around the ellipsis ("9f3c~a1" in ASCII, "9f3c…a1" otherwise).
//
// It lives next to the glyph it inserts on purpose. The root derives these cells
// and the §I/§K fixtures used to hand-type them, and the fixtures were wrong: they
// showed "77b2..c9" and "31a0..f4", neither of which the root can produce (both ids
// are 8 runes, so they are printed in full), in an elision style nothing emits. One
// owner means a fixture can render the same cell the root would.
func (t *Theme) ShortID(id string) string {
	if len([]rune(id)) <= 8 {
		return id
	}

	return t.ElideMiddle(id, 4, 2)
}

// The ASCII checkbox boxes the checkbox lists draw. The Unicode list uses a
// different marker entirely (see pickGlyph's callers), so these two are the
// ASCII-profile fallback pair: a checked and an unchecked row, each with the
// space that keeps the label aligned whether or not the box is filled.
const (
	// BoxChecked is a ticked row's box.
	BoxChecked = "[x] "
	// BoxUnchecked is an unticked row's box; it is the same width as
	// BoxChecked so the two never shift the label beside them.
	BoxUnchecked = "[ ] "
)
