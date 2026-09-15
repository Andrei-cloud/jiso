// sessions.go is the §I sessions DB page (SCR-509): a three-pane
// browser over the session database — SESSIONS list (short id +
// relative time), STATS for the selected session (total/ok/fail/avg/RC
// dist), TX HISTORY (time, tx name, MTI, RC, ✓/✗, latency) — with a
// header filter over the session list and a §C-style reconstructed tx
// review. It is a reference type kept canonical in the router registry,
// so filter/pane/cursor state survives page jumps. All data arrives via
// SetState from root — the page never touches internal/app, never opens
// the database, and never reads the clock (the SCR-501 data-flow
// contract); bus events are ignored and refresh is the root's job (r
// yields SessionsRefreshMsg). Pane focus follows the §C inspector: the
// router's Tab/shift-Tab PaneFocusMsg cycles SESSIONS ↔ TX HISTORY
// (STATS is display-only); below frame.FullWidth the panes collapse to
// the list and Enter drills into the stats+history view.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// EmptyTextNoSessionDB is the empty state every session-backed pane shows when no
// --db was passed. It lived in internal/tui and internal/tui/pages as separate
// literals, which is how one condition came to have two owners: reword one and the
// operator sees two different explanations for the same missing flag.
const EmptyTextNoSessionDB = "database not configured - pass --db to enable session logging"

// --db was passed. It lived in internal/tui and internal/tui/pages as separate
// literals, which is how the same condition came to have two owners: reword one and
// the operator sees two different explanations for the same missing flag.
// Pane focus slots: STATS is display-only and never takes focus.
const (
	paneSessions = iota
	paneHistory
	paneCount
)

// Sessions is the §I page.
type Sessions struct {
	th    *theme.Theme
	state SessionsState

	list    *widgets.Table
	history *widgets.Table
	nav     sessionsNav

	pane      int
	filtering bool
	filter    string
	view      []SessionRow // filtered list, parallel to list rows
	selID     string       // list row under the cursor (identity-tracked)
	txID      int64        // history row under the history cursor

	// reviewOpen tracks the overlay per review identity: a newly pushed
	// Review re-arms the overlay, Esc closes it (the route/summary
	// pattern — the overlay owns Esc first). reviewScroll is the
	// overlay's top line (UAT round 5: a review taller than the window
	// was unreachable — j/k and pgup/pgdn scroll it now).
	reviewOpen    bool
	reviewShownID int64
	reviewScroll  int

	drill  bool // narrow fallback: stats+history for the selected session
	width  int
	height int

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect

	// listRect and reviewRect are the DRAWN boxes the wheel hit map
	// registers under RegionSessionsList / RegionSessionsReview (Task
	// 8.2c): recorded during the last render, the zero value means the
	// pane was not on screen (the review overlay replaces the list, and
	// the narrow drill shows neither list nor review).
	listRect   geom.Rect
	reviewRect geom.Rect

	// selRows are the DRAWN row rects (content-relative, one per visible
	// row) recorded during the last render for the Task 8.3 click-select
	// seam: the SESSIONS list under RegionSessionsList (its wheel
	// region) and the TX HISTORY pane under RegionSessionsHistory (a
	// select-only region: that table draws every row and the box clips,
	// so there is no wheel window to scroll).
	selRows []SelectRegion
}

// §I owns two wheel-scrollable regions and click-selectable rows in the
// list and history panes (Task 8.2c/8.3 seams).
var (
	_ Scroller = (*Sessions)(nil)
	_ Selector = (*Sessions)(nil)
)

