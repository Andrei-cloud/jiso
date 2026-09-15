// hitmap_focus.go is the click-to-focus leg of the mouse hit machinery
// (UAT round 8, Task 8.5, finding 9): the form/wizard modals measure
// their drawn field rows and step-rail labels (FieldRowHits/RailRowHits
// next to each View, pages side), the §J analyze page publishes its rail
// through pages.Focuser, buildHitMap registers all of them as focusHits
// (page geometry first, modals over the page, the §M box and the picker
// rows over the modals), and handleFocusMsg routes a resolved
// focusMsg{region, index} to the region owner's OWN focus/step
// navigation: a field click is ConnectDialog.SetFocus (navigate mode —
// typing still enters edit mode, the 4.2 two-mode model), a rail click
// is the wizard's own step walk (backward = free revisit, forward = the
// current step's Enter leg with its validation, current step = inert).
// The click never invents a transition the keyboard does not have.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// Modal-owned focus regions ("<owner>:<pane>" like the page ids). The
// §J analyze rail is page geometry and publishes its own id
// (pages.RegionAnalyzeRail) through pages.Focuser; these four name the
// centered modals whose geometry the root itself composes.
const (
	regionConnectForm = "connect:form" // §E dialog fields (m.dlg)
	regionServerForm  = "server:form"  // §G start form fields (m.serverDlg)
	regionSendRail    = "send:rail"    // send wizard step rail (m.wizard)
	regionWorkerRail  = "worker:rail"  // §H worker wizard step rail (m.workerWiz)
)

// registerFocusHits adds the Task 8.5 click-to-focus rects to the
// per-frame map in draw order: page-published regions (the §J step rail,
// content-relative through pages.Focuser) first, then the four centered
// modals' own rows (already absolute, the root_view.go draw order
// dlg ▸ wizard ▸ serverDlg ▸ workerWiz). buildHitMap calls it after the
// page's scroll/select regions and before the §M box and picker rows, so
// those overlays shadow the cells they draw over.
func (m *RootModel) registerFocusHits(hm *hitMap) {
	ox, oy := m.contentOrigin()
	if fc, ok := m.Current().(pages.Focuser); ok {
		for _, r := range fc.FocusRegions() {
			hm.addAbs(ox, oy, r.Rect, focusHit(r.ID, r.Index))
		}
	}

	for _, owner := range []struct {
		region string
		hits   []widgets.RowHit
	}{
		{regionConnectForm, m.connectFormRowHits()},
		{regionServerForm, m.serverFormRowHits()},
		{regionSendRail, m.sendRailRowHits()},
		{regionWorkerRail, m.workerRailRowHits()},
	} {
		for _, r := range owner.hits {
			hm.add(r.Rect, focusHit(owner.region, r.Index))
		}
	}
}

// modalRowHits translates a modal's View-relative focus rects into the
// ABSOLUTE cells the hit map resolves: the same centering math
// overlayCenter applies to the modal's composed View (the pickerRowHits
// trick in root_view.go, minus the picker's extra box offset — these
// rects are measured against the modal's own View origin). Rects whose
// first line overlayCenter clips below the canvas publish nothing, and
// one running past the canvas bottom is clamped to its visible lines
// (hit = drawn ink).
func (m *RootModel) modalRowHits(view string, hits []widgets.RowHit) []widgets.RowHit {
	if len(hits) == 0 {
		return nil
	}
	inner := m.innerWS()
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	x := max((inner.Width-boxWidth(lines))/2, 0)
	y := max((inner.Height-len(lines))/2, 0)
	ox, oy := m.contentOrigin()

	out := make([]widgets.RowHit, 0, len(hits))
	for _, rh := range hits {
		row := geom.Rect{X: ox + x + rh.Rect.X, Y: oy + y + rh.Rect.Y, W: rh.Rect.W, H: rh.Rect.H}
		top := row.Y - oy
		if top >= inner.Height {
			continue // the box line is not drawn: overlayCenter stops splicing
		}
		if bot := top + row.H; bot > inner.Height {
			row.H = inner.Height - top
		}
		out = append(out, widgets.RowHit{Rect: row, Index: rh.Index})
	}

	return out
}

// connectFormRowHits / serverFormRowHits / sendRailRowHits /
// workerRailRowHits report the ABSOLUTE drawn focus rects of the four
// centered modals (nil when the modal is closed). buildHitMap registers
// exactly these, and the Task 8.5 tests click exactly these — the
// pickerRowHits pattern: one measurement, no re-derived geometry.
func (m *RootModel) connectFormRowHits() []widgets.RowHit {
	if m.dlg == nil {
		return nil
	}

	return m.modalRowHits(m.dlg.View(), m.dlg.FieldRowHits())
}

func (m *RootModel) serverFormRowHits() []widgets.RowHit {
	if m.serverDlg == nil {
		return nil
	}

	return m.modalRowHits(m.serverDlg.View(), m.serverDlg.FieldRowHits())
}

