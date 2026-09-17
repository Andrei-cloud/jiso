package tui

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// paletteChromeRows is the frame chrome (header, separators, status,
// footer) the palette panel subtracts from the terminal height; the
// frame itself truncates any remaining overflow, so this only sizes the
// match list sensibly.
const paletteChromeRows = 6

// palettePanelHeight sizes the palette panel for a terminal height.
func palettePanelHeight(height int) int {
	return max(height-paletteChromeRows, 2)
}

// palettePanelWidth sizes the palette panel inside the content area: the
// Modal width target, clamped so the panel keeps a margin
// inside the frame (the centered modal border wraps it).
func palettePanelWidth(innerW int) int {
	return max(min(60, innerW-8), 20)
}

// newPalette opens the overlay: a fresh widget over the seeded action
// registry, sized to the current content area (a typical 16 rows until
// the first WindowSizeMsg arrives).
func (m *RootModel) newPalette() *palette.Model {
	inner := m.innerWS()
	w, h := inner.Width, inner.Height
	if w <= 0 {
		w = 40
	}
	if h <= 0 {
		h = 16
	}

	return palette.New(theme.Default(), palette.SeedMatcher(), palettePanelWidth(w), palettePanelHeight(h))
}

// updatePalette is the palette-mode state machine: while open the widget
// owns the keyboard (typing "send" or "1" must not jump pages). A
// successful submit closes the overlay and forwards the action's Msg cmd;
// esc closes it with no cmd, leaving the page stack untouched.
func (m *RootModel) updatePalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pal, cmd := m.pal.Update(msg)
	m.pal = pal

	switch {
	case cmd != nil:
		if id, ok := pal.SelectedActionID(); ok {
			m.debug.logf("palette exec id=%s", id)
		} else {
			m.debug.logf("palette exec")
		}
	case pal.Closed():
		m.debug.logf("palette close")
	}

	if cmd != nil || pal.Closed() {
		m.pal = nil
	}

	return m, cmd
}

// jumpToID resolves a palette GoToPageMsg against the page registry;
// unknown IDs are dropped (the palette only emits seeded ones).
func (m *RootModel) jumpToID(id string) {
	for i := range m.registry {
		if m.registry[i].ID() == id {
			m.jumpTo(i)

			return
		}
	}
}

// pushByID resolves a palette PushPageMsg (show help). The help
// ID opens the §M overlay over the current page — the same behavior as
// the "?" binding; there is no help page in the registry.
func (m *RootModel) pushByID(id string) {
	if id == "help" {
		m.openHelp()

		return
	}
	if m.Current().ID() == id {
		return
	}
	for i := range m.registry {
		if m.registry[i].ID() == id {
			m.Push(m.registry[i])

			return
		}
	}
}
