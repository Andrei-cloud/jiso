// workers.go is the §H workers & stress page (SCR-508): the worker
// table (wireframe columns ID TYPE TRANSACTION STATUS THR INTERVAL/TPS
// RUNTIME OK/FAIL CIRCUIT), the TPS sparkline strip, one eighth-block
// progress row per active worker (the superfile Processes pattern,
// reusing the progress Bar), and the stress summary overlay. It is a
// reference type kept canonical in the router registry, so the table
// cursor survives page jumps. All data arrives via SetState from root —
// the page never touches internal/app and never reads the clock (the
// SCR-501 data-flow contract). Bus events drive every row change root-
// side; the page ignores EventMsg values entirely. The start forms and
// the stop-all confirm are root-owned modals (the §E/§G dialog
// machinery), never part of the page stack.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Workers is the §H page.
type Workers struct {
	th    *theme.Theme
	state WorkersState

	table *widgets.Table
	nav   workersNav

	// summaryOpen tracks the overlay per summary identity: a newly
	// pushed Summary re-arms the overlay, Esc closes it (the route
	// detail pattern — the overlay owns Esc first).
	summaryOpen    bool
	summaryShownID string

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// tableRect is the DRAWN workers table box (content-relative,
	// measured from the rendered string like every sectionRect) recorded
	// during the last render; the zero value means the table was not on
	// screen (the stress-summary overlay owns the body). It is the
	// geometry the wheel hit map registers under RegionWorkersTable.
	tableRect geom.Rect
}

// §H owns one wheel-scrollable region (the workers table) and implements
// the Task 8.2c region seam.
var _ Scroller = (*Workers)(nil)

// workersNav is the page keymap: start forms, stop, stop-all, and the
// pop/close chords.
type workersNav struct {
	OpenBg     key.Binding
	OpenStress key.Binding
	Stop       key.Binding
	StopAll    key.Binding
	Enter      key.Binding
	Cancel     key.Binding
	Pop        key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newWorkersNav() workersNav {
	nav := workersNav{
		OpenBg:     key.NewBinding(key.WithKeys("b")),
		OpenStress: key.NewBinding(key.WithKeys("t")),
		Stop:       key.NewBinding(key.WithKeys("k")),
		StopAll:    key.NewBinding(key.WithKeys("K")),
		Enter:      key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Cancel:     key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Pop:        key.NewBinding(key.WithKeys(theme.KeyEsc)),
	}
	// k is claimed by stop-sel, so it is dropped from the nav up display.
	nav.help = append(tableNavHelp(nav.Stop.Keys()...),
		actEntry("bgsend", nav.OpenBg),
		actEntry("stress", nav.OpenStress),
		actEntry("stop sel", nav.Stop),
		actEntry("stop all", nav.StopAll),
		actEntry("detail", nav.Enter),
		actEntry("back", nav.Pop),
	)

	return nav
}

// NewWorkers builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewWorkers(th *theme.Theme) *Workers {
	if th == nil {
		th = theme.Default()
	}
	w := &Workers{th: th, nav: newWorkersNav(), table: widgets.NewTable(th, workersMinTableWidth)}
	w.table.SetColumns(workersColumns())
	w.table.SetEmptyMessage(workersEmptyHint(th))

	return w
}