func (m *RootModel) sendRailRowHits() []widgets.RowHit {
	if m.wizard == nil {
		return nil
	}

	return m.modalRowHits(m.wizard.View(), m.wizard.RailRowHits())
}

func (m *RootModel) workerRailRowHits() []widgets.RowHit {
	if m.workerWiz == nil {
		return nil
	}

	return m.modalRowHits(m.workerWiz.View(), m.workerWiz.RailRowHits())
}

// analyzeRailRowHits reports the §J page rail's ABSOLUTE drawn label
// rects (the pages.Focuser regions carrying pages.RegionAnalyzeRail,
// translated by the frame's content origin) for the same one-measurement
// click path as the modal rects above.
func (m *RootModel) analyzeRailRowHits() []widgets.RowHit {
	fc, ok := m.Current().(pages.Focuser)
	if !ok {
		return nil
	}
	ox, oy := m.contentOrigin()

	var out []widgets.RowHit
	for _, r := range fc.FocusRegions() {
		if r.ID != pages.RegionAnalyzeRail {
			continue // modal-owned ids register through their owners above
		}
		out = append(out, widgets.RowHit{
			Rect:  geom.Rect{X: ox + r.Rect.X, Y: oy + r.Rect.Y, W: r.Rect.W, H: r.Rect.H},
			Index: r.Index,
		})
	}

	return out
}

// handleFocusMsg is the focusMsg seam (Task 8.5): a left click resolved
// to a drawn field row or rail label moves the owner's focus THROUGH THE
// NAVIGATION THE OWNER ALREADY EXPOSES — a field click is SetFocus
// (navigate mode: highlighted, not typed into; and SetFocus closes any
// open header picker so no stale overlay stays drawn over the field), a
// rail click is the wizard's own step walk: backward = the free revisit
// its Esc performs, forward = a replay of the current step's Enter leg
// through root's validation (never a teleport to the clicked step), and
// the current step is inert.
//
// MODAL GUARD (carry 4, and why the FULL modalOpen() is wrong here):
// these regions belong to the form/wizard modals THEMSELVES — those
// modals are open by definition whenever their regions are clickable, so
// handleSelectMsg's modalOpen() gate would make click-to-focus dead
// forever. The narrow overlayOverForm gate blocks only what can open ON
// TOP of a form modal and own the input there (the file picker, the §M
// box, the palette, or a pending §N3 confirm — the updateKey modal chain
// puts every one of these above the forms). A PAGE-owned region (the §J
// rail) keeps the FULL modalOpen() gate: behind any modal — a form
// included — the page is frozen, exactly the handleSelectMsg doctrine.
func (m *RootModel) handleFocusMsg(msg focusMsg) (tea.Model, tea.Cmd) {
	switch msg.region {
	case regionConnectForm:
		if m.dlg == nil || m.overlayOverForm() {
			return m, nil
		}
		m.dlg.SetFocus(msg.index)

		return m, nil

	case regionServerForm:
		if m.serverDlg == nil || m.overlayOverForm() {
			return m, nil
		}
		m.serverDlg.SetFocus(msg.index)

		return m, nil

	case regionSendRail:
		if m.wizard == nil || m.overlayOverForm() {
			return m, nil
		}
		if msg.index <= m.wizard.Step() {
			m.wizard.BackToStep(msg.index) // free revisit; == is a no-op

			return m, nil
		}
		enter, _ := synthKeyPress("enter")

		return m.updateWizardKey(enter)

	case regionWorkerRail:
		if m.workerWiz == nil || m.overlayOverForm() {
			return m, nil
		}
		if msg.index <= m.workerWiz.Step() {
			m.workerWiz.BackToStep(msg.index) // free revisit; == is a no-op

			return m, nil
		}
		enter, _ := synthKeyPress("enter")

		return m.updateWorkerWizKey(enter)
	}

	if m.modalOpen() {
		return m, nil // the modal owns the screen; the page behind stays frozen
	}
	if msg.region == pages.RegionAnalyzeRail && msg.index != m.analyzeStep {
		// The PgUp/PgDn/Tab path: backward jumps are free revisits, the
		// forward direction runs the current step's gated commit — a
		// click on a later step advances at most one gated step.
		return m.handleAnalyzeStepDelta(pages.AnalyzeStepDeltaMsg{Delta: msg.index - m.analyzeStep})
	}

	return m, nil
}

// overlayOverForm reports whether an input-owning overlay sits ON TOP of
// an open form/wizard modal: the command palette, the §M help box, the
// shared file picker, or a pending §N3 confirm. The updateKey modal chain
// routes every key to these ahead of the forms (and the worker wizard's
// navigate-mode "?" is the one §M-over-a-modal state the router can
// reach), so a click resolved under them must stay inert. The four form
// modals are deliberately NOT in this predicate: their own regions are
// only ever clickable while they are open, so including them would make
// click-to-focus dead — the reconciliation the handleFocusMsg doc names.
func (m *RootModel) overlayOverForm() bool {
	if m.pal != nil || m.help != nil || m.filePick != nil {
		return true
	}

	return m.confirmPending()
}
