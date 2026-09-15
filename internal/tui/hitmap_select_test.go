// hitmap_select_test.go pins Task 8.3 (UAT round 8 finding 9): a LEFT
// CLICK inside a visible row selects that row — the cursor moves to the
// row's data index through the same clamping the keyboard uses — while
// the WHEEL over the very same cell still scrolls the pane (one hit,
// two behaviors: the row rect carries the pane's scroll region id AND
// the select index, registered after the pane scrollHit so the click
// resolves topmost). A click on the plain pane background stays inert
// (the 8.2c rule), and a click NEVER selects the page behind an open
// modal: handleSelectMsg reuses the handleScrollMsg modalOpen gate.
// The machinery (resolve, addAbs, installMouse) is pinned in
// hitmap_test.go / hitmap_regions_test.go.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// selectRowAt finds the published row rect of (region, index) from the
// page's LAST render — the geometry a click must land on — failing the
// test when the page published no such row.
func selectRowAt(t *testing.T, m *RootModel, id string, index int) geom.Rect {
	t.Helper()

	sl, ok := m.Current().(pages.Selector)
	if !ok {
		t.Fatalf("page %T must implement pages.Selector", m.Current())
	}
	for _, r := range sl.SelectRegions() {
		if r.ID == id && r.Index == index {
			return r.Rect
		}
	}
	t.Fatalf("the last render published no %q row %d: %#v", id, index, sl.SelectRegions())

	return geom.Rect{}
}

// TestClickSelectsTransactionsRow is the Task 8.3 tracer (brief Step 1):
// a left click at the screen y of the transactions table's 3rd visible
// row selects it (Table.Cursor()==2); the wheel over the SAME cell
// still scrolls the pane; a click on the plain pane background (the box
// top border) stays inert.
func TestClickSelectsTransactionsRow(t *testing.T) {
	m := NewRootModel(newTxTallApp(t, 40))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	v := m.View() // renders §B, publishes the row rects, arms OnMouse

	ox, oy := m.contentOrigin()
	row := selectRowAt(t, m, pages.RegionTxTable, 2) // the 3rd visible row
	click := tea.MouseClickMsg{X: ox + row.X + row.W/2, Y: oy + row.Y, Button: tea.MouseLeft}

	cmd := v.OnMouse(click)
	if cmd == nil {
		t.Fatal("a click inside a drawn row must emit a select msg, got nil")
	}
	want := selectMsg{region: pages.RegionTxTable, index: 2}
	if got := cmd(); got != want {
		t.Fatalf("row click = %#v, want %#v", got, want)
	}
	if _, _ = m.Update(want); m.tx.Cursor() != 2 {
		t.Fatalf("after click-select the table cursor = %d, want 2", m.tx.Cursor())
	}

	// The wheel over the SAME row cell still scrolls its pane (one hit,
	// two behaviors): the row action carries the pane's region id.
	v2 := m.View()
	wheel := tea.MouseWheelMsg{X: click.X, Y: click.Y, Button: tea.MouseWheelDown}
	wcmd := v2.OnMouse(wheel)
	if wcmd == nil {
		t.Fatal("the wheel over a drawn row must still scroll its pane")
	}
	wmsg := wcmd()
	if wmsg != (scrollMsg{region: pages.RegionTxTable, delta: 1}) {
		t.Fatalf("wheel over the row = %#v, want scrollMsg{tx:table +1}", wmsg)
	}
	m.Update(wmsg)
	content := m.View().Content
	if strings.Contains(content, "Tx 01") || !strings.Contains(content, "Tx 24") {
		t.Errorf("the wheel did not move the window through the pane:\n%s", content)
	}

	// A click on the plain pane BACKGROUND (the table box's top border
	// row, inside the scroll region but on no row) stays inert: only the
	// wheel acts there (the 8.2c rule).
	bg := m.tx.ScrollRegions()[0].Rect
	inert := v2.OnMouse(tea.MouseClickMsg{X: ox + bg.X + bg.W/2, Y: oy + bg.Y, Button: tea.MouseLeft})
	if inert != nil {
		t.Fatalf("a click on the pane background returned %v, want nil", inert())
	}
}

