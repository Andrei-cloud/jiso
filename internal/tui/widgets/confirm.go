package widgets

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// Messages exchanged between a dialog and its page. The dialog never
// blocks: decisions arrive as tea.Cmd results, so the page stays in the
// normal Update loop.
type (
	// WaitResultMsg marks the dialog as open and waiting for a decision
	// (delivered by Confirm.Open's command).
	WaitResultMsg struct{ Question string }
	// ConfirmedMsg is the explicit-yes result.
	ConfirmedMsg struct{ Question string }
	// CancelledMsg is the no/esc/enter result (default is No).
	CancelledMsg struct{ Question string }
)

// phase is the dialog state machine: open -> done (terminal).
type phase int

const (
	phaseOpen phase = iota
	phaseDone
)

// ConfirmDialog is the §N3 confirm widget: an overlay-style renderer that
// returns a string the page composes over its content. Destructive
// actions only; default is No — only an explicit y confirms, while n,
// esc, and enter all cancel. After a decision Pending is false, View
// renders empty (the page drops the overlay), and further keys are
// ignored.
type ConfirmDialog struct {
	theme    *theme.Theme
	question string
	state    phase

	yes, no, cancel, defNo key.Binding
}

// NewConfirmDialog builds a pending dialog for question. The help texts
// on the decision bindings are the DecisionKeys report — the owner
// badges them into its hotkey strip (one surface per state).
func NewConfirmDialog(th *theme.Theme, question string) *ConfirmDialog {
	return &ConfirmDialog{
		theme:    th,
		question: question,
		yes:      key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "confirm")),
		no:       key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n", "cancel")),
		cancel:   key.NewBinding(key.WithKeys(theme.KeyEsc), key.WithHelp(theme.KeyEsc, "cancel")),
		defNo:    key.NewBinding(key.WithKeys(theme.KeyEnter)),
	}
}

// Question reports the dialog's question.
func (m *ConfirmDialog) Question() string { return m.question }

// Pending reports whether the dialog still owns the keyboard.
func (m *ConfirmDialog) Pending() bool { return m.state == phaseOpen }

// Open marks the dialog open and returns the command that announces it
// to the page as a WaitResultMsg.
func (m *ConfirmDialog) Open() tea.Cmd {
	m.state = phaseOpen
	q := m.question

	return func() tea.Msg { return WaitResultMsg{Question: q} }
}

// Update is the state machine: y confirms, n/esc/enter cancel (default
// No), any other key is swallowed while pending. A decision emits its
// typed message via the returned command and closes the dialog.
func (m *ConfirmDialog) Update(msg tea.Msg) (*ConfirmDialog, tea.Cmd) {
	if m.state != phaseOpen {
		return m, nil
	}
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	q := m.question
	switch {
	case key.Matches(km, m.yes):
		m.state = phaseDone

		return m, func() tea.Msg { return ConfirmedMsg{Question: q} }
	case key.Matches(km, m.no), key.Matches(km, m.cancel), key.Matches(km, m.defNo):
		m.state = phaseDone

		return m, func() tea.Msg { return CancelledMsg{Question: q} }
	default:
		return m, nil
	}
}

// DecisionKey is one decision key with its help text, for the owner's
// hotkey strip (widgets keeps frame's hint type behind its import fence).
type DecisionKey struct{ Key, Desc string }

// DecisionKeys reports the decision keys (y confirm, n cancel, esc
// cancel) derived from the bindings Update acts on; the owner styles
// them. Enter still cancels but is not advertised — n/esc carry the
// default-No answer.
func (m *ConfirmDialog) DecisionKeys() []DecisionKey {
	yes, no, cancel := m.yes.Help(), m.no.Help(), m.cancel.Help()

	return []DecisionKey{{yes.Key, yes.Desc}, {no.Key, no.Desc}, {cancel.Key, cancel.Desc}}
}

// View renders the overlay block: the question alone. The decision keys
// ride the owner's hotkey strip badged (one hotkey surface per state);
// after a decision it renders empty.
func (m *ConfirmDialog) View() string {
	if m.state != phaseOpen {
		return ""
	}

	return m.theme.TextPrimary.Render(m.question)
}
