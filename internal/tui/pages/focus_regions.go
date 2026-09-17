// focus_regions.go is the click-to-focus region seam: a page that draws a
// focusable ROW publishes its DRAWN rect under a stable region id, and
// root applies the resolved focus with the step navigation the owner
// already exposes — so a click passes the same gates as the keys (forward
// gated, backward free). Same optional-interface pattern as Scroller;
// centered modal overlays instead measure their own rows with
// FieldRowHits/RailRowHits, since root centers those boxes and owns their
// input routing.
package pages

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

// RegionAnalyzeRail names the §J wizard step rail's click-focus regions
// ("<page>:<pane>", like every region id): a click on a step label routes
// through root's own step-delta path.
const RegionAnalyzeRail = "analyze:rail"

// FocusRegion is one click-focusable ROW a view drew during its last
// render: the row's DRAWN rect (content-relative, the SelectRegion
// doctrine — hit = drawn ink) and the field/step index a click focuses.
type FocusRegion struct {
	ID    string
	Rect  geom.Rect
	Index int
}

// Focuser is implemented by pages that publish click-focus rects. Root
// registers them over the page body (registration order is z-order) and
// routes the resolved focus to the owner's own navigation; unknown regions
// stay inert — a mouse msg never reaches a page as a key.
type Focuser interface {
	// FocusRegions returns the drawn focus rects of the last render, in
	// any order.
	FocusRegions() []FocusRegion
}

var _ Focuser = (*Analyze)(nil)

// FocusRegions publishes the step rail's labels as click targets on the
// rail line: the exact spans railLine draws, clipped to the width it clips
// to (hit = drawn ink). While an overlay owns the keyboard the rail behind
// it publishes nothing and clicks there stay inert.
func (a *Analyze) FocusRegions() []FocusRegion {
	if (a.itemsOpen && len(a.state.Items) > 0) ||
		(a.unparsableOpen && len(a.state.UnparsableRows) > 0) {
		return nil
	}
	w, _ := frame.ContentSize(a.width, a.height)

	spans := railSpans(titleLine(a.th, titleAnalyze), StepNames, a.pick(" \u25b8 ", " > "), w)
	out := make([]FocusRegion, 0, len(spans))
	for _, s := range spans {
		out = append(out, FocusRegion{
			ID:    RegionAnalyzeRail,
			Rect:  geom.Rect{X: s.X, Y: 0, W: s.W, H: 1},
			Index: s.Index,
		})
	}

	return out
}

// railSpan is one drawn step label of a wizard rail: start cell, drawn
// width, step index.
type railSpan struct {
	X, W, Index int
}

// railSpans lays out a wizard rail's "<n> <name>" labels (optionally
// title-prefixed, joined by sep) and reports each step's drawn span. A
// label starting at or beyond clipX drew no ink and publishes no span; one
// cut by the clip publishes only its visible cells.
func railSpans(title string, names []string, sep string, clipX int) []railSpan {
	x := 0
	if title != "" {
		x = lipgloss.Width(title) + 2 // the two-space join between title and labels
	}
	sepW := lipgloss.Width(sep)

	out := make([]railSpan, 0, len(names))
	for i, name := range names {
		label := strconv.Itoa(i+1) + " " + name
		lw := lipgloss.Width(label)
		if x >= clipX {
			break
		}
		out = append(out, railSpan{X: x, W: min(lw, clipX-x), Index: i})
		x += lw + sepW
	}

	return out
}
