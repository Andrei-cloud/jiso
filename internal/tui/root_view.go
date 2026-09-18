// root_view.go is the composition of a frame: the page body, the global chrome
// (status bar, footer) and the overlays that sit over the page. The pages render
// themselves; this file decides what goes over them and in what order.
package tui

import (
	"jiso/internal/tui/frame"
	"jiso/internal/tui/widgets"

	tea "charm.land/bubbletea/v2"
)

// View implements tea.Model: the current page body with the overlays
// layered over it (modals render centered over the page,
// toasts bottom-right), composed into the outer frame, always on the
// alternate screen.
func (m *RootModel) View() tea.View {
	content := m.Current().View().Content
	inner := m.innerWS()

	// Modal overlays, lowest to highest precedence. Each renders its own
	// box (dialogs self-size to the width; the palette, help
	// box, and picker are wrapped in a modal border here) and is centered
	// over the unchanged page body.
	boxed := func(v string) string {
		return modalBox(m.themeOrNil(), modalBoxWidth(inner.Width), v)
	}
	if m.pal != nil {
		// The palette panel is a borderless list; wrap it in the modal
		// border (the panel's own width plus the two border columns).
		content = overlayCenter(content, modalBox(m.themeOrNil(), palettePanelWidth(inner.Width)+2, m.pal.View()), inner.Width, inner.Height)
	}
	if m.dlg != nil {
		// The §E connect dialog (and its server/workers form reuses)
		// self-sizes to the box; centered over the page.
		content = overlayCenter(content, m.dlg.View(), inner.Width, inner.Height)
	}
	if m.wizard != nil {
		// The send wizard reuses the centered modal.
		content = overlayCenter(content, m.wizard.View(), inner.Width, inner.Height)
	}
	if m.serverDlg != nil {
		// The server start form reuses the same centered modal.
		content = overlayCenter(content, m.serverDlg.View(), inner.Width, inner.Height)
	}
	if m.workerWiz != nil {
		// The §H worker start wizard reuses the same centered modal.
		content = overlayCenter(content, m.workerWiz.View(), inner.Width, inner.Height)
	}
	if m.help != nil {
		// The §M help overlay is centered over the page; the page
		// underneath keeps its state.
		content = overlayCenter(content, m.help.View(), inner.Width, inner.Height)
	}
	if m.filePick != nil {
		// The file picker is centered exactly like the modals.
		content = overlayCenter(content, boxed(m.filePick.View()), inner.Width, inner.Height)
	}
	if m.errModal != nil {
		// The error screen reuses the centered modal box, drawn over the
		// frozen page and every input overlay in the confirm band; it
		// owns the keyboard first (see updateKey) so its hint line never
		// advertises keys an invisible overlay would steal.
		content = overlayCenter(content, m.errModal.View(inner.Width, inner.Height), inner.Width, inner.Height)
	}
	for _, c := range []*widgets.ConfirmDialog{
		m.serverConfirm, m.workersConfirm, m.analyzeConfirm, m.ctfConfirm,
		m.analyzeOverwriteConfirm, m.scenarioConfirm, m.disconnectConfirm,
	} {
		// §N3 confirms: the two-line question renders in a centered
		// modal box over the unchanged page.
		if c != nil && c.Pending() {
			content = overlayCenter(content, boxed(c.View()), inner.Width, inner.Height)
		}
	}

	out := tea.NewView(frame.Render(m.frameProps(m.overlayToasts(content))))
	out.AltScreen = true
	// Arm the terminal mouse against a hit map rebuilt from the geometry
	// just composed: the OnMouse closure replays resolved cells against
	// the view the user actually sees (see hitmap.go).
	m.installMouse(&out, m.buildHitMap())

	return out
}
