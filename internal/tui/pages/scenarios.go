package pages

import (
	"strconv"
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Pane focus slots for the §F master-detail split (the §I/§K pattern,
// UAT round 9 F-9e): the router's Tab/shift-Tab PaneFocusMsg cycles the
// SCENARIOS list ↔ the STEPS pane. Exported like §K's CtfPane* so the
// root tests can pin the cycle through Pane().
const (
	ScenarioPaneList = iota
	ScenarioPaneSteps
	scenarioPaneCount
)

// Scenarios is the §F scenarios page: a master-detail screen — the
// scenario list (left/top, widgets.List with a live `/` filter) and the
// STEPS pane (right/bottom) rendering the step stream root pushed for
// the selected scenario. It is a reference type: the router keeps one
// canonical instance in its registry, so filter and cursor state survive
// page jumps. All app data arrives via SetState from the root model —
// the page never touches internal/app and never reads the clock
// (the SCR-501 data-flow contract dashboard.go established).
//
// Selection is tracked by scenario ID (transactions pattern): preserved
// across filter recomposition when the row still matches, else clamped
// to the nearest valid index.
type Scenarios struct {
	th    *theme.Theme
	state ScenariosState

	list *widgets.List
	nav  scenNav

	pane int // focused pane (ScenarioPaneList / ScenarioPaneSteps)

	filtering bool
	filter    string

	view       []ScenarioRow // filtered rows, parallel to list items
	selectedID string        // identity of the row under the cursor

	// stepCursor is the STEPS pane's cursor (index into
	// state.SelectedSteps; 0 when the stream is empty). It re-homes
	// when the selected scenario changes and clamps when the stream
	// shrinks (the list cursor's identity pattern; UAT round 9 F-9e c
	// gives the step stream its own cursor instead of inert nav).
	stepCursor int

	// stepPreviewOpen arms the step message-preview overlay from the
	// pushed state: a Preview whose identity (scenario + step index)
	// differs from the last shown one opens it, a nil Preview clears
	// it, and Esc closes it first (the §I reviewOpen re-arm contract,
	// sessions.go). stepPreviewScroll is the overlay's top line.
	stepPreviewOpen    bool
	stepPreviewScroll  int
	stepPreviewShownID string

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect
}

// scenNav is the page keymap: filter-mode esc/backspace/enter plus the
// page-local triggers; list navigation is owned by widgets.List, while
// the STEPS-pane cursor (UAT round 9 F-9e c) mirrors the same shared
// widgets navKeys set so both panes answer the same keys. Pane
// focus arrives as PaneFocusMsg from the router (the §C contract), so
// Tab/TabBack are registered for the §M legend only — the page never
// matches them in updateKey (the router's global keymap.PaneFocus owns
// the actual Tab→PaneFocusMsg conversion, the §K ctfNav pattern).
type scenNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Export    key.Binding
	Pop       key.Binding
	Tab       key.Binding
	TabBack   key.Binding

	// STEPS-pane step cursor: the shared widgets navKeys set (arrows
	// always, vim aliases, pgup/pgdn, home/end). The list pane's same
	// keys are already listed in the §M navigation group by
	// tableNavHelp, so these need no extra legend lines.
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Top      key.Binding
	Bottom   key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newScenNav() scenNav {
	nav := scenNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Export:    key.NewBinding(key.WithKeys("e")),
		Pop:       key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Tab:       key.NewBinding(key.WithKeys(theme.KeyTab)),
		TabBack:   key.NewBinding(key.WithKeys("shift+tab")),

		Up:       key.NewBinding(key.WithKeys("up", "k")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Top:      key.NewBinding(key.WithKeys("home")),
		Bottom:   key.NewBinding(key.WithKeys("end")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("run", nav.Enter),
		actEntry("preview step", nav.Enter),
		actEntry("export report", nav.Export),
		actEntry("filter", nav.Filter),
		actEntry("pane", nav.Tab, nav.TabBack),
		actEntry("back", nav.Pop),
	)

	return nav
}

// NewScenarios builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewScenarios(th *theme.Theme) *Scenarios {
	if th == nil {
		th = theme.Default()
	}
	s := &Scenarios{th: th, nav: newScenNav(), list: widgets.NewList(th, scenMinListWidth, 8)}
	s.list.SetEmptyMessage(s.emptyText())

	return s
}

// ID reports the router id of this page (ScenariosPageID; the wire-compat
// slot id "scenario" stays with the merged §C inspector).
func (s *Scenarios) ID() string { return ScenariosPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Scenarios) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Scenarios) Size() (width, height int) { return s.width, s.height }

// Pane reports the focused pane (tests; the §I/§K accessor).
func (s *Scenarios) Pane() int { return s.pane }

// Filter exposes the live filter text and whether filter mode owns the
// keyboard (root tests and future deep links).
func (s *Scenarios) Filter() (string, bool) { return s.filter, s.filtering }

// SelectedID reports the identity of the scenario under the cursor in
// the filtered view ("" when the view is empty). Root derives the steps
// it pushes from this value.
func (s *Scenarios) SelectedID() string { return s.selectedID }

// SetSelected moves the cursor onto the row with ID (no-op when the ID
// is not in the current view). Root uses it to land the cursor on the
// scenario a live run started, so the steps pane shows the run without
// the user hunting for it; the selection then survives SetState
// recomposition like any cursor move.
func (s *Scenarios) SetSelected(id string) {
	for i, r := range s.view {
		if r.ID == id {
			s.selectedID = id
			s.list.SetCursor(i)

			return
		}
	}
}

// ClaimsKeyboard implements KeyboardClaimer: while the live filter owns
// the keyboard the router forwards every key (except the global graceful
// exit) here, so scenario names containing q, digits, or : stay typeable.
// It deliberately does NOT claim on STEPS-pane focus: the router turns
// Tab into a PaneFocusMsg only for non-claiming pages (root_keys.go), so
// claiming while steps holds focus would swallow the very Tab that moves
// focus back — unlike §K, which compensates by matching Tab locally in
// updateKey, §F lets the router own Tab end to end.
func (s *Scenarios) ClaimsKeyboard() bool { return s.filtering }

// SetState replaces the rendered snapshot (root pushes it on boot and on
// every Update). Filter and selection are recomposed over the new list:
// selection is preserved by ID when the row still matches. The step
// cursor re-homes when the selected scenario changes (the list cursor's
// identity pattern) and clamps when the stream shrinks, and a pushed
// Preview with a new identity re-arms the message-preview overlay (the
// §I reviewOpen contract).
func (s *Scenarios) SetState(state ScenariosState) {
	prev := s.list.Cursor()
	prevSel := s.selectedID
	s.state = state
	s.rebuild(prev)

	if s.selectedID != prevSel {
		s.stepCursor = 0
	}
	s.stepCursor = min(s.stepCursor, max(len(state.SelectedSteps)-1, 0))

	switch {
	case state.Preview == nil:
		s.stepPreviewOpen = false
		s.stepPreviewShownID = ""
		s.stepPreviewScroll = 0
	case previewIdentity(*state.Preview) != s.stepPreviewShownID:
		s.stepPreviewOpen = true
		s.stepPreviewShownID = previewIdentity(*state.Preview)
		s.stepPreviewScroll = 0
	}
}

// previewIdentity is the overlay's Preview identity: scenario + step
// index. The same identity re-pushed (e.g. a Loading payload replaced by
// the loaded one) keeps the overlay open without resetting its scroll.
func previewIdentity(p ScenarioStepPreview) string {
	return p.ScenarioID + "#" + strconv.Itoa(p.StepIndex)
}

// rebuild recomposes the filtered view and re-places the cursor.
func (s *Scenarios) rebuild(prevCursor int) {
	rows := make([]ScenarioRow, 0, len(s.state.Scenarios))
	f := strings.ToLower(s.filter)
	for _, r := range s.state.Scenarios {
		if f == "" || strings.Contains(r.matchText(), f) {
			rows = append(rows, r)
		}
	}

	items := make([]widgets.Item, len(rows))
	for i, r := range rows {
		items[i] = widgets.Item{Label: r.Name, Data: r.ID}
	}

	s.list.SetItems(items)
	s.list.SetEmptyMessage(s.emptyText())
	s.selectAfterRebuild(prevCursor, rows)
	s.view = rows
}

// emptyText is the list's empty-state line (distinct per cause, like
// the transactions table's). The dash glyph follows the theme's ASCII
// mode (goldens stay 7-bit).
func (s *Scenarios) emptyText() string {
	switch {
	case s.filtering || s.filter != "":
		return "no scenarios match filter"
	default:
		return "no scenarios " + dashIf(s.th, "") + " load via :"
	}
}

// selectAfterRebuild preserves the selected scenario by ID, else clamps
// the previous cursor index into the new view.
func (s *Scenarios) selectAfterRebuild(prevCursor int, rows []ScenarioRow) {
	idx := -1
	if s.selectedID != "" {
		for i, r := range rows {
			if r.ID == s.selectedID {
				idx = i

				break
			}
		}
	}
	if idx < 0 {
		idx = min(max(prevCursor, 0), max(len(rows)-1, 0))
	}
	if len(rows) == 0 {
		s.selectedID = ""
		s.list.SetCursor(0)

		return
	}
	s.list.SetCursor(idx)
	s.selectedID = rows[idx].ID
}

// Update routes sizes, the router's pane-focus tabs, and keys; bus
// events are root-side truth and are ignored with a nil command. While
// the step preview overlay is up it owns the keyboard, so the router's
// Tab does not move pane focus underneath it (the §I Update gate).
func (s *Scenarios) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case PaneFocusMsg:
		if !s.stepPreviewOpen {
			s.pane = (s.pane + scenarioPaneCount + boolToStep(msg.Reverse)) % scenarioPaneCount
		}
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// updateKey is the page-local keymap: the step preview overlay owns the
// keyboard first (Esc closes before anything else, the §I updateReview
// contract), then the live filter, then the page triggers, then the
// focused pane's navigation.
func (s *Scenarios) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if s.stepPreviewOpen {
		return s.updateStepPreview(msg)
	}
	if s.filtering {
		switch {
		case key.Matches(msg, s.nav.Cancel):
			s.filter, s.filtering = "", false
			s.rebuild(s.list.Cursor())

			return s, nil
		case key.Matches(msg, s.nav.Backspace):
			s.filter = dropLastRune(s.filter)
			s.rebuild(s.list.Cursor())

			return s, nil
		case key.Matches(msg, s.nav.Enter):
			s.filtering = false // enter applies the filter and yields the keyboard

			return s, nil
		}
		if r, ok := printableRune(msg.Text); ok {
			s.filter += string(r)
			s.rebuild(s.list.Cursor())

			return s, nil
		}
		// Navigation inside the filter runs on the non-printable
		// codes (arrows, pgup/pgdn, home/end) — a live filter must be
		// typeable, so printable keys including q/digits/: go to the
		// filter text (transactions pattern).
		return s.updateNav(msg)
	}

	switch {
	case key.Matches(msg, s.nav.Filter):
		s.filtering = true

		return s, nil
	case key.Matches(msg, s.nav.Enter):
		// Enter is pane-bound (UAT round 9 F-9e c, the Task 9.7 Minor):
		// on the STEPS pane it asks root for the step's message preview
		// — it must NOT run the list scenario; on the list pane it runs
		// the scenario under the cursor (semantics unchanged).
		if s.pane == ScenarioPaneSteps {
			return s.stepDetail()
		}
		if len(s.view) == 0 {
			return s, nil
		}
		id := s.view[min(s.list.Cursor(), len(s.view)-1)].ID

		return s, func() tea.Msg { return ScenarioRunMsg{ID: id} }
	case key.Matches(msg, s.nav.Export):
		return s, func() tea.Msg { return ScenarioExportMsg{} }
	case key.Matches(msg, s.nav.Pop):
		return s, func() tea.Msg { return ScenarioPopMsg{} }
	default:
		return s.updateNav(msg)
	}
}

