package frame

import (
	"strconv"
	"strings"

	"jiso/internal/tui/theme"
)

// MinWidth is the hard minimum terminal width (design contract: "below
// hard minimum show a truthful 'terminal too small' state"). Below it no
// chrome or page body renders — the frame degrades entirely to the honest
// message instead of a corrupted layout.
const MinWidth = 48

// tooSmall renders the sub-MinWidth frame: exactly height lines, top two
// carrying the truthful state (error symbol+text line, then the numbers),
// never relying on colour alone for the signal.
func tooSmall(th *theme.Theme, width, height int) string {
	head := th.Status(theme.KindError, "terminal too small")
	detail := th.Deemphasized.Render(
		"width " + strconv.Itoa(width) + " cols < " + strconv.Itoa(MinWidth) +
			" minimum - resize the terminal")

	lines := make([]string, 0, max(height, 1))
	lines = append(lines, truncate(head, width), truncate(detail, width))
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height && height > 0 {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}
