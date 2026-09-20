// scroll_regions.go is the wheel-scroll region seam: a page that draws a
// scrollable pane publishes the DRAWN rect of that pane under a stable
// region id, and applies wheel deltas to the same offset its keyboard
// already drives. Root type-asserts the top page to Scroller, so pages
// without scrollable panes stay untouched.
//
// Delta convention (shared with the hitMap wheel decode): d > 0 moves the
// VIEWPORT DOWN through the content (toward later/newer lines); the page
// owns what "down" means for its own offset and applies d with no negation.
package pages

import (
	"jiso/internal/tui/geom"
	"jiso/internal/tui/widgets"
)

// Region ids name a page's scroll region and are "<page>:<pane>", so one
// page's ids can never collide with another's.
const (
	RegionServerLog      = "server:log"
	RegionTxTable        = "tx:table"        // §B transactions table
	RegionWorkersTable   = "workers:table"   // §H workers table
	RegionSessionsList   = "sessions:list"   // §I SESSIONS pane table
	RegionSessionsReview = "sessions:review" // §I tx review overlay window
	RegionAnalyzeItems   = "analyze:items"   // §J generated-item roster
	RegionAnalyzePreview = "analyze:preview" // §J generated-item preview
	RegionCtfRecords     = "ctf:records"     // §K record viewer box
	RegionSendRequest    = "send:request"    // §D REQUEST pane rows/hex
	RegionSendResponse   = "send:response"   // §D RESPONSE pane rows/hex

	// Select-only regions: these panes draw every row they have (no wheel
	// window), so they publish no scrollHit — a row click still selects.
	RegionServerRoutes    = "server:routes"    // §G ROUTES pane rows
	RegionSessionsHistory = "sessions:history" // §I TX HISTORY pane rows
)

// ScrollRegion is one wheel-scrollable area a page published during its
// last View: a stable region id and the DRAWN rect at the page's
// content-relative origin (the same measured-ink geometry as the section
// Rects; root translates it before resolving absolute mouse cells).
type ScrollRegion struct {
	ID   string
	Rect geom.Rect
}

// Scroller is implemented by pages with wheel-scrollable regions. Root
// asserts it per frame, so ScrollRegions must report exactly what the LAST
// render drew: a pane that is not on screen publishes nothing and the
// wheel over that space stays inert.
type Scroller interface {
	// ScrollRegions returns the regions the last View drew, in any order.
	ScrollRegions() []ScrollRegion

	// ScrollRegion applies a content-direction delta (d>0 = down) to the
	// named region and reports whether the page owns the id. An unknown
	// id, or one it cannot scroll right now, changes nothing: return false.
	ScrollRegion(id string, d int) bool
}

// SelectRegion is one click-selectable ROW a page drew during its last
// View: the row's DRAWN rect (content-relative, one cell tall) and the
// data index a click selects. Rows publish under the pane's own region id,
// registered topmost over the pane box: one cell, the wheel scrolls the
// pane, a click selects the row.
type SelectRegion struct {
	ID    string
	Rect  geom.Rect
	Index int
}

// Selector is implemented by pages with click-selectable rows (the same
// optional-interface pattern as Scroller). SelectRegions must report
// exactly what the LAST render drew; a pane that is not on screen
// publishes nothing and clicks on that space stay inert.
type Selector interface {
	// SelectRegions returns the drawn row rects of the last render, in
	// any order.
	SelectRegions() []SelectRegion

	// SelectRegion moves the named region's cursor to the data index
	// (clamped by the page's identity rules, like the keyboard) and
	// reports whether the page owns the id. Unknown id or an out-of-range
	// index changes nothing.
	SelectRegion(id string, index int) bool
}

// selectRows appends translated copies of a widget's drawn row rects as
// SelectRegions under id, offset by (dx, dy) into the page's
// content-relative coords.
func selectRows(dst []SelectRegion, id string, hits []widgets.RowHit, dx, dy int) []SelectRegion {
	for _, h := range hits {
		dst = append(dst, SelectRegion{
			ID:    id,
			Rect:  geom.Rect{X: h.Rect.X + dx, Y: h.Rect.Y + dy, W: h.Rect.W, H: 1},
			Index: h.Index,
		})
	}

	return dst
}