// ID reports the router id of this page (WorkersPageID — the wire-compat
// slot name "stress", kept for hotkey 4 and the palette ":stress" jump).
func (w *Workers) ID() string { return WorkersPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (w *Workers) Theme() *theme.Theme { return w.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (w *Workers) Size() (width, height int) { return w.width, w.height }

// SummaryOpen reports the stress-summary overlay state (tests).
func (w *Workers) SummaryOpen() bool { return w.summaryOpen }

// ScrollRegions publishes the table's drawn rect: the region exists
// exactly while the table is on screen, so the stress-summary overlay
// publishes nothing and the wheel over it stays inert.
func (w *Workers) ScrollRegions() []ScrollRegion {
	if w.tableRect.W <= 0 || w.tableRect.H <= 0 {
		return nil
	}

	return []ScrollRegion{{ID: RegionWorkersTable, Rect: w.tableRect}}
}

// ScrollRegion drives the SAME row window the pgup/pgdn keys move (via
// the table's cursor-dragging navigation and its own wheel window): the
// wheel's content-direction delta (d>0 = down) passes straight into
// Table.ScrollBy, which clamps at both ends. The window is sized to the
// real pane budget (content minus the head, TPS strip, and PROGRESS
// rows) on every render, so it cannot go stale.
func (w *Workers) ScrollRegion(id string, d int) bool {
	if id != RegionWorkersTable || w.tableRect.W <= 0 {
		return false
	}
	w.table.ScrollBy(d)

	return true
}

// OpenSummary re-arms the stress-summary overlay for the summary the
// page already carries (proposal 05 §3: the §A LAST STRESS card's
// "Stress summary" quick action reopens the existing §H overlay for the
// same worker id — the same state the WorkerStopped fold stamped, no
// second summary implementation).
func (w *Workers) OpenSummary() {
	if w.state.Summary != nil {
		w.summaryOpen = true
		w.summaryShownID = w.state.Summary.WorkerID
	}
}

// SelectedID reports the worker id under the table cursor ("" when the
// table is empty).
func (w *Workers) SelectedID() string {
	if len(w.state.Workers) == 0 {
		return ""
	}
	i := min(w.table.Cursor(), len(w.state.Workers)-1)

	return w.state.Workers[i].ID
}

// SetState replaces the rendered snapshot (root pushes it on boot, on
// every bus event, and on every runtime tick). The table is recomposed
// over the new rows and the cursor clamped; a Summary whose WorkerID
// differs from the last shown one re-arms the overlay.
func (w *Workers) SetState(state WorkersState) {
	prev := w.table.Cursor()
	w.state = state

	rows := make([]widgets.Row, len(state.Workers))
	for i, r := range state.Workers {
		rows[i] = widgets.Row{
			r.ID, r.Type, dashIf(w.th, r.Txn), w.statusCell(r.Status),
			r.Thr, dashIf(w.th, r.IntervalTPS), r.Runtime, r.OKFail, dashIf(w.th, r.Circuit),
		}
	}
	w.table.SetRows(rows)
	if n := len(state.Workers); n > 0 {
		w.table.SetCursor(min(prev, n-1))
	}

	if state.Summary != nil && state.Summary.WorkerID != w.summaryShownID {
		w.summaryOpen = true
	}
	if state.Summary == nil {
		w.summaryOpen = false
	}
	if w.summaryOpen && state.Summary != nil {
		w.summaryShownID = state.Summary.WorkerID
	}
}

// Update routes sizes, keys, and the overlay close; bus events are
// root-side truth and are ignored with a nil command (root folds them
// into the next SetState).
func (w *Workers) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return w.updateKey(msg)
	}

	return w, nil
}

// updateKey is the page-local keymap: the summary overlay owns Esc
// first, then the start/stop triggers, then table navigation.
func (w *Workers) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if w.summaryOpen {
		if key.Matches(msg, w.nav.Cancel) {
			w.summaryOpen = false
		}

		return w, nil // the overlay owns the keyboard wholesale
	}

	switch {
	case key.Matches(msg, w.nav.OpenBg):
		return w, func() tea.Msg { return WorkersOpenFormMsg{Kind: "bgsend"} }
	case key.Matches(msg, w.nav.OpenStress):
		return w, func() tea.Msg { return WorkersOpenFormMsg{Kind: "stress"} }
	case key.Matches(msg, w.nav.Stop):
		if id := w.SelectedID(); id != "" {
			return w, func() tea.Msg { return WorkersStopMsg{ID: id} }
		}

		return w, nil
	case key.Matches(msg, w.nav.StopAll):
		return w, func() tea.Msg { return WorkersStopAllMsg{} }
	case key.Matches(msg, w.nav.Pop):
		return w, func() tea.Msg { return WorkersPopMsg{} }
	default:
		next, cmd := w.table.Update(msg)
		w.table = next

		return w, cmd
	}
}

// Hints is the §H context keymap; the four worker actions are primary so
// the narrow footer keeps them (the router appends the global bindings).
func (w *Workers) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: "b", Desc: "bgsend", Primary: true},
		{Key: "t", Desc: "stress", Primary: true},
		{Key: "k", Desc: "stop sel", Primary: true},
		{Key: "K", Desc: "stop all", Primary: true},
		{Key: "j/up", Desc: "nav"},
		{Key: theme.KeyEsc, Desc: "back"},
	}
}
