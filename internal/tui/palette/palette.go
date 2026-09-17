package palette

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	shellquote "github.com/kballard/go-shellquote"

	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// maxListRows caps the match list height; the frame truncates anything
// taller, so the palette never pushes chrome off screen.
const maxListRows = 8

// keyMap owns every key the palette swallows while open: cancel, submit,
// backspace, and list navigation (arrows + j/k, design contract "vim
// aliases where no text field owns input" — here the palette owns the
// text field AND the list, matching fzf's trade-off).
type keyMap struct {
	Cancel    key.Binding
	Submit    key.Binding
	Backspace key.Binding
	Up        key.Binding
	Down      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Submit:    key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
	}
}

// Model is the palette overlay widget: an input line plus a virtualized
// widgets.List of matches, live-filtered as the query changes. The root
// model owns open/close (it keeps the keyboard); Closed reports esc and
// a successful submit reports a non-nil tea.Cmd carrying the action Msg.
//
// Zero value is not usable; build with New.
type Model struct {
	theme   *theme.Theme
	matcher *Matcher
	keys    keyMap
	list    *widgets.List
	// hintColumn is the label column every row's hotkey badge is padded to,
	// sized once from the whole registry (see New).
	hintColumn int
	query      string
	closed     bool
	width      int
	height     int
}

// New builds the widget sized to width x height (height = rows available
// to the whole panel, input line included).
func New(th *theme.Theme, matcher *Matcher, width, height int) *Model {
	m := &Model{
		theme:   th,
		matcher: matcher,
		keys:    newKeyMap(),
		width:   max(width, 2),
		height:  max(height, 2),
	}
	m.list = widgets.NewList(th, m.width, min(max(m.height-1, 1), maxListRows))
	m.list.SetEmptyMessage("no matching actions")

	titles := make([]string, 0, len(m.matcher.All()))
	for _, a := range m.matcher.All() {
		titles = append(titles, a.Title)
	}
	m.hintColumn = widgets.HintColumn(titles)
	m.refilter()

	return m
}

// SetSize resizes the panel (and its list) on WindowSizeMsg.
func (m *Model) SetSize(width, height int) {
	if width >= 2 {
		m.width = width
	}
	if height >= 2 {
		m.height = height
	}
	m.list.SetSize(m.width, min(max(m.height-1, 1), maxListRows))
}

// Closed reports whether esc closed the palette.
func (m *Model) Closed() bool { return m.closed }

// Close marks the palette closed (used by the router after a submit).
func (m *Model) Close() { m.closed = true }

// Query returns the raw input line.
func (m *Model) Query() string { return m.query }

// Cursor reports the selected match index (0 when empty).
func (m *Model) Cursor() int { return m.list.Cursor() }

// SelectedActionID reports the ID of the list's current selection, or
// false when nothing is selected. Read-only accessor for the router's
// lifecycle log: it names the executed action on submit without
// reaching into the widget's internals.
func (m *Model) SelectedActionID() (string, bool) {
	item, ok := m.list.Selected()
	if !ok {
		return "", false
	}

	a, ok := item.Data.(Action)
	if !ok {
		return "", false
	}

	return a.ID, true
}

// Update is the pure transition. While open the palette owns the
// keyboard: digits, letters, and unknown keys never escape to the
// router (typing "1" must not jump to page 1). Returns a non-nil cmd
// only on a successful submit; the cmd yields the action's Msg.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(km, m.keys.Cancel):
		m.closed = true

		return m, nil

	case key.Matches(km, m.keys.Submit):
		return m.submit()

	case key.Matches(km, m.keys.Backspace):
		q := []rune(m.query)
		if len(q) > 0 {
			m.query = string(q[:len(q)-1])
			m.refilter()
		}

		return m, nil

	case key.Matches(km, m.keys.Up):
		m.list.SetCursor(m.list.Cursor() - 1)

		return m, nil

	case key.Matches(km, m.keys.Down):
		m.list.SetCursor(m.list.Cursor() + 1)

		return m, nil

	case km.Text != "" && km.Mod&tea.ModCtrl == 0:
		m.query += km.Text
		m.refilter()

		return m, nil

	default:
		return m, nil // swallow everything else: the palette owns input
	}
}

// submit runs the selected action. Zero matches is a no-op (the palette
// stays open); the action's Msg travels back through a tea.Cmd.
func (m *Model) submit() (*Model, tea.Cmd) {
	item, ok := m.list.Selected()
	if !ok {
		return m, nil
	}
	action, ok := item.Data.(Action)
	if !ok || action.Run == nil {
		return m, nil
	}
	_, args := splitCommand(m.query)
	msg := action.Run(args)

	return m, func() tea.Msg { return msg }
}

// refilter re-runs the matcher on the command word (first token) so
// ":tx --flag value" keeps filtering by "tx" while the trailing
// tokens are reserved as args, and moves the cursor back to the top.
func (m *Model) refilter() {
	cmdWord, _ := splitCommand(m.query)
	matches := m.matcher.Search(cmdWord, 0)

	items := make([]widgets.Item, 0, len(matches))
	for _, a := range matches {
		items = append(items, widgets.Item{
			Label: widgets.LabelWithHint(m.theme, a.Title, strings.Join(a.Hints, " "), m.hintColumn),
			Data:  a,
		})
	}
	m.list.SetItems(items)
	m.list.SetCursor(0)
}

// splitCommand tokenizes the palette line with go-shellquote — the same
// tokenizer internal/cli/lexer wraps (internal/tui may not import
// internal/cli, so the palette calls the library directly). The first
// token selects the action, the rest are its args. An unlexable line
// (unbalanced quotes) yields the raw query and no args, which simply
// fails to match and keeps enter a no-op.
func splitCommand(line string) (cmd string, args []string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", nil
	}
	parts, err := shellquote.Split(trimmed)
	if err != nil || len(parts) == 0 {
		return trimmed, nil
	}
	if len(parts) > 1 {
		return parts[0], parts[1:]
	}

	return parts[0], nil
}

// View renders the panel: input line, then the match list window.
func (m *Model) View() string {
	_, args := splitCommand(m.query)
	count := m.list.Len()

	head := m.theme.Accent.Render(":" + m.query)
	if count > 0 {
		head += m.theme.Deemphasized.Render("  " + itoa(count) + " match" + plural(count))
	}
	if len(args) > 0 {
		head += m.theme.Deemphasized.Render("  args: " + strings.Join(args, " "))
	}

	return head + "\n" + m.list.View()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "es"
}
