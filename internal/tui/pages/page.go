// Package pages holds the M5 screens. The Page interface lives here so
// screens can implement it without an import cycle; internal/tui
// re-exports it as a type alias.
//
// Import fence (enforced by imports_guard_test.go): pages may import the
// leaf TUI packages, internal/app/events (observation taxonomy only),
// bubbletea/lipgloss/x-ansi, and stdlib — never internal/app,
// internal/cli, internal/command, cobra, or internal/tui itself (root
// owns all App access).
package pages

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// Page is one screen in the router stack (the tui package aliases it).
// Implementations keep their own state and receive every message the
// router forwards. View renders the page body; the router owns the global
// chrome, so pages set only their Content and never draw chrome.
type Page interface {
	// ID names the page for routing assertions and future deep links.
	ID() string

	// Update returns the next page value and an optional command. It must
	// stay pure: state transitions and tea.Cmd returns, no I/O.
	Update(msg tea.Msg) (Page, tea.Cmd)

	// View renders the page body for the current frame.
	View() tea.View

	// Hints returns the context-sensitive footer keymap for this page.
	// Primary entries survive the narrow (<80 cols) footer; the router
	// appends the global bindings after these.
	Hints() []frame.KeyHint
}

// PaneFocusMsg is delivered to the current page when Tab / shift+Tab
// cycles pane focus; the router only produces it.
type PaneFocusMsg struct {
	// Reverse is true for shift+Tab.
	Reverse bool
}

// KeyboardClaimer is implemented by pages that own the keyboard while a
// text-input mode is active: while ClaimsKeyboard is true the router
// forwards every key press there, so keys colliding with global bindings
// (q, digits, ":") stay typeable. The claim is total; ctrl+c stays global.
type KeyboardClaimer interface {
	ClaimsKeyboard() bool
}
