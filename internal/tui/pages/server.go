// server.go is the §G mock-server page (SCR-507): a status header
// (● running / ○ stopped — symbol+word, never color alone), the STATS
// card and the ROUTES table side by side at ≥ frame.FullWidth and
// stacked below it, the start-form hint while stopped, and the
// Enter-on-route detail overlay. It is a reference type: the router
// keeps one canonical instance in its registry, so routes focus and the
// detail cursor survive page jumps. All app data arrives via SetState
// from the root model — the page never touches internal/app and never
// reads the clock (the SCR-501 data-flow contract dashboard.go
// established). The start form itself is a root-owned modal (the §E
// ConnectDialog machinery, reused), like the connect dialog never lives
// in the page stack.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Server is the §G page.
type Server struct {
	th    *theme.Theme
	state ServerState

	table *widgets.Table
	nav   serverNav

	routesFocused bool
	// logScroll is the SERVER LOG scroll offset in lines above the
	// newest (0 = following the live tail; j/k move it, G/end resume)
	// (proposal 05: the log is the page's primary live pane).
	logScroll  int
	detailOpen bool
	detailIdx  int

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect

	// logRect is the DRAWN SERVER LOG box (content-relative, measured
	// from the rendered string like every sectionRect) recorded during
	// the last View; the zero value means the pane was not on screen.
	// It is the geometry the Task 8.2b hit map registers under
	// RegionServerLog.
	logRect geom.Rect

	// selRows are the DRAWN ROUTES row rects (content-relative) recorded
	// during the last render for the Task 8.3 click-select seam, under
	// RegionServerRoutes (a select-only region: the routes table is
	// unwindowed, the box clips its body).
	selRows []SelectRegion
}

// §G owns one wheel-scrollable region (the SERVER LOG pane) and
// click-selectable rows in the ROUTES pane (Task 8.2b/8.3 seams).
var (
	_ Scroller = (*Server)(nil)
	_ Selector = (*Server)(nil)
)

// serverNav is the page keymap: stop, routes-pane focus, detail
// enter/close, and the pop chord.
type serverNav struct {
	Stop   key.Binding
	Routes key.Binding
	Enter  key.Binding
	Cancel key.Binding
	Pop    key.Binding
	Up     key.Binding
	Down   key.Binding
	Follow key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newServerNav() serverNav {
	nav := serverNav{
		Stop:   key.NewBinding(key.WithKeys("s")),
		Routes: key.NewBinding(key.WithKeys("r")),
		Enter:  key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Cancel: key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Pop:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Up:     key.NewBinding(key.WithKeys("up", "k")),
		Down:   key.NewBinding(key.WithKeys("down", "j")),
		Follow: key.NewBinding(key.WithKeys("G", "end")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("stop server", nav.Stop),
		// r toggles the routes-pane focus (it never reloads anything —
		// the registry text must match what the key does, E5-FIX/M6).
		actEntry("focus routes", nav.Routes),
		actEntry("open route", nav.Enter),
		actEntry("follow log tail", nav.Follow),
		actEntry("back", nav.Pop),
	)

	return nav
}

// NewServer builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewServer(th *theme.Theme) *Server {
	if th == nil {
		th = theme.Default()
	}
	s := &Server{th: th, nav: newServerNav(), table: widgets.NewTable(th, serverMinTableWidth)}
	s.table.SetGrid(false) // §G ROUTES pane boxes itself; list stays flat
	s.table.SetColumns(serverColumns())
	s.table.SetEmptyMessage("no mock routes configured")

	return s
}

// ID reports the router id of this page (ServerPageID — the wire-compat
// slot name, kept for hotkey 4 and the palette ":server" jump).
func (s *Server) ID() string { return ServerPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Server) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Server) Size() (width, height int) { return s.width, s.height }

// RoutesFocused reports whether the routes pane owns the navigation
// keys (r / tab toggle it; root tests and future deep links).
func (s *Server) RoutesFocused() bool { return s.routesFocused }

// DetailOpen reports the route-detail overlay state and its row index
// (-1 when closed).
func (s *Server) DetailOpen() (bool, int) { return s.detailOpen, s.detailIdx }

// LogScroll reports the SERVER LOG offset (0 = following the newest
// line; j/k and the wheel move it) — for root tests and deep links.
func (s *Server) LogScroll() int { return s.logScroll }

// ScrollRegions publishes the SERVER LOG pane's drawn rect: the region
// exists exactly while the pane is on screen, so §G before its first log
// line, the starting line, and the route-detail view publish nothing and
// the wheel over them stays inert instead of phantom-hitting a box that
// is not drawn.
func (s *Server) ScrollRegions() []ScrollRegion {
	if s.logRect.W <= 0 || s.logRect.H <= 0 {
		return nil
	}

	return []ScrollRegion{{ID: RegionServerLog, Rect: s.logRect}}
}