// ScrollRegions publishes the regions the last render drew: the
// SESSIONS list box while the list panes are on screen, and the review
// window while the tx review overlay is up. They never coexist (the
// review replaces the body), and a pane that is not drawn publishes
// nothing.
func (s *Sessions) ScrollRegions() []ScrollRegion {
	out := make([]ScrollRegion, 0, 2)
	if s.listRect.W > 0 && s.listRect.H > 0 {
		out = append(out, ScrollRegion{ID: RegionSessionsList, Rect: s.listRect})
	}
	if s.reviewRect.W > 0 && s.reviewRect.H > 0 {
		out = append(out, ScrollRegion{ID: RegionSessionsReview, Rect: s.reviewRect})
	}
	if len(out) == 0 {
		return nil
	}

	return out
}

// ScrollRegion routes the wheel's content-direction delta (d>0 = down)
// to the same offset the keyboard drives: the list window through
// Table.ScrollBy (clamped by the widget), the review overlay through
// scrollReviewBy (clamped to the scroll extent, exactly like j/k).
func (s *Sessions) ScrollRegion(id string, d int) bool {
	switch id {
	case RegionSessionsList:
		if s.listRect.W <= 0 {
			return false
		}
		s.list.ScrollBy(d)

		return true
	case RegionSessionsReview:
		if s.reviewRect.W <= 0 {
			return false
		}
		s.scrollReviewBy(d)

		return true
	}

	return false
}

// SelectRegions publishes the drawn row rects of the list and history
// panes (Task 8.3): a click on a visible row selects it.
func (s *Sessions) SelectRegions() []SelectRegion { return s.selRows }

// SelectRegion moves the clicked pane's cursor and re-tracks the
// selected identity exactly like the keyboard nav does (syncSelID /
// syncTxID), so the next SetState preserves the click instead of
// snapping back.
func (s *Sessions) SelectRegion(id string, index int) bool {
	switch id {
	case RegionSessionsList:
		if index < 0 || index >= len(s.view) {
			return false
		}
		s.list.SetCursor(index)
		s.syncSelID()

		return true
	case RegionSessionsHistory:
		if index < 0 || index >= len(s.state.History) {
			return false
		}
		s.history.SetCursor(index)
		s.syncTxID()

		return true
	}

	return false
}

// ListCursor reports the SESSIONS list cursor index (0 when empty).
func (s *Sessions) ListCursor() int { return s.list.Cursor() }

// HistoryCursor reports the TX HISTORY cursor index (0 when empty).
func (s *Sessions) HistoryCursor() int { return s.history.Cursor() }

// sessionsNav is the page keymap: pane focus arrives as PaneFocusMsg
// from the router (the §C contract), so the page binds only its local
// keys.
type sessionsNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Refresh   key.Binding
	Review    key.Binding
	Down      key.Binding
	Up        key.Binding
	PageUp    key.Binding
	PageDown  key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newSessionsNav() sessionsNav {
	nav := sessionsNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Refresh:   key.NewBinding(key.WithKeys("r")),
		Review:    key.NewBinding(key.WithKeys("t")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		PageUp:    key.NewBinding(key.WithKeys("pgup")),
		PageDown:  key.NewBinding(key.WithKeys("pgdown")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("open", nav.Enter),
		actEntry("review tx", nav.Review),
		actEntry("filter", nav.Filter),
		actEntry("reload", nav.Refresh),
		actEntry("back", nav.Cancel),
	)

	return nav
}

// NewSessions builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewSessions(th *theme.Theme) *Sessions {
	if th == nil {
		th = theme.Default()
	}
	s := &Sessions{
		th: th, nav: newSessionsNav(),
		list:    widgets.NewTable(th, sessionsMinListWidth),
		history: widgets.NewTable(th, sessionsMinHistoryWidth),
	}
	s.list.SetGrid(false) // §I panes box themselves; inner lists stay flat
	s.history.SetGrid(false)
	s.list.SetColumns(sessionsListColumns())
	s.history.SetColumns(sessionsHistoryColumns())

	return s
}

// ID reports the router id of this page (SessionsPageID — the wire-
// compat slot name "db", hotkey 6; the slot's frame tab title is §I's
// "SESSIONS").
func (s *Sessions) ID() string { return SessionsPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Sessions) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Sessions) Size() (width, height int) { return s.width, s.height }

