// section_rect.go defines how pages record their section Rects: the DRAWN
// box measured from the composed string, never the requested box.
package pages

import (
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/geom"
)

// sectionRect measures the DRAWN ink of a rendered section string and
// returns its Rect at the content-relative origin (x,y). The recorded Rect
// is the drawn box, not the requested one (some modes draw narrower and
// stacked boxes can draw short), so stacked/joined origins advance by the
// measured extents — exactly what JoinHorizontal consumes — not by nominal
// w/h plus gap terms.
func sectionRect(x, y int, out string) geom.Rect {
	return geom.Rect{X: x, Y: y, W: lipgloss.Width(out), H: lipgloss.Height(out)}
}