// TestSelectMsgFrozenByModal pins the mandatory gate: handleSelectMsg
// reuses modalOpen exactly like handleScrollMsg, so a click resolved to
// the page area behind an open modal (palette, §M overlay) never selects
// the frozen page underneath — and selection resumes once it closes.
func TestSelectMsgFrozenByModal(t *testing.T) {
	m := NewRootModel(newTxTallApp(t, 40))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	_ = m.View()

	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 5}); m.tx.Cursor() != 5 {
		t.Fatalf("baseline (no modal): cursor = %d, want 5", m.tx.Cursor())
	}

	// The command palette draws over the page: a straggler selectMsg for
	// the page region must not move the cursor behind it.
	_, _ = m.Update(ch(':'))
	if m.pal == nil {
		t.Fatal(": must open the command palette")
	}
	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 1}); m.tx.Cursor() != 5 {
		t.Fatalf("palette open: cursor = %d, want 5 (frozen behind the modal)", m.tx.Cursor())
	}
	_, _ = m.Update(special(tea.KeyEsc))
	if m.pal != nil {
		t.Fatal("esc must close the palette")
	}
	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 1}); m.tx.Cursor() != 1 {
		t.Fatalf("after the palette closed the page must select again, cursor = %d", m.tx.Cursor())
	}

	// §M open: the page behind stays frozen too (margins included).
	_, _ = m.Update(ch('?'))
	if m.help == nil {
		t.Fatal("? must open the §M overlay")
	}
	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 3}); m.tx.Cursor() != 1 {
		t.Fatalf("§M open: cursor = %d, want 1 (frozen behind the overlay)", m.tx.Cursor())
	}
}

// TestClickSelectsFilePickerEntry pins the file-picker leg: clicking an
// entry row moves the picker cursor AND runs the widget's own entry
// selection — a selectable file commits through FilePickedMsg (the same
// cmd Enter emits), a directory row descends.
func TestClickSelectsFilePickerEntry(t *testing.T) {
	t.Run("file commits", func(t *testing.T) {
		m := NewRootModel(nil)
		_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
		_, _ = m.Update(ch('2'))
		dir := pickRootFixture(t) // specs/ a.json b.txt
		m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }
		_, fcmd := m.Update(ch('f')) // §B tx-file picker (exts: .json)
		if fcmd == nil {
			t.Fatal("f on §B must yield the tx-file pick cmd")
		}
		m.Update(fcmd()) // the page's TxPickFileMsg leg opens the picker (the program runs cmds between Updates)
		if m.filePick == nil {
			t.Fatal("f on §B must open the picker")
		}
		v := m.View()

		// Entries: dirs first (specs/), then files → "a.json" is data
		// index 1. The picker rows are published as ABSOLUTE rects.
		var row geom.Rect
		for _, rh := range m.pickerRowHits() {
			if rh.Index == 1 {
				row = rh.Rect
			}
		}
		if row.W <= 0 {
			t.Fatal("the picker published no row for a.json")
		}
		cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
		if cmd == nil {
			t.Fatal("a click on a picker row must emit a select msg")
		}
		if got := cmd(); got != (selectMsg{region: regionPicker, index: 1}) {
			t.Fatalf("picker row click = %#v, want selectMsg{picker:entries 1}", got)
		}
		_, sel := m.Update(selectMsg{region: regionPicker, index: 1})
		if sel == nil {
			t.Fatal("clicking a selectable file must return the picker's own selection cmd")
		}
		pick, ok := sel().(widgets.FilePickedMsg)
		if !ok || pick.Path != filepath.Join(dir, "a.json") || pick.Label != "fixture/a.json" {
			t.Fatalf("click-select committed %#v, want FilePickedMsg for a.json", sel())
		}
	})

	t.Run("directory descends", func(t *testing.T) {
		m := NewRootModel(nil)
		_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
		_, _ = m.Update(ch('2'))
		dir := pickRootFixture(t)
		m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }
		_, fcmd := m.Update(ch('f'))
		m.Update(fcmd())
		_ = m.View()

		// Index 0 is the specs/ directory: the click enters it (like
		// Enter), no commit cmd.
		_, cmd := m.Update(selectMsg{region: regionPicker, index: 0})
		if cmd != nil {
			t.Fatalf("a directory click returned %v, want nil (descend, no commit)", cmd())
		}
		if got := m.filePick.CurrentDir(); got != filepath.Join(dir, "specs") {
			t.Fatalf("directory click left the browser at %q, want specs/", got)
		}
	})
}

