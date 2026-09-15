// scroll_regions_test.go pins the Task 8.2c wheel regions (UAT round 8
// finding 9): every registered page publishes the DRAWN pane rect under
// its stable id only while that pane is on screen, sizes the widget
// window to the real pane height (the finding-5 fill), and applies the
// wheel's content-direction delta (d>0 = down) to the same offset its
// keyboard drives, clamped at both ends. §G's SERVER LOG seam is pinned
// in server_scroll_test.go; the pages here mirror it exactly.
package pages

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// --- §B transactions (tx:table) ---------------------------------------

// txTallState is §B with n plainly-numbered rows (taller than any pane).
func txTallState(n int) TransactionsState {
	st := TransactionsState{FileName: "pool.json", TxCount: n}
	for i := 1; i <= n; i++ {
		st.Rows = append(st.Rows, TxRow{
			ID: fmt.Sprintf("tx-%02d", i), Name: fmt.Sprintf("Tx %02d", i), MTI: "0200",
			Description: fmt.Sprintf("description %02d", i),
		})
	}

	return st
}

func TestTransactionsScrollRegionsPublishesTableRect(t *testing.T) {
	t.Parallel()

	p := txPage(t, txTallState(40), 120, 32)
	_ = p.View().Content

	regions := p.ScrollRegions()
	if len(regions) != 1 {
		t.Fatalf("§B must publish exactly one scroll region, got %#v", regions)
	}
	r := regions[0]
	if r.ID != RegionTxTable {
		t.Errorf("region id = %q, want %q", r.ID, RegionTxTable)
	}
	if r.Rect.W <= 0 || r.Rect.H <= 0 {
		t.Errorf("published rect must be a drawn box, got %v", r.Rect)
	}
	if r.Rect.Y < 1 {
		t.Errorf("content-relative table rect Y = %d, want below the title row", r.Rect.Y)
	}
	// The window fills the real pane (finding-5 fill): the table's row
	// budget is the content height minus the title row and the grid
	// chrome — and the drawn box reaches the bottom of the pane.
	_, ch := frame.ContentSize(120, 32)
	if _, count := p.table.Window(); count != ch-1-tableGridChrome {
		t.Errorf("table window = %d rows, want the pane budget %d", count, ch-1-tableGridChrome)
	}
	if got := r.Rect.Y + r.Rect.H; got != ch {
		t.Errorf("table box bottom = %d, want the pane bottom %d (the box fills the pane)", got, ch)
	}

	// Empty and error states draw no table: nothing is published and the
	// wheel over them stays inert.
	empty := txPage(t, TransactionsState{}, 120, 32)
	_ = empty.View().Content
	if got := empty.ScrollRegions(); len(got) != 0 {
		t.Errorf("§B empty state published regions %#v, want none", got)
	}
	failed := txPage(t, TransactionsState{Error: "open boom"}, 120, 32)
	_ = failed.View().Content
	if got := failed.ScrollRegions(); len(got) != 0 {
		t.Errorf("§B error state published regions %#v, want none", got)
	}
}

func TestTransactionsScrollRegionWindowsTable(t *testing.T) {
	t.Parallel()

	p := txPage(t, txTallState(40), 120, 32)
	_ = p.View().Content

	// d<0 = window UP (wheel-up), d>0 = window DOWN, clamped at both ends.
	if !p.ScrollRegion(RegionTxTable, -3) {
		t.Fatal("the page must own its own region id")
	}
	if top, _ := p.table.Window(); top != 0 {
		t.Fatalf("after wheel-up past the top the window must clamp at 0, got %d", top)
	}
	if !p.ScrollRegion(RegionTxTable, 3) {
		t.Fatal("the page must own its own region id")
	}
	if top, _ := p.table.Window(); top != 3 {
		t.Fatalf("after 3 rows down the window top = %d, want 3", top)
	}
	p.ScrollRegion(RegionTxTable, 1000)
	if top, count := p.table.Window(); top != p.table.Len()-count {
		t.Fatalf("wheel-down past the last row must clamp the window full at the bottom, got top %d with %d rows", top, count)
	}
	if !strings.Contains(p.View().Content, "Tx 40") {
		t.Error("the bottom-clamped window must render the last row")
	}
	if p.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
}

// --- §H workers (workers:table) ----------------------------------------

