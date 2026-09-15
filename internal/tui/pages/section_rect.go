// section_rect.go defines how pages record their section Rects (Task 6.1
// review, finding F2): the DRAWN box measured from the composed string,
// never the requested box. section_rect_test.go pins every recorded
// origin onto the drawn border ink so Phase 8's hit-map inherits
// known-good geometry.
package pages

import (
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/geom"
)

// sectionRect measures the drawn ink of one rendered section string and
// returns the Rect it occupies at the content-relative origin (x,y).
//
// The recorded Rect is the DRAWN box, not the requested one: a
// ModeServer section draws w-2 cells wide, a border-only pane (the §F
// list, the §K records box) draws w-2 too, and a stacked box can draw a
// line shorter than its nominal height. Stacked/joined origins therefore
// advance by the measured extents (lipgloss.Width/Height of the composed
// strings — exactly what strings.Join and JoinHorizontal consume), not by
// nominal w/h plus gap terms; Section.Render's returned Rect stays the
// requested layout grid (pinned in widgets), and pages record this one.
func sectionRect(x, y int, out string) geom.Rect {
	return geom.Rect{X: x, Y: y, W: lipgloss.Width(out), H: lipgloss.Height(out)}
}
