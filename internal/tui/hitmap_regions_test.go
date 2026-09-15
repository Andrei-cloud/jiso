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
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
)

// txTallFixtureJSON builds an n-transaction tx file (plainly numbered so
// the rendered window is assertable by row name).
func txTallFixtureJSON(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"type":"transaction","name":"Tx %02d","description":"description %02d","fields":{"0":"0200","7":"auto"}}`, i, i)
	}
	b.WriteString("]")

	return b.String()
}

// newTxTallApp is the newTxFileApp idiom (root_transactions_test.go: the
// config singleton means never parallel) with n numbered rows.
func newTxTallApp(t *testing.T, n int) *app.App {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir())
	txFile := t.TempDir() + "/pool.json"
	if err := os.WriteFile(txFile, []byte(txTallFixtureJSON(n)), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetSpec("../../specs/spec.json")
	cfg.SetFile(txFile)

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	return a
}

// TestScrollMsgTxTableWindowSurvivesSync pins the Task 8.2c review C1:
// RootModel.Update runs syncPages after EVERY message — including the
// scrollMsg itself — and the §B sync re-sets the SAME table cursor
// (SetState → rebuild → SetCursor with the unchanged index). A
// dragWindow that fired on every SetCursor snapped the wheel window
// back to the cursor row, making tx:table dead at runtime even though
// the page-level seam test (which skips the sync wrapper) passed. The
// wheel must move the window through the REAL Update path and it must
// stay there; a genuine cursor move (the keyboard) must still drag it.
func TestScrollMsgTxTableWindowSurvivesSync(t *testing.T) {
	m := NewRootModel(newTxTallApp(t, 40))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	_ = m.View() // §B renders (and publishes the region)

	if _, cmd := m.Update(scrollMsg{region: pages.RegionTxTable, delta: 5}); cmd != nil {
		t.Fatalf("the wheel returned cmd %v, want nil", cmd)
	}
	content := m.View().Content
	// The window moved AND survived the post-Update syncPages: five rows
	// down, so the first row is out of the window while the new first
	// row renders.
	if !strings.Contains(content, "Tx 06") {
		t.Errorf("after wheel-down 5 the window must render the new first row:\n%s", content)
	}
	if strings.Contains(content, "Tx 01") {
		t.Errorf("the wheel window snapped back to the cursor on the post-Update syncPages:\n%s", content)
	}

	// The same-cursor guard must not have cost the keyboard its drag:
	// after a deep wheel, a REAL cursor move (j) drags the window back
	// to the cursor.
	m.Update(scrollMsg{region: pages.RegionTxTable, delta: 1000}) // bottom clamp
	_ = m.View()
	if strings.Contains(m.View().Content, "Tx 01") {
		t.Fatal("precondition: the deep wheel must have hidden the first row")
	}
	_, _ = m.Update(ch('j'))
	content = m.View().Content
	if !strings.Contains(content, "Tx 02") {
		t.Errorf("a real cursor move must still drag the window into view:\n%s", content)
	}
	if strings.Contains(content, "Tx 40") {
		t.Errorf("the window did not drag back to the cursor (the bottom row is still visible):\n%s", content)
	}
}

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
			name: "analyze roster", key: '7', want: pages.RegionAnalyzeItems,
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
			name: "sessions review", key: '6', want: pages.RegionSessionsReview,
			push: func(m *RootModel) {
				var st pages.SessionsState
				st.DBPath = "./sessions.db"
				for i := 1; i <= 40; i++ {
					st.Sessions = append(st.Sessions, pages.SessionRow{
						ID: fmt.Sprintf("sess-%04d", i), ShortID: fmt.Sprintf("s-%04d", i), When: "today",
					})
				}
				st.Review = &pages.TxReviewState{
					TxID:     7,
					Headline: []string{"Purchase id 7 RC 96"},
					Request: &pages.TxReviewMessage{
						HEX: "30 32 30 30", Describe: "MTI ....\n  2 (PAN) 4242424242424242",
					},
				}
				m.sessions.SetState(st)
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
			if !ok || act.region != tc.want {
				t.Fatalf("pane centre = %+v,%v, want a hit on %q", act, ok, tc.want)
			}
			// Task 8.3: the centre of a ROW-BEARING pane (tx, workers,
			// sessions list, analyze roster) now resolves the topmost
			// SELECT hit carrying the same region id — the wheel still
			// scrolls through it, a click selects. Text panes (review,
			// preview, records) keep the plain scroll hit.
			if act.kind != hitScroll && act.kind != hitSelect {
				t.Fatalf("pane centre resolved kind %v on %q, want scroll or select", act.kind, act.region)
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