// workersTallState is §H with n plainly-numbered workers (no status
// line, TPS strip, or PROGRESS rows, so the table owns the whole pane
// under the title).
func workersTallState(n int) WorkersState {
	st := WorkersState{}
	for i := 1; i <= n; i++ {
		st.Workers = append(st.Workers, WorkerRow{
			ID: fmt.Sprintf("w-%02d", i), Type: "background", Txn: "Echo",
			Status: StatusRunning, Thr: "1", Runtime: "00:00:01", OKFail: "1 / 0",
		})
	}

	return st
}

func TestWorkersScrollRegionsPublishesTableRect(t *testing.T) {
	t.Parallel()

	w := workersPageAt(t, workersTallState(40), 120, 32)
	_ = w.View().Content

	regions := w.ScrollRegions()
	if len(regions) != 1 {
		t.Fatalf("§H must publish exactly one scroll region, got %#v", regions)
	}
	r := regions[0]
	if r.ID != RegionWorkersTable {
		t.Errorf("region id = %q, want %q", r.ID, RegionWorkersTable)
	}
	if r.Rect.W <= 0 || r.Rect.H <= 0 {
		t.Errorf("published rect must be a drawn box, got %v", r.Rect)
	}
	// The window fills the pane under the head: content height minus the
	// title row and the grid chrome (this fixture has no status line,
	// TPS strip, or PROGRESS rows).
	_, ch := frame.ContentSize(120, 32)
	if _, count := w.table.Window(); count != ch-1-tableGridChrome {
		t.Errorf("table window = %d rows, want the pane budget %d", count, ch-1-tableGridChrome)
	}
	if got := r.Rect.Y + r.Rect.H; got != ch {
		t.Errorf("table box bottom = %d, want the pane bottom %d", got, ch)
	}

	// The stress-summary overlay replaces the body: the table is not on
	// screen, so nothing is published and the wheel stays inert.
	st := workersTallState(40)
	st.Summary = workersSummaryFixture()
	ov := workersPageAt(t, st, 120, 32)
	_ = ov.View().Content
	if !ov.SummaryOpen() {
		t.Fatal("precondition: the pushed Summary must arm the overlay")
	}
	if got := ov.ScrollRegions(); len(got) != 0 {
		t.Errorf("§H with the summary overlay published regions %#v, want none", got)
	}
}

func TestWorkersScrollRegionWindowsTable(t *testing.T) {
	t.Parallel()

	w := workersPageAt(t, workersTallState(40), 120, 32)
	_ = w.View().Content

	if !w.ScrollRegion(RegionWorkersTable, 3) {
		t.Fatal("the page must own its own region id")
	}
	if top, _ := w.table.Window(); top != 3 {
		t.Fatalf("after 3 rows down the window top = %d, want 3", top)
	}
	w.ScrollRegion(RegionWorkersTable, -100)
	if top, _ := w.table.Window(); top != 0 {
		t.Fatalf("wheel-up past the top must clamp at 0, got %d", top)
	}
	w.ScrollRegion(RegionWorkersTable, 1000)
	if top, count := w.table.Window(); top != w.table.Len()-count {
		t.Fatalf("wheel-down past the last row must clamp full at the bottom, got top %d with %d rows", top, count)
	}
	if !strings.Contains(w.View().Content, "w-40") {
		t.Error("the bottom-clamped window must render the last worker")
	}
	if w.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
}

// --- §I sessions (sessions:list, sessions:review) -----------------------

// sessionsTallRows is n plainly-numbered session rows.
func sessionsTallRows(n int) []SessionRow {
	rows := make([]SessionRow, 0, n)
	for i := 1; i <= n; i++ {
		rows = append(rows, SessionRow{
			ID: fmt.Sprintf("sess-%04d", i), ShortID: fmt.Sprintf("s-%04d", i),
			When: "today 12:01",
		})
	}

	return rows
}

