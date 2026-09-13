// settings.go is the §L settings page (SCR-512): a two-column
// key/value grid of the live session config (wireframe §L) with a
// page-local edit mode — Enter opens the focused field as a text
// input, Enter commits, Esc reverts the field — and the [w] save
// confirm overlay rendered from root-derived state. It is a reference
// type kept canonical in the router registry, so cursor/draft state
// survives page jumps. All data arrives via SetState from root; the
// page never touches internal/app, never reads the clock, and never
// writes a file. Below frame.FullWidth the grid stacks into a single
// column.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// Settings is the §L page.
type Settings struct {
	th    *theme.Theme
	state SettingsState

	cursor  int
	editing bool
	editBuf string
	editRow int // row index the buffer belongs to (draft bookkeeping)

	// drafts holds committed-but-not-yet-reflected values by key (the
	// attempted text stays visible beside its inline error until the
	// next snapshot agrees, then drops out).
	drafts map[string]string

	width  int
	height int
}

// settingsNav is the page keymap: grid navigation, edit/commit/revert,
// save/confirm/cancel, refresh, and pop.
type settingsNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Refresh   key.Binding
	Save      key.Binding
	PickFile  key.Binding
	Down      key.Binding
	Up        key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newSettingsNav() settingsNav {
	nav := settingsNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Refresh:   key.NewBinding(key.WithKeys("r")),
		Save:      key.NewBinding(key.WithKeys("w")),
		PickFile:  key.NewBinding(key.WithKeys("f")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
	}
	nav.help = []HelpEntry{
		navEntry("move", nav.Up, nav.Down),
		actEntry("edit field", nav.Enter),
		actEntry("save", nav.Save),
		actEntry("reload", nav.Refresh),
		actEntry("pick file", nav.PickFile),
		actEntry("revert / back", nav.Cancel),
	}

	return nav
}

// NewSettings builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewSettings(th *theme.Theme) *Settings {
	if th == nil {
		th = theme.Default()
	}

	return &Settings{th: th, drafts: map[string]string{}}
}

// ID reports the router id (SettingsPageID; palette ":settings").
func (s *Settings) ID() string { return SettingsPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Settings) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Settings) Size() (width, height int) { return s.width, s.height }

// Cursor reports the grid row cursor (tests).
func (s *Settings) Cursor() int { return s.cursor }

// Editing reports the field edit mode (tests).
func (s *Settings) Editing() bool { return s.editing }

// EditBuffer exposes the in-progress field text (tests).
func (s *Settings) EditBuffer() string { return s.editBuf }

// Filter is the §B/§K-compatible accessor stub: §L has no list filter
// (the root test helpers probe it uniformly); it always reports off.
func (s *Settings) Filter() (string, bool) { return "", false }

// ClaimsKeyboard implements KeyboardClaimer: the edit buffer (paths
// full of q, digits, and ":") and the save overlay own the keyboard
// while active; Ctrl+C stays global.
func (s *Settings) ClaimsKeyboard() bool { return s.editing || s.state.Save != nil }

// RowValue is the effective display value of row i: the edit buffer
// while editing that row, else a pending draft (an attempted commit
// the snapshot has not accepted), else the snapshot value.
func (s *Settings) RowValue(i int) string {
	row, ok := s.row(i)
	if !ok {
		return ""
	}
	if s.editing && s.editRow == i {
		return s.editBuf
	}
	if d, has := s.drafts[row.Key]; has && d != row.Value {
		return d
	}

	return row.Value
}

// row fetches one snapshot row.
func (s *Settings) row(i int) (SettingsRow, bool) {
	if i < 0 || i >= len(s.state.Rows) {
		return SettingsRow{}, false
	}

	return s.state.Rows[i], true
}

// SetState replaces the rendered snapshot (root pushes it on load,
// after every apply, and around the save overlay). Page-local state
// survives: the cursor clamps to the row count and drafts whose
// snapshot value now agrees are dropped.
func (s *Settings) SetState(state SettingsState) {
	s.state = state
	if n := len(state.Rows); n > 0 {
		s.cursor = min(s.cursor, n-1)
	}
	for key, draft := range s.drafts {
		for _, r := range state.Rows {
			if r.Key == key && r.Value == draft {
				delete(s.drafts, key)
			}
		}
	}
}

// Update routes sizes and keys; bus events are root-side truth and
// are ignored with a nil command.
func (s *Settings) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// updateKey routes the keyboard: the save overlay owns it first, then
// the edit buffer, then the grid.
func (s *Settings) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if s.state.Save != nil {
		switch {
		case key.Matches(msg, s.nav().Cancel):
			return s, func() tea.Msg { return SettingsSaveCancelMsg{} }
		case key.Matches(msg, s.nav().Save):
			return s, func() tea.Msg { return SettingsSaveConfirmMsg{} }
		}

		return s, nil
	}
	if s.editing {
		return s.updateEdit(msg)
	}

	nav := s.nav()
	switch {
	case key.Matches(msg, nav.Enter):
		if _, ok := s.row(s.cursor); ok {
			value := s.RowValue(s.cursor)
			s.editing, s.editRow, s.editBuf = true, s.cursor, value
		}
	case key.Matches(msg, nav.Down):
		s.moveCursor(1)
	case key.Matches(msg, nav.Up):
		s.moveCursor(-1)
	case key.Matches(msg, nav.Save):
		return s, func() tea.Msg { return SettingsSaveMsg{} }
	case key.Matches(msg, nav.Refresh):
		return s, func() tea.Msg { return SettingsRefreshMsg{} }
	case key.Matches(msg, nav.PickFile):
		if row, ok := s.row(s.cursor); ok && row.Pickable {
			return s, func() tea.Msg { return SettingsPickFileMsg{Key: row.Key} }
		}
	case key.Matches(msg, nav.Cancel):
		return s, func() tea.Msg { return SettingsPopMsg{} }
	}

	return s, nil
}

// updateEdit is the focused-field text input: printable/backspace
// edit the buffer, Enter commits, Esc reverts the field.
func (s *Settings) updateEdit(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	nav := s.nav()
	switch {
	case key.Matches(msg, nav.Enter):
		key := s.state.Rows[s.editRow].Key
		value := s.editBuf
		s.drafts[key] = value
		s.editing = false

		return s, func() tea.Msg { return SettingsCommitMsg{Key: key, Value: value} }
	case key.Matches(msg, nav.Cancel):
		s.editing = false
		delete(s.drafts, s.state.Rows[s.editRow].Key)
		s.editBuf = ""
	case key.Matches(msg, nav.Backspace):
		s.editBuf = dropLastRune(s.editBuf)
	default:
		if r, ok := printableRune(msg.Text); ok {
			s.editBuf += string(r)
		}
	}

	return s, nil
}

// moveCursor clamps the row cursor (j/k/down/up walk the §L rows in
// wireframe order; on the two-column grid this snakes row-pair by
// row-pair left, right, next pair).
func (s *Settings) moveCursor(delta int) {
	n := len(s.state.Rows)
	if n == 0 {
		return
	}
	s.cursor = max(0, min(n-1, s.cursor+delta))
}

// nav builds the keymap lazily (value type; no state).
func (s *Settings) nav() settingsNav { return newSettingsNav() }

// Hints is the §L context keymap; edit/save/back are primary so the
// narrow footer keeps them (the router appends the global bindings).
func (s *Settings) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "edit field", Primary: true},
		{Key: "w", Desc: "save", Primary: true},
		{Key: "r", Desc: "reload"},
		{Key: theme.KeyEsc, Desc: "revert/back", Primary: true},
	}
}