// TestClickSelectsEverySurface wires the same-shape click-select across
// the remaining surfaces (brief Step 3): workers table, sessions list,
// sessions tx-history, server routes, analyze generated items. State is
// seeded through the root's own caches so the post-Update syncPages keeps
// the selection the click made (the production identity path).
func TestClickSelectsEverySurface(t *testing.T) {
	pushWorkers := func(m *RootModel) {
		// Six rows (not the 40 the 8.2c push used): every provisional
		// cache row also draws a PROGRESS line, and 40 of those would
		// legitimately squeeze the table's pane to a 1-row wheel window.
		for i := 1; i <= 6; i++ {
			m.workerRowFor(fmt.Sprintf("w-%02d", i))
		}
		m.syncWorkers()
	}
	pushSessions := func(m *RootModel) {
		now := time.Now()
		for i := 1; i <= 40; i++ {
			m.sessionsList = append(m.sessionsList, app.DbSessionView{
				SessionID: fmt.Sprintf("sess-%04d", i), LastActiveTime: now,
			})
		}
		m.syncSessions()
	}
	pushSessionsHistory := func(m *RootModel) {
		pushSessions(m)
		now := time.Now()
		for i := 1; i <= 10; i++ {
			m.sessionsHistory = append(m.sessionsHistory, app.DbTransactionView{
				ID: int64(i), TxName: fmt.Sprintf("Tx %02d", i), MTI: "0200",
				Timestamp: now, ResponseCode: "00", Success: true,
			})
		}
		m.syncSessions()
	}
	pushRoutes := func(m *RootModel) {
		var routes []config.MockRouteConfig
		for i := 1; i <= 40; i++ {
			routes = append(routes, config.MockRouteConfig{
				Name: fmt.Sprintf("r-%02d", i), ResponseMTI: "0210",
			})
		}
		m.serveRoutesFn = func() []config.MockRouteConfig { return routes }
		m.syncServer()
	}
	pushAnalyzeItems := func(m *RootModel) {
		m.analyzeStep = pages.StepRun
		m.analyzeStatus = pages.AnalyzeStatusDone
		for i := 1; i <= 10; i++ {
			m.analyzeItemRows = append(m.analyzeItemRows, pages.AnalyzeItemRow{
				Key: fmt.Sprintf("transaction|t%02d", i), Name: fmt.Sprintf("T%02d", i),
				Kind: "transaction", Preview: "{}",
			})
		}
		m.analyzeItemsID = 1
		m.syncAnalyze()
	}

	cases := []struct {
		name   string
		key    rune
		push   func(*RootModel)
		id     string
		cursor func(*RootModel) int
	}{
		{"workers table", '5', pushWorkers, pages.RegionWorkersTable, func(m *RootModel) int { return m.workers.Cursor() }},
		{"sessions list", '6', pushSessions, pages.RegionSessionsList, func(m *RootModel) int { return m.sessions.ListCursor() }},
		{"sessions tx history", '6', pushSessionsHistory, pages.RegionSessionsHistory, func(m *RootModel) int { return m.sessions.HistoryCursor() }},
		{"server routes", '4', pushRoutes, pages.RegionServerRoutes, func(m *RootModel) int { return m.server.RoutesCursor() }},
		{"analyze items", '7', pushAnalyzeItems, pages.RegionAnalyzeItems, func(m *RootModel) int { return m.analyze.ItemsCursor() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewRootModel(nil)
			_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
			_, _ = m.Update(ch(tc.key))
			tc.push(m)
			v := m.View()

			ox, oy := m.contentOrigin()
			row := selectRowAt(t, m, tc.id, 2) // the 3rd visible row
			click := tea.MouseClickMsg{X: ox + row.X + row.W/2, Y: oy + row.Y, Button: tea.MouseLeft}

			cmd := v.OnMouse(click)
			if cmd == nil {
				t.Fatal("a click inside a drawn row must emit a select msg, got nil")
			}
			msg := cmd()
			if msg != (selectMsg{region: tc.id, index: 2}) {
				t.Fatalf("row click = %#v, want selectMsg{%q 2}", msg, tc.id)
			}
			// The click survives the post-Update syncPages: the page
			// re-tracks the selected identity exactly like the keyboard.
			if _, _ = m.Update(msg); tc.cursor(m) != 2 {
				t.Fatalf("after click-select the cursor = %d, want 2", tc.cursor(m))
			}
		})
	}
}