func TestSessionsScrollRegionsPublishesListRect(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	st := sessionsFixtureState(th)
	st.Sessions = sessionsTallRows(40)
	p := sessionsPageAt(t, st, 120, 32)
	_ = p.View().Content

	regions := p.ScrollRegions()
	if len(regions) != 1 {
		t.Fatalf("§I must publish exactly the list region, got %#v", regions)
	}
	r := regions[0]
	if r.ID != RegionSessionsList {
		t.Errorf("region id = %q, want %q", r.ID, RegionSessionsList)
	}
	if r.Rect.W <= 0 || r.Rect.H <= 0 || r.Rect.Y < 1 {
		t.Errorf("published rect must be a drawn box below the head, got %v", r.Rect)
	}
	// The list window fills the section body (h-3 inner rows minus the
	// flat table header), so the pane stays exactly full.
	if _, count := p.list.Window(); count != max(r.Rect.H-4, 1) {
		t.Errorf("list window = %d rows, want the section budget %d", count, max(r.Rect.H-4, 1))
	}

	// The review overlay replaces the body: the list publishes nothing
	// and the review window publishes its own region.
	st.Review = sessionsReviewFixture()
	rv := sessionsPageAt(t, st, 120, 32)
	_ = rv.View().Content
	regions = rv.ScrollRegions()
	if len(regions) != 1 || regions[0].ID != RegionSessionsReview {
		t.Fatalf("the open review must publish exactly the review region, got %#v", regions)
	}
	if rr := regions[0].Rect; rr.W <= 0 || rr.H <= 0 || rr.Y < 1 {
		t.Errorf("review rect must be the window under the head, got %v", rr)
	}
}

func TestSessionsScrollRegionDrivesOffsets(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	st := sessionsFixtureState(th)
	st.Sessions = sessionsTallRows(40)
	p := sessionsPageAt(t, st, 120, 32)
	_ = p.View().Content

	if !p.ScrollRegion(RegionSessionsList, 3) {
		t.Fatal("the page must own its own region id")
	}
	if top, _ := p.list.Window(); top != 3 {
		t.Fatalf("after 3 rows down the list window top = %d, want 3", top)
	}
	p.ScrollRegion(RegionSessionsList, -100)
	if top, _ := p.list.Window(); top != 0 {
		t.Fatalf("wheel-up past the top must clamp at 0, got %d", top)
	}
	p.ScrollRegion(RegionSessionsList, 1000)
	if top, count := p.list.Window(); top != p.list.Len()-count {
		t.Fatalf("wheel-down past the last row must clamp full at the bottom, got top %d with %d rows", top, count)
	}

	// The review overlay: the wheel drives the SAME reviewScroll the j/k
	// keys move, clamped at the top (the view clamps the bottom).
	st.Review = sessionsReviewFixture()
	rv := sessionsPageAt(t, st, 120, 32)
	_ = rv.View().Content
	if !rv.ScrollRegion(RegionSessionsReview, 2) {
		t.Fatal("the page must own the review region while it is open")
	}
	if rv.reviewScroll != 2 {
		t.Fatalf("after two wheel-ups reviewScroll = %d, want 2", rv.reviewScroll)
	}
	rv.ScrollRegion(RegionSessionsReview, -100)
	if rv.reviewScroll != 0 {
		t.Fatalf("wheel-up past the first line must clamp at 0, got %d", rv.reviewScroll)
	}
	if rv.ScrollRegion(RegionSessionsList, 1) {
		t.Fatal("the list region must be inert while the review overlay owns the body")
	}
	if rv.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
}

// --- §K ctf (ctf:records) -----------------------------------------------

// ctfTallPreviewState attaches a plain n-record preview overlay to the
// fixture (records are fixed literals longer than any window; the same
// shape as ctfPreviewState in ctf_golden_test.go).
func ctfTallPreviewState(th *theme.Theme, n int) CtfState {
	st := ctfFixtureState(th)
	preview := &CtfPreview{
		Headline: []string{"9f3c..a1" + joinSep(th) + fmt.Sprintf("%d records", n)},
		OutPath:  "./out/CTF_001.dat",
	}
	for i := 1; i <= n; i++ {
		preview.Records = append(preview.Records,
			padRight(fmt.Sprintf("REC %03d 0200 4242424242424242 ARN%07d", i, i), 168))
	}
	st.Preview = preview
	st.PreviewID = 1

	return st
}

