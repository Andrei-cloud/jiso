// Package tui implements the jiso Bubble Tea v2 terminal frontend: the root
// model, the page-stack router, and the hybrid keymap (TUI-401). It consumes
// ONLY the internal/app façade — never internal/cli or internal/command —
// and is enforced by TestTuiForbiddenImports in imports_guard_test.go.
package tui

import (
	"jiso/internal/tui/frame"
	"jiso/internal/tui/pages"
)

// KeyHint is one contextual footer entry (alias of frame.KeyHint so pages
// can implement Hints without importing the frame package everywhere).
type KeyHint = frame.KeyHint

// Page is one screen in the router stack. The interface is canonically
// defined in internal/tui/pages (screens implement it without an import
// cycle: pages must never import internal/tui); this alias keeps every
// existing internal/tui reference working unchanged.
type Page = pages.Page

// PaneFocusMsg is delivered to the current page when Tab / shift+Tab cycles
// pane focus. The router only produces it; page-level focus bookkeeping
// belongs to the pages. Canonical definition lives in internal/tui/pages.
type PaneFocusMsg = pages.PaneFocusMsg
