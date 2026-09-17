package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// connectActionID is the palette action id of the connect row.
const connectActionID = "connect"

// disconnectActionID is the palette action the 'D' quick key and the §A
// quick-actions row share. The row is listed only while root's snapshot
// marks a live connection — the page never learns that from the app.
const disconnectActionID = "disconnect"

// sendWizardActionID opens the send wizard. The page lists it only while
// a connection is live; the palette keeps the command always available,
// where the wizard itself opens the connect step first.
const sendWizardActionID = "send-wizard"

// The scenario runner and the stress workers both put messages on the
// wire, so their quick-action rows are connection-dependent too.
const (
	gotoScenariosActionID = "goto.scenarios"
	gotoWorkersActionID   = "goto.workers"
)

// lastSendActionID reopens the last completed §D snapshot; the row only
// appears after a send has run, and Enter on it dispatches
// LastSendViewMsg.
const lastSendActionID = "last-send"

// stressSummaryActionID is the LAST STRESS reopen row: "Stress summary"
// appears only while a completed stress run exists and dispatches
// LastStressSummaryMsg. There is no card-focus system — the row is the
// affordance.
const stressSummaryActionID = "last-stress"

// Dashboard is the §A landing page: an information grid of CONNECTION,
// MOCK SERVER, LAST SEND, LAST STRESS, SERVER LOG, SESSION and QUICK
// ACTIONS cards over the palette action registry. It is a reference type
// kept canonical in the router registry, so the quick-actions cursor
// survives page jumps. All app data arrives via SetState from the root
// model — the page never touches internal/app and never reads the clock.
type Dashboard struct {
	th    *theme.Theme
	state DashboardState

	actions *widgets.List
	nav     dashNav

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// sections records the rect of every widgets.Section this page drew
	// during the last render, in draw order, content-relative.
	sections []geom.Rect
}

// dashNav is the page's keymap: the enter binding that runs the selected
// quick action, the 'D' quick key that drops the live connection (the
// uppercase counterpart of the global 'c' connect hotkey, bound nowhere
// else), and the 't' quick key that opens the stress wizard. j/k reach
// the quick-actions list through the shared widgets nav keys.
type dashNav struct {
	Run          key.Binding
	Disconnect   key.Binding
	StressWizard key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newDashNav() dashNav {
	nav := dashNav{
		Run:          key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Disconnect:   key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "disconnect")),
		StressWizard: key.NewBinding(key.WithKeys("t")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("run action", nav.Run),
		actEntry("disconnect", nav.Disconnect),
		actEntry("stress run", nav.StressWizard),
	)

	return nav
}

// NewDashboard builds the page. A nil theme selects theme.Default
// (production); golden tests inject an explicit NewWith profile.
func NewDashboard(th *theme.Theme) *Dashboard {
	if th == nil {
		th = theme.Default()
	}
	d := &Dashboard{th: th, nav: newDashNav()}
	d.actions = widgets.NewList(th, 20, 5)
	d.actions.SetEmptyMessage("no actions registered - open the palette with :")

	return d
}

// ID reports the router slot this page fills ("status", the boot page).
func (d *Dashboard) ID() string { return DashboardPageID }