func TestCtfScrollRegionsPublishesRecordsRect(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	_ = c.View().Content
	if got := c.ScrollRegions(); len(got) != 0 {
		t.Fatalf("§K without the preview overlay published regions %#v, want none", got)
	}

	c = ctfPage(t, 120, 32)
	c.SetState(ctfTallPreviewState(c.th, 40))
	_ = c.View().Content
	if !c.PreviewOpen() {
		t.Fatal("precondition: the pushed Preview must arm the viewer")
	}

	regions := c.ScrollRegions()
	if len(regions) != 1 {
		t.Fatalf("§K must publish exactly one scroll region, got %#v", regions)
	}
	r := regions[0]
	if r.ID != RegionCtfRecords {
		t.Errorf("region id = %q, want %q", r.ID, RegionCtfRecords)
	}
	if r.Rect.W <= 0 || r.Rect.H <= 0 {
		t.Errorf("published rect must be a drawn box, got %v", r.Rect)
	}
	// The box sits below the page head and the overlay headline lines.
	if r.Rect.Y < 2 {
		t.Errorf("content-relative records rect Y = %d, want below the head+headline rows", r.Rect.Y)
	}
	// The vertical window is the shared viewerGeom budget (the same one
	// the keys clamp against): wheel and keyboard can never disagree.
	_, pageH := c.viewerSize()
	if c.recOff+max(pageH-2, 1) > 40 {
		t.Fatal("fixture must be taller than the record window")
	}
}

func TestCtfScrollRegionWalksRecords(t *testing.T) {
	t.Parallel()

	c := ctfPage(t, 120, 32)
	c.SetState(ctfTallPreviewState(c.th, 40))
	_ = c.View().Content

	// The wheel walks the record cursor like j/k and drags the window
	// along; clamping is the keyboard's clamp at both ends.
	if !c.ScrollRegion(RegionCtfRecords, -3) {
		t.Fatal("the page must own its own region id")
	}
	if c.recCursor != 0 {
		t.Fatalf("wheel-up past the first record left recCursor = %d, want 0", c.recCursor)
	}
	if !c.ScrollRegion(RegionCtfRecords, 5) {
		t.Fatal("the page must own its own region id")
	}
	if c.recCursor != 5 {
		t.Fatalf("after 5 wheel-downs recCursor = %d, want 5", c.recCursor)
	}
	c.ScrollRegion(RegionCtfRecords, 1000)
	if want := 40 - 1; c.recCursor != want {
		t.Fatalf("wheel-down past the last record must clamp at %d, got %d", want, c.recCursor)
	}
	_, rows := c.viewerGeom()
	if want := 40 - rows; c.recOff != want {
		t.Fatalf("the window must drag along and sit full at the bottom, recOff = %d, want %d", c.recOff, want)
	}
	if !strings.Contains(c.View().Content, "REC 040") {
		t.Error("the bottom-clamped window must render the last record")
	}
	if c.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
}

// --- §J analyze (analyze:items, analyze:preview) -------------------------

// analyzeTallItemsState attaches a picker roster of n items whose first
// preview outgrows the pane (the same shape as analyzeDoneWithItems).
func analyzeTallItemsState(n int) AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	for i := 1; i <= n; i++ {
		body := strings.Repeat("filler line\n", 60)
		st.Items = append(st.Items, AnalyzeItemRow{
			Key: fmt.Sprintf("transaction|item-%02d", i), Name: fmt.Sprintf("Item %02d", i),
			Kind: "transaction", Included: true,
			Preview: "{\n" + body + fmt.Sprintf("  \"tail\": \"item %02d\"\n}", i),
		})
	}
	st.ItemsID = 1

	return st
}

func TestAnalyzeScrollRegionsPublishPickerRects(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeTallItemsState(40), 120, 32)
	_ = a.View().Content
	if !a.itemsOpen {
		t.Fatal("precondition: a fresh ItemsID must arm the picker")
	}

	regions := a.ScrollRegions()
	if len(regions) != 2 {
		t.Fatalf("the open picker must publish the roster and preview rects, got %#v", regions)
	}
	var items, prev geom.Rect
	for _, r := range regions {
		switch r.ID {
		case RegionAnalyzeItems:
			items = r.Rect
		case RegionAnalyzePreview:
			prev = r.Rect
		default:
			t.Fatalf("unexpected region id %q", r.ID)
		}
	}
	// The panes sit below the rail line and the overlay's two-line head.
	if items.Y != itemsBodyY || prev.Y < itemsBodyY {
		t.Errorf("pane rows = %d/%d, want at or below the overlay head (%d)", items.Y, prev.Y, itemsBodyY)
	}
	if items.W <= 0 || items.H <= 0 || prev.W <= 0 || prev.H <= 0 {
		t.Errorf("pane rects must be real boxes: %v vs %v", items, prev)
	}
	// The two panes never overlap: the wheel over either resolves one.
	if items.X < prev.X+prev.W && prev.X < items.X+items.W &&
		items.Y < prev.Y+prev.H && prev.Y < items.Y+items.H {
		t.Errorf("roster %v and preview %v overlap: the wheel could not tell them apart", items, prev)
	}

	// Closed picker: nothing is on screen, nothing is published.
	a2 := analyzePage(t, analyzeTallItemsState(3), 120, 32)
	_, _ = a2.Update(special(tea.KeyEscape)) // close (applies the set)
	_ = a2.View().Content
	if got := a2.ScrollRegions(); len(got) != 0 {
		t.Errorf("§J with the picker closed published regions %#v, want none", got)
	}
}