// ScrollRegion drives the SAME logScroll offset the j/k keys move: the
// wheel's content-direction delta maps straight onto it (d>0 = viewport
// down = toward the newest = offset toward 0), clamped exactly like
// updateKey. The pane's visible window is derived from the real drawn
// height inside logBox on every render, so no separate SetHeight call
// can go stale here.
func (s *Server) ScrollRegion(id string, d int) bool {
	if id != RegionServerLog || s.detailOpen || s.state.Starting {
		return false
	}
	s.logScroll = min(max(s.logScroll-d, 0), max(len(s.state.Log)-1, 0))

	return true
}

// SelectRegions publishes the ROUTES pane's drawn row rects (Task 8.3):
// a click on a visible route moves the routes cursor there.
func (s *Server) SelectRegions() []SelectRegion { return s.selRows }

// SelectRegion moves the routes cursor to the clicked row (clamped like
// the keyboard nav; SetState re-places by index, which the click has
// already moved).
func (s *Server) SelectRegion(id string, index int) bool {
	if id != RegionServerRoutes || index < 0 || index >= len(s.state.Routes) {
		return false
	}
	s.table.SetCursor(index)

	return true
}

// RoutesCursor reports the routes table cursor index (0 when empty).
func (s *Server) RoutesCursor() int { return s.table.Cursor() }

// SetState replaces the rendered snapshot (root pushes it on boot, on
// every tick, and on every start/stop transition). The routes table is
// recomposed over the new rows and the cursor clamped; the detail
// overlay closes when its route disappears.
func (s *Server) SetState(state ServerState) {
	prev := s.table.Cursor()
	s.state = state

	rows := make([]widgets.Row, len(state.Routes))
	for i, r := range state.Routes {
		rows[i] = widgets.Row{r.Match, dashIf(s.th, r.Resp), s.hitsCell(r)}
	}
	s.table.SetRows(rows)

	if s.detailIdx > len(state.Routes)-1 {
		s.detailIdx = max(len(state.Routes)-1, 0)
	}
	if len(state.Routes) == 0 {
		s.detailIdx = 0
	} else {
		s.table.SetCursor(min(prev, len(state.Routes)-1))
	}
	if s.detailOpen && len(state.Routes) == 0 {
		s.detailOpen = false
	}
	if s.logScroll > max(len(state.Log)-1, 0) {
		s.logScroll = max(len(state.Log)-1, 0)
	}
}

// Update routes sizes, pane-focus toggles, and keys; bus events do not
// concern this page and are ignored with a nil command.
func (s *Server) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case PaneFocusMsg:
		s.routesFocused = !s.routesFocused
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// updateKey is the page-local keymap: the detail overlay owns Esc
// first, then the page triggers, then routes-pane navigation.
func (s *Server) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if s.detailOpen {
		if key.Matches(msg, s.nav.Cancel) {
			s.detailOpen = false

			return s, nil
		}

		return s, nil // the overlay owns the keyboard wholesale
	}

	switch {
	case key.Matches(msg, s.nav.Stop):
		return s, func() tea.Msg { return ServerStopMsg{} }
	case key.Matches(msg, s.nav.Routes):
		s.routesFocused = !s.routesFocused

		return s, nil
	case key.Matches(msg, s.nav.Enter):
		if s.routesFocused && len(s.state.Routes) > 0 {
			s.detailIdx = min(s.table.Cursor(), len(s.state.Routes)-1)
			s.detailOpen = true
		}

		return s, nil
	case key.Matches(msg, s.nav.Pop):
		return s, func() tea.Msg { return ServerPopMsg{} }
	default:
		if s.routesFocused {
			next, cmd := s.table.Update(msg)
			s.table = next

			return s, cmd
		}

		// Log focus (default): j/k scroll the history, G/end resume
		// following the newest line.
		switch {
		case key.Matches(msg, s.nav.Up):
			s.logScroll = min(s.logScroll+1, max(len(s.state.Log)-1, 0))
		case key.Matches(msg, s.nav.Down):
			s.logScroll = max(s.logScroll-1, 0)
		case key.Matches(msg, s.nav.Follow):
			s.logScroll = 0
		}

		return s, nil
	}
}

// Hints is the §G context keymap; stop/routes/configure are primary so
// the narrow footer keeps them (the router appends the global
// bindings). c is intercepted by the router on this page and opens the
// server start form instead of the §E connect dialog.
func (s *Server) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: "s", Desc: "stop", Primary: true},
		{Key: "r", Desc: "routes", Primary: true},
		{Key: "c", Desc: "configure", Primary: true},
		{Key: theme.KeyNavJK, Desc: "scroll log"},
		{Key: theme.KeyEsc, Desc: "back"},
	}
}
