// scroll_regions.go is the wheel-scroll region seam (UAT round 8, Task
// 8.2b, finding 9): a page that draws a scrollable pane publishes the
// DRAWN rect of that pane under a stable region id, and applies wheel
// deltas to the same offset its keyboard already drives. The root routes
// a resolved scrollMsg{region, delta} through this seam instead of
// hardcoding any page's region ids: it type-asserts the top page to
// Scroller (the KeyboardClaimer optional-interface pattern), so pages
// without scrollable panes — and pages whose regions a later task
// registers — stay untouched.
//
// Delta convention (pinned at 7f8d8c3, shared with the hitMap wheel
// decode and Analyze.ScrollPreview): d > 0 moves the VIEWPORT DOWN
// through the content (toward later/newer lines); the page owns what
// "down" means for its own offset, and applies d with no negation.
package pages

import "jiso/internal/tui/geom"

// RegionServerLog names the §G SERVER LOG pane's scroll region. Region
// ids are "<page>:<pane>" so one page's ids can never collide with
// another's. The Task 8.2c regions follow the same convention.
const (
	RegionServerLog      = "server:log"
	RegionTxTable        = "tx:table"        // §B transactions table
	RegionWorkersTable   = "workers:table"   // §H workers table
	RegionSessionsList   = "sessions:list"   // §I SESSIONS pane table
	RegionSessionsReview = "sessions:review" // §I tx review overlay window
	RegionAnalyzeItems   = "analyze:items"   // §J generated-item roster
	RegionAnalyzePreview = "analyze:preview" // §J generated-item preview
	RegionCtfRecords     = "ctf:records"     // §K record viewer box
)

// ScrollRegion is one wheel-scrollable area a page published during its
// last View: a stable region id and the DRAWN rect it occupies at the
// page's content-relative origin — the same measured-ink geometry as the
// Phase-6 section Rects (sectionRect). The root's hit map translates it
// with frame.ContentOrigin before resolving absolute mouse cells.
type ScrollRegion struct {
	ID   string
	Rect geom.Rect
}

// Scroller is implemented by pages with wheel-scrollable regions. The
// root asserts it per frame after the page's View, so ScrollRegions must
// report exactly what the LAST render drew (a pane that is not on screen
// — §G's LOG before its first line, or anything under the detail overlay
// — publishes nothing, and the wheel over that space stays inert).
type Scroller interface {
	// ScrollRegions returns the regions the last View drew, in any order.
	ScrollRegions() []ScrollRegion

	// ScrollRegion applies a content-direction delta (d>0 = down) to the
	// named region and reports whether the page owns the region id. An
	// unknown id, or a region the page cannot scroll right now, changes
	// nothing and returns false.
	ScrollRegion(id string, d int) bool
}
