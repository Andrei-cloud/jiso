// root_stack.go is the page stack itself: push, pop, replace, the hotkey
// jump slots, and forwarding a message to every page on the stack. The
// model itself (struct, constructor, accessors) stays in root.go.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// StackDepth reports the number of pages on the router stack.
func (m *RootModel) StackDepth() int { return len(m.stack) }

// StackIDs returns the page IDs bottom..top; for tests and future deep links.
func (m *RootModel) StackIDs() []string {
	ids := make([]string, 0, len(m.stack))
	for _, p := range m.stack {
		ids = append(ids, p.ID())
	}

	return ids
}

// Current returns the top-of-stack page.
func (m *RootModel) Current() Page { return m.stack[len(m.stack)-1] }

// Push puts a page on top of the stack. The page is seeded with the
// current terminal size (an unseeded page would render at the fallback
// size until the next resize).
func (m *RootModel) Push(p Page) {
	m.leaveAnalyze()
	m.leaveCtf()
	m.leaveSettings()
	m.leaveDisconnect()
	p = m.seedSize(p)
	m.stack = append(m.stack, p)
	m.enterAnalyze()

	m.debug.logf("page push id=%s depth=%d", p.ID(), len(m.stack))
}

// seedSize forwards the current terminal size to a page entering the
// stack (no-op before the first WindowSizeMsg).
func (m *RootModel) seedSize(p Page) Page {
	if m.width <= 0 {
		return p
	}
	next, _ := p.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})

	return next
}

// Pop removes and returns the top page. The bottom page is never popped: the
// stack keeps at least one page, and "quit at root" is expressed by
// tea.Quit, not by an empty stack.
func (m *RootModel) Pop() Page {
	if len(m.stack) <= 1 {
		return nil
	}
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]

	return top
}

// Replace discards the whole stack and starts a new one with p. Like Push,
// the incoming page is seeded with the current terminal size.
func (m *RootModel) Replace(p Page) {
	m.leaveAnalyze()
	m.leaveCtf()
	from := m.Current().ID()
	m.leaveSettings()
	m.leaveDisconnect()
	p = m.seedSize(p)
	m.stack = []Page{p}
	m.enterAnalyze()

	m.debug.logf("page jump from=%s to=%s", from, p.ID())
}

// jumpTo replaces the stack with the registry page for slot i (1-based hotkey
// i+1). Repeating the current page is a no-op so key bursts stay stable.
func (m *RootModel) jumpTo(i int) {
	if i < 0 || i >= len(m.registry) {
		return
	}
	if m.Current().ID() == m.registry[i].ID() {
		return
	}
	m.Replace(m.registry[i])
}

// forward sends msg to the top page and stores the returned page.
func (m *RootModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	top := len(m.stack) - 1
	next, cmd := m.stack[top].Update(msg)
	m.stack[top] = next

	return m, cmd
}

// forwardAll sends msg to every page on the stack (resizes must reach hidden
// pages too or they render stale after a pop) and batches their commands.
func (m *RootModel) forwardAll(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, p := range m.stack {
		next, cmd := p.Update(msg)
		m.stack[i] = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// popPage is the esc contract: a pushed page unwinds, and a hotkey-jumped
// page at depth 1 navigates home to the dashboard (esc on the dashboard
// itself never reaches here — it stays a no-op there).
func (m *RootModel) popPage() {
	if m.StackDepth() > 1 {
		m.Pop()

		return
	}

	if m.Current().ID() != pages.DashboardPageID {
		m.jumpToID(pages.DashboardPageID)
	}
}

// PageIDs are the registered page slots in jump-key order: hotkey N
// selects PageIDs[N-1], exactly the global-chrome footer
// legend. Drill-downs and the palette-only settings page live after the
// eight slots and claim no digit.
var PageIDs = []string{
	pages.DashboardPageID,
	pages.TransactionsPageID,
	pages.ScenariosPageID,
	pages.ServerPageID,
	pages.WorkersPageID,
	pages.SessionsPageID,
	pages.AnalyzePageID,
	pages.CtfPageID,
}

// PageLabels are the footer legends for PageIDs ("1 dash 2 tx 3
// scenarios ..."). The footer and the §M help advertise these, never
// the raw ids.
var PageLabels = []string{"dash", "tx", "scenarios", "server", "workers", "sessions", "analyze", "ctf"}
