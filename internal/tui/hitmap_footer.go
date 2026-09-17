// hitmap_footer.go is the footer leg of the hit map: clicking a footer legend
// cell fires its action. The frame owns the geometry (frame.FooterHits); this
// file only wires the packed entries into the root's cell→key map.
package tui

import (
	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

// registerFooterHits adds one ABSOLUTE key hit per VISIBLE footer entry: the
// key replays through updateKey, so it reaches the modal that owns the
// keyboard (no modalOpen swallow here). Entries the packing dropped have no
// rect, and ctrl/alt chords are unspellable by synthKeyPress — those cells
// stay click-inert, never a wrong press.
func (m *RootModel) registerFooterHits(hm *hitMap) {
	for _, hh := range frame.FooterHits(m.themeOrNil(), m.footerHints(), m.width, m.height) {
		if _, ok := synthKeyPress(hh.Dispatch); !ok {
			continue // display-only legend: the cell stays inert
		}
		hm.add(geom.Rect{X: hh.X, Y: hh.Y, W: hh.W, H: 1}, keyHit(hh.Dispatch))
	}
}
