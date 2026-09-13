package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
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

	filtering bool
	filter    string

	view       []ScenarioRow // filtered rows, parallel to list items
	selectedID string        // identity of the row under the cursor

	width, height int // last tea.WindowSizeMsg (terminal, not content area)
}

// scenNav is the page keymap: filter-mode esc/backspace/enter plus the
// page-local triggers; list navigation is owned by widgets.List.
type scenNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Export    key.Binding
	Pop       key.Binding

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
	}
	nav.help = append(tableNavHelp(),
		actEntry("run", nav.Enter),
		actEntry("export report", nav.Export),
		actEntry("filter", nav.Filter),
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
func (s *Scenarios) ClaimsKeyboard() bool { return s.filtering }

// SetState replaces the rendered snapshot (root pushes it on boot and on
// every Update). Filter and selection are recomposed over the new list:
// selection is preserved by ID when the row still matches.
func (s *Scenarios) SetState(state ScenariosState) {
	prev := s.list.Cursor()
	s.state = state
	s.rebuild(prev)
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

// Update routes sizes and keys; bus events and pane-focus toggles do not
// concern this page and are ignored with a nil command.
func (s *Scenarios) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// updateKey is the page-local keymap: filter-mode editing first (the live
// filter owns the keyboard), then the page triggers, then list nav.
func (s *Scenarios) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
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

// updateNav forwards navigation to the list and re-syncs the selected
// identity; unknown keys reach the list and are ignored there.
func (s *Scenarios) updateNav(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	next, cmd := s.list.Update(msg)
	s.list = next
	if n := len(s.view); n > 0 {
		s.selectedID = s.view[min(s.list.Cursor(), n-1)].ID
	}

	return s, cmd
}

// Hints is the §F context keymap; run/export are primary so the narrow
// footer keeps them (the router appends the global bindings). Esc pops
// like the inspector and send pages (no-op at depth 1).
func (s *Scenarios) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "run", Primary: true},
		{Key: "e", Desc: "export report", Primary: true},
		{Key: "/", Desc: "filter"},
		{Key: theme.KeyNavJK, Desc: "nav"},
		{Key: theme.KeyEsc, Desc: "back"},
	}
}