// Pane reports the focused pane (tests).
func (s *Sessions) Pane() int { return s.pane }

// ReviewOpen reports the tx review overlay state (tests).
func (s *Sessions) ReviewOpen() bool { return s.reviewOpen }

// Drill reports the narrow drill-down state (tests).
func (s *Sessions) Drill() bool { return s.drill }

// Filter exposes the live filter text and whether filter mode owns the
// keyboard (root tests; the §B Transactions contract).
func (s *Sessions) Filter() (string, bool) { return s.filter, s.filtering }

// ClaimsKeyboard implements KeyboardClaimer: while the live filter owns
// the keyboard the router forwards every key here (the §B contract), so
// session ids containing q, digits, or : stay typeable.
func (s *Sessions) ClaimsKeyboard() bool { return s.filtering }

// SelectedSessionID reports the session under the list cursor in the
// filtered view ("" when the view is empty).
func (s *Sessions) SelectedSessionID() string {
	if len(s.view) == 0 {
		return ""
	}

	return s.view[min(s.list.Cursor(), len(s.view)-1)].ID
}

// SelectedTxID reports the tx row under the history cursor (0 when the
// history is empty).
func (s *Sessions) SelectedTxID() int64 {
	if len(s.state.History) == 0 {
		return 0
	}

	return s.state.History[min(s.history.Cursor(), len(s.state.History)-1)].ID
}

// SetState replaces the rendered snapshot (root pushes it on load, on
// every refresh, and after select/review queries). Page-local state
// survives: the cursors are re-placed by identity (root's SelectedID,
// the history row id), and a Review whose TxID differs from the last
// shown one re-arms the overlay.
func (s *Sessions) SetState(state SessionsState) {
	prevTx := s.txID
	s.state = state
	s.rebuild()

	if n := len(state.History); n > 0 {
		idx := s.historyCursorFor(prevTx, n)
		s.history.SetCursor(idx)
		s.txID = state.History[idx].ID
	} else {
		s.txID = 0
	}

	switch {
	case state.Review == nil:
		s.reviewOpen = false
		s.reviewShownID = 0
		s.reviewScroll = 0
	case state.Review.TxID != s.reviewShownID:
		s.reviewOpen = true
		s.reviewShownID = state.Review.TxID
		s.reviewScroll = 0
	}
}

// historyCursorFor picks the history row the cursor lands on after a state swap:
// the row matching prevTx by id, else the current cursor clamped to the list.
func (s *Sessions) historyCursorFor(prevTx int64, n int) int {
	if prevTx != 0 {
		for i, h := range s.state.History {
			if h.ID == prevTx {
				return i
			}
		}
	}

	return min(s.history.Cursor(), n-1)
}

// rebuild recomposes the filtered session list and re-places the list
// cursor on root's SelectedID (clamped when absent).
func (s *Sessions) rebuild() {
	f := strings.ToLower(s.filter)
	rows := make([]SessionRow, 0, len(s.state.Sessions))
	for _, r := range s.state.Sessions {
		if f == "" || sessionMatches(r, f) {
			rows = append(rows, r)
		}
	}

	display := make([]widgets.Row, len(rows))
	for i, r := range rows {
		display[i] = widgets.Row{dashIf(s.th, r.ShortID), dashIf(s.th, r.When)}
	}
	s.list.SetRows(display)
	s.list.SetEmptyMessage(s.emptyText())

	idx := 0
	if n := len(rows); n > 0 {
		// The cursor follows the row the user is on (identity wins over
		// root's SelectedID, so intermediate SetStates never steal local
		// navigation — the §B selectAfterRebuild contract); root's
		// selection lands when the local row is gone (first load,
		// filter, list change).
		idx = s.rowIndexByID(rows, s.selID)
		if idx < 0 {
			idx = s.rowIndexByID(rows, s.state.SelectedID)
		}
		if idx < 0 {
			idx = min(s.list.Cursor(), n-1)
		}
	}
	s.list.SetCursor(max(idx, 0))
	s.view = rows
	if n := len(rows); n > 0 {
		s.selID = rows[min(s.list.Cursor(), n-1)].ID
	} else {
		s.selID = ""
	}

	hrows := make([]widgets.Row, len(s.state.History))
	for i, h := range s.state.History {
		hrows[i] = widgets.Row{
			dashIf(s.th, h.Time), dashIf(s.th, h.Name), dashIf(s.th, h.MTI),
			dashIf(s.th, h.RC), s.txStatusCell(h.Status), dashIf(s.th, h.Latency),
		}
	}
	s.history.SetRows(hrows)
	s.history.SetEmptyMessage(s.historyEmptyText())
}

