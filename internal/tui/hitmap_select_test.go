// hitmap_select_test.go pins click-to-select: a left click in a visible
// row selects it through the keyboard's clamping, the wheel over the same
// cell still scrolls, pane background clicks stay inert, and an open modal
// freezes selection of the page behind it.
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

// selectRowAt finds the published row rect of (region, index) from the page's last render.
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

	// the wheel over the same row cell still scrolls its pane (one hit, two behaviors)
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

	// the same cell with the window scrolled by one maps to data index
	// scrollOff + visibleIndex (3), not the visible row number (2): only
	// a scrolled click pins the offset term
	v3 := m.View()
	scmd := v3.OnMouse(click)
	if scmd == nil {
		t.Fatal("a click on a scrolled row must emit a select msg")
	}
	if got := scmd(); got != (selectMsg{region: pages.RegionTxTable, index: 3}) {
		t.Fatalf("scrolled row click = %#v, want selectMsg{tx:table 3} (scrollOff 1 + visibleIndex 2)", got)
	}
	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 3}); m.tx.Cursor() != 3 {
		t.Fatalf("after scrolled click-select the cursor = %d, want 3", m.tx.Cursor())
	}

	// a click on the pane background (box top border, no row) stays inert
	bg := m.tx.ScrollRegions()[0].Rect
	inert := v2.OnMouse(tea.MouseClickMsg{X: ox + bg.X + bg.W/2, Y: oy + bg.Y, Button: tea.MouseLeft})
	if inert != nil {
		t.Fatalf("a click on the pane background returned %v, want nil", inert())
	}
}

// handleSelectMsg reuses the modalOpen gate: a straggler selectMsg must
// not move the page behind an open modal, and selection resumes once it closes.
func TestSelectMsgFrozenByModal(t *testing.T) {
	m := NewRootModel(newTxTallApp(t, 40))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	_ = m.View()

	if _, _ = m.Update(selectMsg{region: pages.RegionTxTable, index: 5}); m.tx.Cursor() != 5 {
		t.Fatalf("baseline (no modal): cursor = %d, want 5", m.tx.Cursor())
	}

	// the palette draws over the page: a straggler selectMsg must not move the cursor
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

// clicking a picker entry row moves the cursor and runs the widget's own
// selection: a file commits via FilePickedMsg, a directory descends.
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

		// entries: ".." leads, then dirs, then files → "a.json" is data index 2
		var row geom.Rect
		for _, rh := range m.pickerRowHits() {
			if rh.Index == 2 {
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
		if got := cmd(); got != (selectMsg{region: regionPicker, index: 2}) {
			t.Fatalf("picker row click = %#v, want selectMsg{picker:entries 2}", got)
		}
		_, sel := m.Update(selectMsg{region: regionPicker, index: 2})
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

		// index 0 is the .. parent row, index 1 the specs/ dir: the click descends, no commit cmd
		_, cmd := m.Update(selectMsg{region: regionPicker, index: 1})
		if cmd != nil {
			t.Fatalf("a directory click returned %v, want nil (descend, no commit)", cmd())
		}
		if got := m.filePick.CurrentDir(); got != filepath.Join(dir, "specs") {
			t.Fatalf("directory click left the browser at %q, want specs/", got)
		}
	})

	t.Run("parent row climbs", func(t *testing.T) {
		m := NewRootModel(nil)
		_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
		_, _ = m.Update(ch('2'))
		dir := pickRootFixture(t)
		m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }
		_, fcmd := m.Update(ch('f'))
		m.Update(fcmd())
		_ = m.View()

		// row 0 is the .. entry: clicking it climbs above the start dir, no commit
		_, cmd := m.Update(selectMsg{region: regionPicker, index: 0})
		if cmd != nil {
			t.Fatalf("the .. row click returned %v, want nil (climb, no commit)", cmd())
		}
		if got := m.filePick.CurrentDir(); got != filepath.Dir(dir) {
			t.Fatalf("the .. row click left the browser at %q, want %q", got, filepath.Dir(dir))
		}
	})
}

// same-shape click-select across the remaining surfaces, seeded through
// the root's own caches so syncPages keeps the click's selection.
func TestClickSelectsEverySurface(t *testing.T) {
	pushWorkers := func(m *RootModel) {
		// six rows: each cache row also draws a progress line and 40 would
		// squeeze the table's wheel window to one row
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
			// the click survives the post-Update syncPages identity re-track
			if _, _ = m.Update(msg); tc.cursor(m) != 2 {
				t.Fatalf("after click-select the cursor = %d, want 2", tc.cursor(m))
			}
		})
	}
}

