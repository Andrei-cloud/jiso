// hitmap_regions_test.go pins the Task 8.2c wheel-region wiring at the
// root (UAT round 8 finding 9): the modal wheel-freeze (fix 1: the page
// branch of handleScrollMsg is inert while any root-owned modal owns
// the screen) and the per-frame hit-map registration of every page
// region added after the §G tracer (translated by frame.ContentOrigin,
// inert one row above the drawn box top). The 8.1/8.2 machinery is
// pinned in hitmap_test.go.
package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
)

// TestScrollMsgFrozenByModal pins Task 8.2c fix 1: while a root-owned
// modal owns the screen (palette, §M overlay, §N3 confirm), the page
// branch of handleScrollMsg is inert — a wheel resolved to the page's
// own region id must not move the frozen page behind the modal. This
// also closes the 8.2b §M margin quirk: with the overlay open, wheeling
// outside the help box no longer scrolls the page underneath.
func TestScrollMsgFrozenByModal(t *testing.T) {
	m := serverAt(t, 80, 24, 30)

	// Baseline: with no modal the page region scrolls normally.
	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -2})
	if got := m.server.LogScroll(); got != 2 {
		t.Fatalf("baseline (no modal): logScroll = %d, want 2", got)
	}

	// The command palette owns the keyboard and draws over the page.
	_, _ = m.Update(ch(':'))
	if m.pal == nil {
		t.Fatal(": must open the command palette")
	}
	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -1})
	if got := m.server.LogScroll(); got != 2 {
		t.Fatalf("palette open: page logScroll = %d, want 2 (frozen behind the modal)", got)
	}
	_, _ = m.Update(special(tea.KeyEsc))
	if m.pal != nil {
		t.Fatal("esc must close the palette")
	}
	if _, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -1}); m.server.LogScroll() != 3 {
		t.Fatal("after the palette closed the page must scroll again")
	}

	// §M open: a wheel over the page margins around the box no longer
	// scrolls the page underneath (the 8.2b quirk, now frozen like the
	// other modals; the box's own region is pinned in
	// TestScrollMsgDispatchHelpOverlay).
	_, _ = m.Update(ch('?'))
	if m.help == nil {
		t.Fatal("? must open the §M overlay")
	}
	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -1})
	if got := m.server.LogScroll(); got != 3 {
		t.Fatalf("§M open: page logScroll = %d, want 3 (margins frozen)", got)
	}
	_, _ = m.Update(special(tea.KeyEsc))
	if m.help != nil {
		t.Fatal("esc must close the overlay")
	}

	// A pending §N3 confirm (q at root arms the quit confirm) freezes
	// the page behind it too.
	_, _ = m.Update(ch('q'))
	if m.workersConfirm == nil || !m.workersConfirm.Pending() {
		t.Fatal("q at root must arm the quit confirmation")
	}
	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -1})
	if got := m.server.LogScroll(); got != 3 {
		t.Fatalf("confirm pending: page logScroll = %d, want 3 (frozen)", got)
	}
}

