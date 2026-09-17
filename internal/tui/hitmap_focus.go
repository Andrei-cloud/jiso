// hitmap_focus.go is the click-to-focus leg of the hit map: the modals and the
// §J page rail publish drawn field/step rects, buildHitMap registers them as
// focusHits in z-order, and handleFocusMsg routes a resolved focusMsg to the
// owner's OWN focus/step navigation. The click never invents a transition the
// keyboard does not have.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// Modal-owned focus regions ("<owner>:<pane>" like the page ids) for the
// centered modals; the §J rail is page geometry and publishes its own id
// through pages.Focuser.
const (
	regionConnectForm = "connect:form" // §E dialog fields (m.dlg)
	regionServerForm  = "server:form"  // §G start form fields (m.serverDlg)
	regionSendRail    = "send:rail"    // send wizard step rail (m.wizard)
	regionWorkerRail  = "worker:rail"  // §H worker wizard step rail (m.workerWiz)
)

// registerFocusHits adds the focus rects in draw order: the page-published
// §J rail (content-relative) first, then the four centered modals' absolute
// rows (dlg ▸ wizard ▸ serverDlg ▸ workerWiz). buildHitMap calls it before the
// §M box and picker rows so those overlays shadow the cells they cover.
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

// modalRowHits translates a modal's View-relative rects to ABSOLUTE cells with
// the same centering math overlayCenter applies. Rects clipped below the
// canvas publish nothing; one running past the bottom is clamped — a hit must
// be drawn ink.
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

// connectFormRowHits, serverFormRowHits, sendRailRowHits and
// workerRailRowHits report the ABSOLUTE drawn focus rects of the four centered
// modals (nil when the modal is closed): one measurement per row, no
// re-derived geometry.
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

// analyzeRailRowHits reports the §J page rail's ABSOLUTE drawn label rects,
// the pages.Focuser regions carrying pages.RegionAnalyzeRail translated by the
// frame's content origin.
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

// handleFocusMsg routes a resolved click to the owner's own focus/step
// navigation: a field click is SetFocus (navigate mode; it also closes any
// open header picker), a rail click is the wizard's step walk — backward is
// the free revisit, forward replays the current step's Enter leg ONLY for the
// immediately next step; larger gaps and the current step stay inert.
//
// Form regions use the narrow overlayOverForm gate: modalOpen() would make
// click-to-focus dead, since those modals are open by definition whenever
// their regions are clickable. The page-owned §J rail keeps the full
// modalOpen() gate, like handleSelectMsg.
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
		if msg.index > m.wizard.Step()+1 {
			return m, nil // a forward gap would skip steps: inert, never a surprise
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
		if msg.index > m.workerWiz.Step()+1 {
			return m, nil // a forward gap would skip steps: inert, never a surprise
		}
		enter, _ := synthKeyPress("enter")

		return m.updateWorkerWizKey(enter)
	}

	if m.modalOpen() {
		return m, nil // the modal owns the screen; the page behind stays frozen
	}
	if msg.region == pages.RegionAnalyzeRail && msg.index != m.analyzeStep {
		// PgUp/PgDn path: backward jumps are free revisits; forward runs
		// the current step's gated commit, advancing at most one step.
		return m.handleAnalyzeStepDelta(pages.AnalyzeStepDeltaMsg{Delta: msg.index - m.analyzeStep})
	}

	return m, nil
}

// overlayOverForm reports an input-owning overlay on top of an open form
// modal: the palette, §M box, file picker, or a pending §N3 confirm. The four
// form modals are deliberately excluded — their regions are only clickable
// while they are open, so including them would make click-to-focus dead.
func (m *RootModel) overlayOverForm() bool {
	if m.pal != nil || m.help != nil || m.filePick != nil {
		return true
	}

	return m.confirmPending()
}