// same offset guard as the grid leg in TestClickSelectsTransactionsRow:
// after a wheel the same cell maps to scrollOff + visibleIndex, not the
// pre-scroll visible row number.
func TestClickSelectsScrolledSessionRow(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('6'))
	now := time.Now()
	for i := 1; i <= 40; i++ {
		m.sessionsList = append(m.sessionsList, app.DbSessionView{
			SessionID: fmt.Sprintf("sess-%04d", i), LastActiveTime: now,
		})
	}
	m.syncSessions()
	v := m.View()

	ox, oy := m.contentOrigin()
	row := selectRowAt(t, m, pages.RegionSessionsList, 2)
	cell := tea.MouseClickMsg{X: ox + row.X + row.W/2, Y: oy + row.Y, Button: tea.MouseLeft}
	cmd := v.OnMouse(cell)
	if cmd == nil {
		t.Fatal("a click inside a drawn row must emit a select msg, got nil")
	}
	if _, _ = m.Update(cmd()); m.sessions.ListCursor() != 2 {
		t.Fatalf("click-select cursor = %d, want 2", m.sessions.ListCursor())
	}

	// wheel the window down one (over the row's own cell, so the wheel still scrolls a scrolled pane)
	v2 := m.View()
	wcmd := v2.OnMouse(tea.MouseWheelMsg{X: cell.X, Y: cell.Y, Button: tea.MouseWheelDown})
	if wcmd == nil {
		t.Fatal("the wheel over a drawn row must still scroll its pane")
	}
	m.Update(wcmd())

	// the same cell now shows the next data row: scrollOff 1 + visibleIndex 2 = 3
	v3 := m.View()
	scmd := v3.OnMouse(cell)
	if scmd == nil {
		t.Fatal("a click on a scrolled row must emit a select msg")
	}
	if got := scmd(); got != (selectMsg{region: pages.RegionSessionsList, index: 3}) {
		t.Fatalf("scrolled row click = %#v, want selectMsg{sessions:list 3} (scrollOff 1 + visibleIndex 2)", got)
	}
	if _, _ = m.Update(selectMsg{region: pages.RegionSessionsList, index: 3}); m.sessions.ListCursor() != 3 {
		t.Fatalf("after scrolled click-select the cursor = %d, want 3", m.sessions.ListCursor())
	}
}

// the picker branch of handleSelectMsg runs before the modalOpen gate, so
// buildHitMap's confirmPending suppression is what keeps a click from
// selecting an entry under a pending confirm.
func TestFilePickerRowsSuppressedUnderConfirm(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	dir := pickRootFixture(t)
	m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }
	_, fcmd := m.Update(ch('f'))
	m.Update(fcmd())
	if m.filePick == nil {
		t.Fatal("f on §B must open the picker")
	}

	// precondition: with only the picker up its rows are registered (the suppression must be the confirm's doing)
	rows := m.pickerRowHits()
	if len(rows) == 0 {
		t.Fatal("precondition: the open picker publishes clickable rows")
	}
	hm := m.buildHitMap()
	if act, ok := hm.resolve(rows[0].Rect.X+rows[0].Rect.W/2, rows[0].Rect.Y); !ok || act.region != regionPicker || act.kind != hitSelect {
		t.Fatalf("precondition: picker row = %+v,%v, want the select hit", act, ok)
	}

	// arm a §N3 confirm over the open picker (confirms draw last, above even the picker)
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "quit jiso?")
	if !m.confirmPending() {
		t.Fatal("precondition: the confirm must be pending")
	}

	hm2 := m.buildHitMap()
	for _, r := range rows {
		if act, ok := hm2.resolve(r.Rect.X+r.Rect.W/2, r.Rect.Y); ok && act.region == regionPicker {
			t.Fatalf("picker row %d still resolves a select hit under the confirm: %+v", r.Index, act)
		}
	}
	// a click on a picker row's cell commits nothing while the confirm owns the screen
	v := m.View()
	for _, r := range rows {
		if cmd := v.OnMouse(tea.MouseClickMsg{X: r.Rect.X + r.Rect.W/2, Y: r.Rect.Y, Button: tea.MouseLeft}); cmd != nil {
			t.Fatalf("a click under the confirm replayed %#v, want inert", cmd())
		}
	}
}