func TestAnalyzeScrollRegionDrivesPickerOffsets(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeTallItemsState(40), 120, 32)
	_ = a.View().Content

	// Preview sub-pane: the wheel drives ScrollPreview, the one scroll
	// contract Task 7.3 pinned, clamped at the top.
	if !a.ScrollRegion(RegionAnalyzePreview, 3) {
		t.Fatal("the page must own the preview region while the picker is open")
	}
	if a.previewOff != 3 {
		t.Fatalf("after 3 wheel-downs previewOff = %d, want 3", a.previewOff)
	}
	a.ScrollRegion(RegionAnalyzePreview, -100)
	if a.previewOff != 0 {
		t.Fatalf("wheel-up past the first line must clamp at 0, got %d", a.previewOff)
	}
	a.ScrollRegion(RegionAnalyzePreview, 1000)
	if limit := max(a.previewContentHeight()-a.previewWindow(), 0); a.previewOff != limit {
		t.Fatalf("wheel-down past the last line must clamp at %d, got %d", limit, a.previewOff)
	}

	// Roster: the wheel walks the item cursor (the window dragged along,
	// the keys' rule) and a new item previews from the top.
	if !a.ScrollRegion(RegionAnalyzeItems, 2) {
		t.Fatal("the page must own the roster region while the picker is open")
	}
	if a.itemCursor != 2 {
		t.Fatalf("after 2 wheel-downs itemCursor = %d, want 2", a.itemCursor)
	}
	if a.previewOff != 0 {
		t.Fatalf("moving to a new item must re-home the preview, got %d", a.previewOff)
	}
	a.ScrollRegion(RegionAnalyzeItems, -100)
	if a.itemCursor != 0 {
		t.Fatalf("wheel-up past the first item must clamp at 0, got %d", a.itemCursor)
	}
	a.ScrollRegion(RegionAnalyzeItems, 1000)
	if a.itemCursor != 39 {
		t.Fatalf("wheel-down past the last item must clamp at 39, got %d", a.itemCursor)
	}
	rows := a.itemsWindow()
	if want := 40 - rows; a.itemOff != want {
		t.Fatalf("the roster window must drag along and sit full at the bottom, itemOff = %d, want %d", a.itemOff, want)
	}
	if a.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
}

// TestAnalyzeReopenXHomesPreview pins the Task 8.2c reopen reset: the
// [x] reopen branch must re-home the picker itself. The Esc/Enter close
// paths already re-home (finding 8), so the test parks focus/offset on
// a closed-but-stale picker (the state [x] must defend against: a close
// that did not run the overlay's own re-home) and asserts [x] comes
// back home on the roster with a top-aligned preview.
func TestAnalyzeReopenXHomesPreview(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeTallItemsState(3), 120, 32)
	_, _ = a.Update(special(tea.KeyTab)) // focus the preview sub-pane
	_, _ = a.Update(press('j'))          // scroll the preview window
	if a.previewOff == 0 {
		t.Fatal("precondition: the preview must be scrolled before the close")
	}
	a.itemsOpen = false // simulate a close that skipped the overlay's re-home

	_, _ = a.Update(press('x')) // reopen
	if !a.itemsOpen {
		t.Fatal("x must reopen the picker")
	}
	if a.previewFocused || a.previewOff != 0 {
		t.Fatalf("reopen left previewFocused=%v previewOff=%d, want false/0", a.previewFocused, a.previewOff)
	}
}