// Theme exposes the resolved theme (layout helpers and tests).
func (d *Dashboard) Theme() *theme.Theme { return d.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (d *Dashboard) Size() (width, height int) { return d.width, d.height }

// SetState replaces the rendered snapshot and rebuilds the quick-actions
// list from state.Actions, keeping the cursor in range. Connection-
// dependent rows (connect/disconnect/send/goto) list only with a live
// connection; the reopen rows only while their cards have content.
func (d *Dashboard) SetState(state DashboardState) {
	d.state = state

	labels := make([]string, 0, len(state.Actions))
	kept := make([]palette.Action, 0, len(state.Actions))
	for _, a := range state.Actions {
		switch a.ID {
		case connectActionID:
			if state.HasConnection {
				continue // the row toggles to Disconnect
			}
		case disconnectActionID:
			if !state.HasConnection {
				continue
			}
		case sendWizardActionID:
			if !state.HasConnection {
				continue
			}
		case gotoScenariosActionID, gotoWorkersActionID:
			// Rows whose action needs a live link stay hidden while
			// offline (the palette still carries them).
			if !state.HasConnection {
				continue
			}
		case lastSendActionID:
			// The row only exists once a send has completed.
			if state.LastSend == nil {
				continue
			}
		case stressSummaryActionID:
			// The reopen row only exists once a stress run completed.
			if state.LastStress == nil {
				continue
			}
		}
		labels = append(labels, a.Title)
		kept = append(kept, a)
	}

	// One column for the whole list, so every badge lands on the same
	// cell. Rows carry no key badges: they are selected with Enter, not
	// with their advertised keys.
	column := widgets.HintColumn(labels)
	items := make([]widgets.Item, 0, len(kept))
	for _, a := range kept {
		items = append(items, widgets.Item{
			Label: widgets.LabelWithHint(d.th, a.Title, "", column),
			Data:  a,
		})
	}

	d.actions.SetItems(items)
}

// Update routes sizes and the page-local keys; everything else is
// ignored with a nil command.
func (d *Dashboard) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return d.updateKey(msg)
	}

	return d, nil
}

// updateKey is the page-local keymap: enter runs the selected action, D
// emits the disconnect Msg (root decides: leg, confirm, or sane info
// no-op), t emits the stress-wizard open Msg (the page never opens
// anything itself), and every other key scrolls the quick-actions list.
func (d *Dashboard) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, d.nav.Run):
		return d, d.runSelectedAction()
	case key.Matches(msg, d.nav.Disconnect):
		out := palette.DisconnectMsg{}

		return d, func() tea.Msg { return out }
	case key.Matches(msg, d.nav.StressWizard):
		// The exact message Workers.updateKey emits for its own t, so
		// root opens the identical wizard from either page.
		return d, func() tea.Msg { return WorkersOpenFormMsg{Kind: WorkerModeStress} }
	default:
		var cmd tea.Cmd
		d.actions, cmd = d.actions.Update(msg)

		return d, cmd
	}
}

// runSelectedAction dispatches the selected quick action's Msg through
// its Run (the same contract the palette uses); the router interprets it.
// This is the last-send/last-stress reopen path — the cards' "enter"
// affordances are the rows, never a parallel card-focus mechanism.
func (d *Dashboard) runSelectedAction() tea.Cmd {
	item, ok := d.actions.Selected()
	if !ok {
		return nil
	}
	a, ok := item.Data.(palette.Action)
	if !ok || a.Run == nil {
		return nil
	}
	out := a.Run(nil)
	if out == nil {
		return nil
	}

	return func() tea.Msg { return out }
}

// Hints is the §A context keymap; c/enter are primary so the narrow
// footer keeps them. D is always advertised: pressing it without a
// connection is the root's sane info no-op, never a dead key. The page
// has one focusable list, so j/k scroll it and no pane toggle exists.
func (d *Dashboard) Hints() []frame.KeyHint {
	hints := []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "run", Primary: true},
	}
	if d.state.HasConnection {
		// Connected: c drops the link and s opens the send wizard; the
		// redundant D row stays out to keep the footer in budget.
		hints = append(hints,
			frame.KeyHint{Key: "c", Desc: "disconnect", Primary: true},
			frame.KeyHint{Key: "s", Desc: "send", Primary: true})
	} else {
		hints = append(hints,
			frame.KeyHint{Key: "c", Desc: "connect", Primary: true},
			// "drop" (not "disconnect"): the fuller label would push
			// "q quit / back" past the pinned 90-col footer budget.
			frame.KeyHint{Key: "D", Desc: "drop"})
	}
	lead, run := hints[1], hints[0]
	hints = append(hints, frame.KeyHint{Key: theme.KeyNavJK, Desc: hintScroll})
	// Advertise the global F9 selection toggle here so the obscure key is
	// findable on the landing page. It is a display-only legend cell for
	// the click hit map (synthKeyPress spells no "F9"), never a wrong
	// press.
	f9 := frame.KeyHint{Key: "F9", Desc: "select", Primary: true}

	return append([]frame.KeyHint{lead, run, f9}, hints[2:]...)
}
