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

// Pane focus slots: the router's Tab/shift-Tab PaneFocusMsg cycles the
// scenario list ↔ the STEPS pane.
const (
	ScenarioPaneList = iota
	ScenarioPaneSteps
	scenarioPaneCount
)

// Scenarios is the master-detail scenarios page: a filtered scenario list
// beside a STEPS pane showing the selected scenario's steps. A reference
// type: the router's one canonical instance keeps filter and cursor state
// across page jumps. All data arrives via SetState from the root model —
// the page never touches internal/app and never reads the clock.
type Scenarios struct {
	th    *theme.Theme
	state ScenariosState

	list *widgets.List
	nav  scenNav

	pane int // focused pane (ScenarioPaneList / ScenarioPaneSteps)

	filtering bool
	filter    string

	// Selection is tracked by ID: preserved across filter recomposition
	// when the row still matches, else clamped to the nearest valid index.
	view       []ScenarioRow // filtered rows, parallel to list items
	selectedID string        // identity of the row under the cursor

	// STEPS pane cursor (index into state.SelectedSteps, 0 when empty):
	// re-homes when the selected scenario changes, clamps when it shrinks.
	stepCursor int

	// Message-preview overlay, armed from pushed state: a Preview whose
	// identity (scenario + step index) differs from the last shown one
	// opens it, a nil Preview clears it, Esc closes it. Scroll is the
	// overlay's top line.
	stepPreviewOpen    bool
	stepPreviewScroll  int
	stepPreviewShownID string

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// geom.Rect of every Section drawn during the last render, in draw
	// order, content-relative (the hit-map finalises the offsets).
	sections []geom.Rect
}

// scenNav is the page keymap: filter keys, page triggers, and the
// STEPS-pane cursor mirroring the shared widgets navKeys. Pane focus
// arrives as PaneFocusMsg from the router, so Tab/TabBack are registered
// for the help legend only — the page never matches them in updateKey.
type scenNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Export    key.Binding
	Pop       key.Binding
	Tab       key.Binding
	TabBack   key.Binding

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

// NewScenarios builds the page; a nil theme selects theme.Default().
func NewScenarios(th *theme.Theme) *Scenarios {
	if th == nil {
		th = theme.Default()
	}
	s := &Scenarios{th: th, nav: newScenNav(), list: widgets.NewList(th, scenMinListWidth, 8)}
	s.list.SetEmptyMessage(s.emptyText())

	return s
}

// ID reports the router id of this page (ScenariosPageID).
func (s *Scenarios) ID() string { return ScenariosPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Scenarios) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Scenarios) Size() (width, height int) { return s.width, s.height }

// Pane reports the focused pane (tests).
func (s *Scenarios) Pane() int { return s.pane }

// Filter exposes the live filter text and whether filter mode owns the keyboard.
func (s *Scenarios) Filter() (string, bool) { return s.filter, s.filtering }

// SelectedID reports the row under the cursor in the filtered view (""
// when empty); root derives the pushed steps from it.
func (s *Scenarios) SelectedID() string { return s.selectedID }

// SetSelected moves the cursor onto the row with id (no-op when absent).
// Root lands it on a live run's scenario so the steps pane follows the run.
func (s *Scenarios) SetSelected(id string) {
	for i, r := range s.view {
		if r.ID == id {
			s.selectedID = id
			s.list.SetCursor(i)

			return
		}
	}
}

// ClaimsKeyboard implements KeyboardClaimer: the live filter claims the
// keyboard so scenario names stay typeable. It deliberately does NOT claim
// on STEPS-pane focus — that would swallow the very Tab the router turns
// into a PaneFocusMsg, which it only sends to non-claiming pages.
func (s *Scenarios) ClaimsKeyboard() bool { return s.filtering }

// SetState replaces the rendered snapshot: filter and selection recompose
// (selection preserved by ID, else clamped), the step cursor re-homes on
// scenario change, and a Preview with a new identity re-arms the overlay.
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

// previewIdentity is the overlay's Preview identity: scenario + step index.
// The same identity re-pushed keeps the overlay open without resetting scroll.
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

// emptyText is the list's empty-state line, distinct per cause; the dash
// follows the theme's ASCII mode (goldens stay 7-bit).
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

// Update routes sizes, pane-focus cycles, and keys. While the preview
// overlay is up it owns the keyboard, so pane focus does not move underneath it.
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

// updateKey is the page-local keymap with overlay-first routing: the
// preview overlay owns the keyboard (Esc closes first), then the live
// filter, then the page triggers, then the focused pane's navigation.
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
		// Inside a live filter, navigation runs on the non-printable
		// codes; printable keys (q, digits, :) go to the filter text.
		return s.updateNav(msg)
	}

	switch {
	case key.Matches(msg, s.nav.Filter):
		s.filtering = true

		return s, nil
	case key.Matches(msg, s.nav.Enter):
		// Enter is pane-bound: on STEPS it asks root for the step's
		// message preview, never runs the scenario; on the list it runs it.
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
// tracked identity. While STEPS holds focus the nav keys drive the step
// cursor over the pushed stream, never the list cursor.
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

// moveStepCursor moves the STEPS cursor over the pushed stream on the
// shared nav key set, clamped to the stream; unknown keys do nothing.
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

// stepViewport is the STEPS pane's visible line count, the page size for
// pgup/pgdn; it mirrors render's pane geometry so it matches what is drawn.
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

// Hints is the page context keymap; the narrow footer keeps run/export/tab
// primary (the router appends the global bindings). Esc pops (no-op at depth 1).
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
