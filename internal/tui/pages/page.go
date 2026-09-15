// Package pages holds the M5 screens (wireframes
// .opencode/plans/02-tui-wireframes.md §A-§M). The Page interface lives here
// — not in internal/tui — so screens can implement it without an import
// cycle: internal/tui imports pages and re-exports the interface as a type
// alias (tui.Page = pages.Page), while pages never imports its parent.
//
// Import fence (enforced by imports_guard_test.go): pages may import the
// leaf TUI packages (theme, widgets, frame, palette), internal/app/events
// (the observation taxonomy only), bubbletea/lipgloss/x-ansi, and stdlib.
// It must NEVER import internal/app, internal/cli, internal/command,
// cobra, or internal/tui itself: pages consume only the state structs and
// EventMsg values the root model forwards (root owns all App access).
package pages

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// Page is one screen in the router stack (canonical definition; the tui
// package aliases it). Implementations keep their own state and receive
// every message the router forwards (keys the global layer did not claim,
// plus tea.WindowSizeMsg, tui.PaneFocusMsg, and pages.EventMsg). Update
// returns the next page value; View renders the page body — the router owns
// global chrome (frame sections, AltScreen, overlays), so pages must set
// only their Content and never draw header/status/footer themselves.
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

// PaneFocusMsg is delivered to the current page when Tab / shift+Tab cycles
// pane focus. The router only produces it; page-level focus bookkeeping
// belongs to the pages. Canonical definition lives here (pages must not
// import internal/tui); the tui package aliases it.
type PaneFocusMsg struct {
	// Reverse is true for shift+Tab.
	Reverse bool
}

// KeyboardClaimer is implemented by pages that own the keyboard while a
// page-local text-input mode is active (SCR-502 live filter). While
// ClaimsKeyboard reports true the router forwards every key press to the
// page unclaimed by the global layer — the same contract the command
// palette uses — so printable keys that collide with global bindings (q,
// digits, :, ?) stay typeable. The graceful exit (ctrl+c) stays global.
// UAT round 8 (D2): the claim is total; the old FreshDraftHelp
// "?"-on-empty escape hatch is gone — esc leaves the field first, and
// only then do the global keys work again.
type KeyboardClaimer interface {
	ClaimsKeyboard() bool
}