// updateNav forwards navigation to the focused pane and re-syncs the
// tracked identity; unknown keys reach the list and are ignored there.
// While the STEPS pane holds focus the nav keys drive the step cursor
// over the pushed stream (Task 9.8, replacing the round-9 inert
// routing), never the list cursor (the §I updateNav routing-by-pane
// contract).
func (s *Scenarios) updateNav(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if s.pane == ScenarioPaneSteps {
		s.moveStepCursor(msg)

		return s, nil
	}
	next, cmd := s.list.Update(msg)
	s.list = next
	if n := len(s.view); n > 0 {
		s.selectedID = s.view[min(s.list.Cursor(), n-1)].ID
	}

	return s, cmd
}

// moveStepCursor moves the STEPS-pane cursor over the pushed step
// stream on the shared nav key set (j/k + arrows one step, pgup/pgdn a
// viewport, home/end the ends). The cursor is clamped into the stream;
// a stream-less pane stays inert. Unknown keys do nothing.
func (s *Scenarios) moveStepCursor(msg tea.KeyPressMsg) {
	n := len(s.state.SelectedSteps)
	if n == 0 {
		return
	}
	page := max(s.stepViewport()-1, 1) // one line of context, like widgets.List
	switch {
	case key.Matches(msg, s.nav.Up):
		s.stepCursor--
	case key.Matches(msg, s.nav.Down):
		s.stepCursor++
	case key.Matches(msg, s.nav.PageUp):
		s.stepCursor -= page
	case key.Matches(msg, s.nav.PageDown):
		s.stepCursor += page
	case key.Matches(msg, s.nav.Top):
		s.stepCursor = 0
	case key.Matches(msg, s.nav.Bottom):
		s.stepCursor = n - 1
	default:
		return
	}
	s.stepCursor = min(max(s.stepCursor, 0), n-1)
}