// sessionMatches is the client-side filter: the lowercased id (short or
// full) or the relative stamp contains the lowercased filter.
func sessionMatches(r SessionRow, f string) bool {
	return strings.Contains(strings.ToLower(r.ID+" "+r.ShortID+" "+r.When), f)
}

// emptyText is the SESSIONS pane's short empty-state line (the full
// next-action sentence goes to the full-width hint line — the pane is
// too narrow to carry it without truncation).
func (s *Sessions) emptyText() string {
	switch {
	case s.filtering || s.filter != "":
		return "no sessions match filter"
	case s.state.DBPath == "":
		return "database not configured"
	default:
		return "no sessions"
	}
}

// emptyHintLine is the wireframe's full empty-state sentence naming the
// next action, rendered across the page when the list is empty.
func (s *Sessions) emptyHintLine() string {
	if s.filtering || s.filter != "" || len(s.state.Sessions) > 0 {
		return ""
	}
	if s.state.DBPath == "" {
		return EmptyTextNoSessionDB
	}

	return "no sessions in " + s.state.DBPath + ". Pass --db to enable logging."
}

// historyEmptyText is the TX HISTORY pane's empty-state line.
func (s *Sessions) historyEmptyText() string {
	if s.state.SelectedID == "" {
		return "select a session to see its transactions"
	}

	return "no transactions recorded for this session"
}

// syncSelID re-reads the list identity after a cursor move.
func (s *Sessions) syncSelID() {
	if n := len(s.view); n > 0 {
		s.selID = s.view[min(s.list.Cursor(), n-1)].ID
	}
}

// rowIndexByID finds a session row by full id, -1 when absent.
func (s *Sessions) rowIndexByID(rows []SessionRow, id string) int {
	if id == "" {
		return -1
	}
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}

	return -1
}

// syncTxID re-reads the history identity after a cursor move.
func (s *Sessions) syncTxID() {
	if n := len(s.state.History); n > 0 {
		s.txID = s.state.History[min(s.history.Cursor(), n-1)].ID
	}
}

// Hints is the §I context keymap; select/review/refresh are primary so
// the narrow footer keeps them (the router appends the global bindings).
func (s *Sessions) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "open", Primary: true},
		{Key: "t", Desc: "review tx", Primary: true},
		{Key: theme.KeyTab, Desc: "pane", Primary: true},
		{Key: "/", Desc: "filter"},
		{Key: "r", Desc: "reload"},
		{Key: theme.KeyEsc, Desc: "back"},
	}
}

// txStatusCell maps a canonical tx status token to its symbol+word cell
// (never colour alone): ok gets the ok kind, fail the error kind,
// timeout the error kind with its own word.
func (s *Sessions) txStatusCell(status string) string {
	switch status {
	case TxStatusOK:
		return s.th.Status(theme.KindOK, status)
	case TxStatusFail:
		return s.th.Status(theme.KindError, status)
	case TxStatusTimeout:
		return s.th.Status(theme.KindError, status)
	default:
		return dashIf(s.th, status)
	}
}
