package frame

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// Border glyph sets. The rounded set is the wireframe's; the ASCII set
// (th.ASCII) keeps the frame legible on terminals without Unicode.
type borderGlyphs struct {
	topLeft, topRight       string
	midLeft, midRight       string
	bottomLeft, bottomRight string
	side, dash              string
}

var (
	roundedBorder = borderGlyphs{"┌", "┐", "├", "┤", "└", "┘", "│", "─"}
	asciiBorder   = borderGlyphs{"+", "+", "+", "+", "+", "+", "|", "-"}
)

func borders(th *theme.Theme) borderGlyphs {
	if th.ASCII {
		return asciiBorder
	}

	return roundedBorder
}

func topRuleLeft(th *theme.Theme) string  { return borders(th).topLeft + dashGlyph(th) }
func topRuleRight(th *theme.Theme) string { return borders(th).topRight }
func midLeft(th *theme.Theme) string      { return borders(th).midLeft }
func midRight(th *theme.Theme) string     { return borders(th).midRight }
func bottomLeft(th *theme.Theme) string   { return borders(th).bottomLeft }
func bottomRight(th *theme.Theme) string  { return borders(th).bottomRight }
func dashGlyph(th *theme.Theme) string    { return borders(th).dash }

// ruleBorder styles a rule string with the subtle border foreground —
// identity render under a colorless profile.
func ruleBorder(th *theme.Theme, s string) string {
	return lipgloss.NewStyle().Foreground(th.SubtleBorder.GetBorderTopForeground()).Render(s)
}

// ruleLine renders a full-width horizontal rule: left + dashes + right.
func ruleLine(th *theme.Theme, left, right string, width int) string {
	return ruleBorder(th, left+strings.Repeat(dashGlyph(th), max(width-runeWidth(left)-runeWidth(right), 0))+right)
}

// wrapRow puts one already-fitted line inside the side rules: side + " "
// + line + padding + side, totalling exactly width cells.
func wrapRow(th *theme.Theme, line string, width int) []string {
	const gap = 1 // one space after the left side rule
	between := width - 2
	if line == "" {
		return []string{ruleBorder(th, sideGlyph(th)) + strings.Repeat(" ", between) + ruleBorder(th, sideGlyph(th))}
	}
	pad := max(between-gap-lipgloss.Width(line), 0)

	return []string{ruleBorder(th, sideGlyph(th)) + " " + line + strings.Repeat(" ", pad) + ruleBorder(th, sideGlyph(th))}
}

// wrapRows wraps every content row into the side rules.
func wrapRows(th *theme.Theme, rows []string, width int) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, wrapRow(th, r, width)...)
	}

	return out
}

func sideGlyph(th *theme.Theme) string { return borders(th).side }

// runeWidth is the display width of a plain (unstyled) rule glyph run.
func runeWidth(s string) int { return lipgloss.Width(s) }

// itoaFrame formats a small non-negative int (frame stays strconv-light
// like palette).
func itoaFrame(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}
