package frame

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// renderSegment styles one Segment: status symbol+text+colour, or muted
// plain text when Plain.
func renderSegment(th *theme.Theme, s Segment) string {
	if s.Text == "" {
		return ""
	}
	if s.Plain {
		return th.Deemphasized.Render(s.Text)
	}
	return th.Status(s.Kind, s.Text)
}

// clipStyle truncates without adding any styling of its own (no colour,
// no padding) — it only enforces the width budget.
var clipStyle = lipgloss.NewStyle().Inline(true)

// truncate clamps a styled string to width cells, ANSI-aware, no padding.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return clipStyle.MaxWidth(max(width, 0)).Render(s)
}

// fitContent splits the page body, truncates each line to width, and pads
// (or clips) to exactly height lines.
func fitContent(content string, width, height int) []string {
	src := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(src) {
			out = append(out, truncate(src[i], width))
		} else {
			out = append(out, "")
		}
	}
	return out
}