// stepViewport is the STEPS pane's visible line count, the page size
// for pgup/pgdn. It mirrors render's pane geometry (title row, optional
// error strip, banner row, and the section's title + two rules; the
// stacked band gives the steps pane the lower half) so the page step
// matches what is actually drawn.
func (s *Scenarios) stepViewport() int {
	w, h := frame.ContentSize(s.width, s.height)
	errLines := 0
	if strip := s.errorStrip(w); strip != "" {
		errLines = strings.Count(strip, "\n") + 1
	}
	paneH := max(h-2-errLines, 4)
	if w < frame.FullWidth {
		paneH = max(paneH/2, 3)
	}

	return max(paneH-3, 1)
}

// Hints is the §F context keymap; run/export are primary so the narrow
// footer keeps them (the router appends the global bindings); tab is
// primary too because pane focus is the finding F-9e entry point (the
// §I hint). Esc pops like the inspector and send pages (no-op at depth 1).
func (s *Scenarios) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "run", Primary: true},
		{Key: "e", Desc: "export report", Primary: true},
		{Key: theme.KeyTab, Desc: "pane", Primary: true},
		{Key: "/", Desc: "filter"},
		{Key: theme.KeyNavJK, Desc: "nav"},
		{Key: theme.KeyEsc, Desc: "back"},
	}
}
