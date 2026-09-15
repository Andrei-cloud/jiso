package tui

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// pageCount is the number of registered jump slots (hotkeys 1..8).
const pageCount = 8

// keyMatches reports whether msg was pressed for binding b.
func keyMatches(msg tea.KeyPressMsg, b key.Binding) bool {
	return key.Matches(msg, b)
}

// globalKeyMap is the router-level keymap, evaluated before any page sees a
// key. Arrows and their hjkl aliases are deliberately NOT bound here: they
// belong to pages (the global layer only forwards them). Ctrl+C is handled
// gracefully (tea.Quit, per the TUI design contract); Ctrl+Z and Ctrl+\ stay
// unbound so the Bubble Tea runtime keeps its signal behaviour.
type globalKeyMap struct {
	Quit          key.Binding // q: pop the stack, quit at root
	Help          key.Binding // ?: push the help page
	Palette       key.Binding // : and ctrl+p: open the command palette
	PaneFocus     key.Binding // Tab: forward a PaneFocusMsg
	PaneFocusBack key.Binding // shift+Tab: same, Reverse
	GracefulExit  key.Binding // ctrl+c: return tea.Quit (runtime-managed teardown)
	Connect       key.Binding // c: open the §E connect dialog overlay (SCR-505)
	Send          key.Binding // s: open the send wizard (proposal 04 §B)

	// MouseToggle is the F9 global mouse-mode toggle (UAT round 9,
	// F-9c): flips RootModel.mouseEnabled so the terminal regains (or
	// re-releases) native click-drag text selection. The bound spelling
	// is "f9" because that is what tea.KeyF9.String() reports — the
	// vocabulary key.Matches compares in (ultraviolet keyTypeString);
	// docs and the footer legend display it as "F9".
	MouseToggle key.Binding // f9: toggle mouse reporting / text selection

	PageJumps [pageCount]key.Binding // 1..8: jump to page N

	// Palette-mode keys (esc/enter/backspace/up/down/j/k) live inside
	// internal/tui/palette.Model: while open the widget owns the keyboard
	// wholesale, so the global layer only opens the overlay and forwards.
}

// newGlobalKeyMap builds the bindings; kept a function so tests could inject
// a variant without mutating package state.
func newGlobalKeyMap() globalKeyMap {
	km := globalKeyMap{
		Quit:          key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Help:          key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Palette:       key.NewBinding(key.WithKeys(":", "ctrl+p"), key.WithHelp(":", "cmd")),
		PaneFocus:     key.NewBinding(key.WithKeys(theme.KeyTab), key.WithHelp("tab", "focus pane")),
		PaneFocusBack: key.NewBinding(key.WithKeys("shift+tab")),
		GracefulExit:  key.NewBinding(key.WithKeys("ctrl+c")),
		Connect:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "connect")),
		Send:          key.NewBinding(key.WithKeys("s")), // help text is contextual (the wizard is dashboard-gated)
		MouseToggle:   key.NewBinding(key.WithKeys("f9"), key.WithHelp("F9", "select")),
	}

	for i := range km.PageJumps {
		km.PageJumps[i] = key.NewBinding(
			key.WithKeys(runeDigit(i+1)),
			key.WithHelp(runeDigit(i+1), PageLabels[i]),
		)
	}

	return km
}

// runeDigit maps a 1-based page number (1..8) to its hotkey digit string.
func runeDigit(n int) string {
	return string(rune('0' + n))
}

// globalFooterHints derives the frame footer's global half from the keymap
// bindings themselves (design contract: hints generated, never hardcoded).
// The list leads with the wireframe's page legend ("1 dash 2 tx
// 3 scenarios 4 server 5 workers 6 sessions 7 analyze 8 ctf") and ends
// with the always-visible trio (": cmd ? help q quit", Primary). The
// router appends the current page's context keys after these, so a
// width-constrained footer drops context keys first, then the jump
// legend, and the trio never.
func globalFooterHints(km *globalKeyMap) []frame.KeyHint {
	hints := make([]frame.KeyHint, 0, pageCount+3)
	for _, b := range km.PageJumps {
		h := b.Help()
		hints = append(hints, frame.KeyHint{Key: h.Key, Desc: h.Desc})
	}
	for _, b := range []key.Binding{km.Palette, km.Help, km.Quit} {
		h := b.Help()
		hints = append(hints, frame.KeyHint{Key: h.Key, Desc: h.Desc, Primary: true})
	}

	return hints
}
