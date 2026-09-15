// hitmap_footer.go is the Task 8.4 leg of the hit map (UAT round 8 finding
// 9): clicking a footer legend cell fires its action. The frame owns the
// geometry (frame.FooterHits / frame.FooterOrigin); this file only wires
// the packed entries into the root's cell→key map. The tests are in
// hitmap_footer_test.go; the resolve/cmd machinery is pinned in
// hitmap_test.go.
package tui

import (
	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

// registerFooterHits adds one ABSOLUTE key hit per VISIBLE footer entry
// (Task 8.4, finding 9): frame.FooterHits replays the exact packing Render
// just drew, so every rect sits on the footer row at the cell the entry's
// text occupies — entries the width pressure dropped (the "…+N" tail) have
// no rect and stay inert, and a label that spells no single key ("j/k") is
// filtered by synthKeyPress rather than firing a wrong press. A resolved
// cell replays its dispatch key through updateKey, so with a modal owning
// the keyboard the key reaches the MODAL (root_keys.go's modal chain) —
// the footer adds no modalOpen() swallow, because that would wrongly keep
// the key from the overlay that must receive it.
//
// Limitation (Task 8.4, pinned for future authors): synthKeyPress spells
// single runes and named UNMODIFIED special keys (its one chord is
// backtab's shift) — it cannot spell ctrl/alt chords. A future footer
// entry dispatched as e.g. "ctrl+r" therefore renders but stays
// click-inert through this filter, never a wrong press; making chords
// clickable means teaching synthKeyPress to decode a modifier prefix
// into a tea.KeyMod, not loosening the !ok filter below.
func (m *RootModel) registerFooterHits(hm *hitMap) {
	for _, hh := range frame.FooterHits(m.themeOrNil(), m.footerHints(), m.width, m.height) {
		if _, ok := synthKeyPress(hh.Dispatch); !ok {
			continue // display-only legend: the cell stays inert
		}
		hm.add(geom.Rect{X: hh.X, Y: hh.Y, W: hh.W, H: 1}, keyHit(hh.Dispatch))
	}
}