// TestBuildHitMapPageRegions pins Task 8.2c registration for the pages
// registered beyond the §G tracer: each region reaches the per-frame
// hit map translated by frame.ContentOrigin (wheel over the drawn box
// centre resolves it), and a wheel one row above the box's drawn top
// border resolves nothing under that region id.
func TestBuildHitMapPageRegions(t *testing.T) {
	narrowPreview := strings.Repeat("filler\n", 40)
	cases := []struct {
		name string
		key  rune
		push func(m *RootModel)
		want string
	}{
		{
			name: "tx table", key: '2', want: pages.RegionTxTable,
			push: func(m *RootModel) {
				st := pages.TransactionsState{FileName: "pool.json", TxCount: 40}
				for i := 1; i <= 40; i++ {
					st.Rows = append(st.Rows, pages.TxRow{
						ID: fmt.Sprintf("tx-%02d", i), Name: fmt.Sprintf("Tx %02d", i), MTI: "0200",
						Description: fmt.Sprintf("description %02d", i),
					})
				}
				m.tx.SetState(st)
			},
		},
		{
			name: "workers table", key: '5', want: pages.RegionWorkersTable,
			push: func(m *RootModel) {
				var st pages.WorkersState
				for i := 1; i <= 40; i++ {
					st.Workers = append(st.Workers, pages.WorkerRow{
						ID: fmt.Sprintf("w-%02d", i), Type: "background", Txn: "Echo",
						Status: pages.StatusRunning, Thr: "1", Runtime: "00:00:01", OKFail: "1 / 0",
					})
				}
				m.workers.SetState(st)
			},
		},
		{
			name: "sessions list", key: '6', want: pages.RegionSessionsList,
			push: func(m *RootModel) {
				var st pages.SessionsState
				st.DBPath = "./sessions.db"
				for i := 1; i <= 40; i++ {
					st.Sessions = append(st.Sessions, pages.SessionRow{
						ID: fmt.Sprintf("sess-%04d", i), ShortID: fmt.Sprintf("s-%04d", i), When: "today",
					})
				}
				m.sessions.SetState(st)
			},
		},
		{
			name: "analyze preview", key: '7', want: pages.RegionAnalyzePreview,
			push: func(m *RootModel) {
				st := pages.AnalyzeState{Step: pages.StepRun, Status: pages.AnalyzeStatusDone}
				st.Items = []pages.AnalyzeItemRow{{
					Key: "transaction|a", Name: "A", Kind: "transaction",
					Included: true, Preview: "{\n" + narrowPreview + "}",
				}}
				st.ItemsID = 1
				m.analyze.SetState(st)
			},
		},
		{
			name: "ctf records", key: '8', want: pages.RegionCtfRecords,
			push: func(m *RootModel) {
				st := pages.CtfState{DBPath: "./sessions.db"}
				st.Preview = &pages.CtfPreview{Headline: []string{"one preview"}}
				for i := 1; i <= 40; i++ {
					st.Preview.Records = append(st.Preview.Records,
						fmt.Sprintf("REC %03d %s", i, strings.Repeat("x", 160)))
				}
				st.Preview.OutPath = "./out.dat"
				st.PreviewID = 1
				m.ctf.SetState(st)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewRootModel(nil)
			_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			_, _ = m.Update(ch(tc.key)) // jump owns the page (and sizes it)
			if got := m.Current().ID(); got == pages.DashboardPageID {
				t.Fatalf("hotkey %q did not leave the dashboard", tc.key)
			}
			tc.push(m) // push AFTER the last Update: the sync wrapper would wipe it
			_ = m.View()

			hm := m.buildHitMap()
			sc, ok := m.Current().(pages.Scroller)
			if !ok {
				t.Fatal("the page must implement pages.Scroller")
			}
			var rel geom.Rect
			found := false
			for _, r := range sc.ScrollRegions() {
				if r.ID == tc.want {
					rel, found = r.Rect, true
				}
			}
			if !found {
				t.Fatalf("the last render published no %q region: %#v", tc.want, sc.ScrollRegions())
			}

			ox, oy := m.contentOrigin()
			abs := geom.Rect{X: rel.X + ox, Y: rel.Y + oy, W: rel.W, H: rel.H}
			act, ok := hm.resolve(abs.X+abs.W/2, abs.Y+abs.H/2)
			if !ok || act.kind != hitScroll || act.region != tc.want {
				t.Fatalf("pane centre = %+v,%v, want a scroll hit on %q", act, ok, tc.want)
			}
			// One row above the drawn top border must not resolve THIS
			// region (it may legitimately resolve the pane stacked above
			// it, e.g. the analyze roster over its preview).
			if a2, ok2 := hm.resolve(abs.X+abs.W/2, abs.Y-1); ok2 && a2.region == tc.want {
				t.Errorf("a cell above the box top resolved %q: the region rect is not the drawn ink", tc.want)
			}
		})
	}
}
