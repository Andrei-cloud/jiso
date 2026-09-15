// focus_regions.go is the click-to-focus region seam (UAT round 8, Task
// 8.5, finding 9): a page that draws a focusable ROW — today the §J
// wizard step rail — publishes the DRAWN rect of that row under a stable
// region id, and the ROOT applies a resolved focusMsg{region, index} with
// the step navigation the owner already exposes (root's
// handleAnalyzeStepDelta for the rail). A page never mutates root-owned
// wizard state, so a click passes the same gates as the keys: forward
// transitions stay gated, backward stays a free revisit. This is the
// optional-interface pattern of Scroller/Selector (scroll_regions.go);
// the centered modal overlays (connect/server forms, send/worker
// wizards) instead measure their own rows with FieldRowHits/RailRowHits
// next to their View code, because the root centers those boxes and owns
// their input routing.
package pages

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

// RegionAnalyzeRail names the §J wizard step rail's click-focus regions
// ("<page>:<pane>" like every other region id): a click on a step label
// routes through root's handleAnalyzeStepDelta, the PgUp/PgDn/Tab path.
const RegionAnalyzeRail = "analyze:rail"

// FocusRegion is one click-focusable ROW a view drew during its last
// render: the row's DRAWN rect (content-relative, the SelectRegion
// doctrine — hit = drawn ink) and the field/step index a click focuses.
type FocusRegion struct {
	ID    string
	Rect  geom.Rect
	Index int
}

// Focuser is implemented by pages that publish click-focus rects. The
// root asserts it per frame after the page's View, registers the rects
// over the page body (registration order is z-order), and routes the
// resolved focusMsg to the region owner's own focus/step navigation;
// unknown regions stay inert: a mouse msg never reaches a page as a key.
type Focuser interface {
	// FocusRegions returns the drawn focus rects of the last render, in
	// any order.
	FocusRegions() []FocusRegion
}

var _ Focuser = (*Analyze)(nil)

// FocusRegions publishes the step rail's labels as click targets on the
// rail line (the page's first line): the exact spans railLine draws,
// clipped to the width it clips to (hit = drawn ink). While the
// generated-item picker or the unparsable reviewer is open it owns the
// keyboard, so the rail behind it publishes nothing and clicks there
// stay inert (the modal-ownership doctrine of the select seams).
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

// railSpans lays out a wizard rail's "<n> <name>" labels — joined by a
// styled sep, optionally preceded by a title span joined by two spaces
// (the analyze/worker rail vocabulary) — and reports each step's drawn
// span. clipX is the cell where the rail line stops being drawn (the
// owner's clipCells bound); a label starting at or beyond it drew no ink
// and publishes no span, and one cut by the clip publishes only its
// visible cells.
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
