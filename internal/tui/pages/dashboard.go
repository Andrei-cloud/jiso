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

// connectActionID is the palette action id 'c' preselects once SCR-505
// registers the connect dialog action; until then 'c' parks the cursor
// on the top row of the quick-actions list.
const connectActionID = "connect"

// disconnectActionID is the palette action the 'D' quick key and the §A
// quick-actions row share (TUI-514). The row is listed only while
// DashboardState.HasConnection marks a live connection — the page learns
// that from root's snapshot, never from the app.
const disconnectActionID = "disconnect"

// sendWizardActionID is the proposal-04 §B quick-action row: "Send
// transaction (s)" opens the send wizard. The page lists it only while a
// connection is live ("send should appear only when connected"); the
// palette keeps the command always available, where the wizard itself
// opens the connect step first.
const sendWizardActionID = "send-wizard"

// The scenario runner and the stress workers both put messages on the
// wire, so their quick-action rows are connection-dependent too (UAT:
// options that only work on a live link stay hidden while offline).
const (
	gotoScenariosActionID = "goto.scenarios"
	gotoWorkersActionID   = "goto.workers"
)

// lastSendActionID reopens the last completed §D snapshot; the row only
// appears after a send has run (UAT). Proposal 05 §3: the LAST SEND
// card's "enter open" affordance rides this same row — Enter on the row
// dispatches LastSendViewMsg, the router's single viewLastSend path.
const lastSendActionID = "last-send"

// stressSummaryActionID is the proposal-05 §3 LAST STRESS reopen row:
// "Stress summary" appears only while a completed stress run exists
// (DashboardState.LastStress != nil) and dispatches
// LastStressSummaryMsg — the router's single summary-overlay path. No
// card-focus system: the row is the affordance (the dashboard claims no
// keys beyond what already existed).
const stressSummaryActionID = "last-stress"

// Dashboard is the §A landing page: the proposal-05 §3 information grid
// — CONNECTION, MOCK SERVER, LAST SEND, LAST STRESS, SERVER LOG, SESSION
// and QUICK ACTIONS cards over the palette action registry. It is a
// reference type: the router keeps one canonical instance in its
// registry, so the quick-actions cursor survives page jumps. All app
// data arrives via SetState from the root model — the page never touches
// internal/app and never reads the clock. The EVENT FEED pane is gone
// (proposal 05 §3): its content lives in the CONNECTION card, the
// timestamped status strip and the live SERVER LOG card.
type Dashboard struct {
	th    *theme.Theme
	state DashboardState

	actions *widgets.List
	nav     dashNav

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect
}

// dashNav is the page's keymap: the enter binding that runs the selected
// quick action and the 'D' quick key that drops the live connection
// (TUI-514 — uppercase counterpart of the global 'c' connect hotkey;
// nothing on this page or in the global keymap binds it). The feed
// scroll bindings left with the EVENT FEED pane; j/k reach the
// quick-actions list through the shared widgets nav keys (registered via
// tableNavHelp, the drift-pinned source).
type dashNav struct {
	Run        key.Binding
	Disconnect key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newDashNav() dashNav {
	nav := dashNav{
		Run:        key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Disconnect: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "disconnect")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("run action", nav.Run),
		actEntry("disconnect", nav.Disconnect),
	)

	return nav
}

// NewDashboard builds the page. A nil theme selects theme.Default()
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

// SetState replaces the rendered snapshot (root pushes it on boot, on
// every Update, and after bridge events). The quick-actions list is
// rebuilt from state.Actions, keeping the cursor in range; the
// disconnect row is listed only while a connection is live (TUI-514 —
// the palette keeps the command at all times, where it behaves sanely
// without one), and the last-send / last-stress reopen rows only while
// their cards have content (UAT / proposal 05 §3).
func (d *Dashboard) SetState(state DashboardState) {
	d.state = state

	labels := make([]string, 0, len(state.Actions))
	kept := make([]palette.Action, 0, len(state.Actions))
	for _, a := range state.Actions {
		switch a.ID {
		case connectActionID:
			if state.HasConnection {
				continue // the row toggles to Disconnect (proposal 04)
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
			// UAT: rows whose action only works on a live link stay
			// hidden while offline (the palette still carries them).
			if !state.HasConnection {
				continue
			}
		case lastSendActionID:
			// UAT: the row only exists once a send has completed.
			if state.LastSend == nil {
				continue
			}
		case stressSummaryActionID:
			// Proposal 05 §3: the reopen row only exists once a stress
			// run has completed.
			if state.LastStress == nil {
				continue
			}
		}
		labels = append(labels, a.Title)
		kept = append(kept, a)
	}

	// One column for the whole list, so every badge lands on the same cell.
	// UAT round 5: rows carry NO key badges — the digits duplicate the
	// footer legend and the letters duplicate the page's own footer hints;
	// the rows are selected with Enter, not with their advertised keys.
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

// Update routes sizes and the page-local keys. Everything else — bus
// events (the EVENT FEED pane is gone), pane-focus toggles (the page has
// a single focusable list), unrelated msgs — is ignored with a nil
// command.
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
// no-op), and every other key (j/k, g/G, home/end) scrolls the
// quick-actions list. The page-local "c parks the cursor on the connect
// row" handler is gone (UAT round 5): the global c binding always claims
// the key first, so the handler was production-dead.
func (d *Dashboard) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, d.nav.Run):
		return d, d.runSelectedAction()
	case key.Matches(msg, d.nav.Disconnect):
		out := palette.DisconnectMsg{}

		return d, func() tea.Msg { return out }
	default:
		var cmd tea.Cmd
		d.actions, cmd = d.actions.Update(msg)

		return d, cmd
	}
}

// runSelectedAction dispatches the selected quick action's Msg through
// its Run (the same contract the palette uses); the router interprets
// the Msg. This is the LAST SEND ("View last send" → LastSendViewMsg)
// and LAST STRESS ("Stress summary" → LastStressSummaryMsg) reopen
// path — the cards' "enter" affordances are the rows, never a parallel
// card-focus mechanism.
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
// footer keeps them (the router appends the global bindings). D is the
// §A disconnect quick key (TUI-514): always advertised — pressing it
// without a connection is the root's sane info no-op, never a dead key.
// The "tab focus pane" hint left with the EVENT FEED pane: the page has
// one focusable list, so j/k scroll it and no pane toggle exists.
func (d *Dashboard) Hints() []frame.KeyHint {
	hints := []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "run", Primary: true},
	}
	if d.state.HasConnection {
		// Connected (proposal 04): c drops the link and s opens the send
		// wizard; the old D drop row is redundant and stays out to keep
		// the footer inside its pinned budget.
		hints = append(hints,
			frame.KeyHint{Key: "c", Desc: "disconnect", Primary: true},
			frame.KeyHint{Key: "s", Desc: "send", Primary: true})
	} else {
		hints = append(hints,
			frame.KeyHint{Key: "c", Desc: "connect", Primary: true},
			// "drop" (not "disconnect"): with the two page primaries and
			// the global ones, "D disconnect" would push "q quit / back"
			// past the 90-col footer budget frame_view_test.go pins.
			frame.KeyHint{Key: "D", Desc: "drop"})
	}
	// The wireframe leads the page legend with the connect/disconnect key.
	lead, run := hints[1], hints[0]
	hints = append(hints, frame.KeyHint{Key: theme.KeyNavJK, Desc: hintScroll})

	return append([]frame.KeyHint{lead, run}, hints[2:]...)
}
