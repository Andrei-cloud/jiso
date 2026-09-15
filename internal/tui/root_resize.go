package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// defaultResizeCoalesceWindow bounds how often the page stack is relaid
// out during a resize burst (one forwardAll per window, plus one trailing
// flush guaranteeing the final size lands). v2.0.9 coalesces only the OS
// signal itself — signals_unix.go:16 gives the SIGWINCH channel capacity 1,
// so back-to-back signals are dropped, and tty.go:105-127 checkResize
// re-queries the live size per signal — but every delivered
// tea.WindowSizeMsg still runs a full Update+View. The window below is the
// root-level coalescing the design contract (§lifecycle non-negotiable 3,
// "coalesce bursts") requires on top of that.
const defaultResizeCoalesceWindow = 32 * time.Millisecond

// innerWS reports the content area inside the outer frame border — the
// size every page, dialog, palette, help box, and picker lays out at.
// frame.ContentSize is the single shrink oracle, so pages and Render
// never disagree mid-shrink.
func (m *RootModel) innerWS() tea.WindowSizeMsg {
	return innerWSOf(tea.WindowSizeMsg{Width: m.width, Height: m.height})
}

// innerWSOf maps a terminal size to the content-area size inside the
// outer border.
func innerWSOf(ws tea.WindowSizeMsg) tea.WindowSizeMsg {
	w, h := frame.ContentSize(ws.Width, ws.Height)

	return tea.WindowSizeMsg{Width: w, Height: h}
}

// resizeFlushMsg is the private message the trailing flush Cmd returns;
// processing it clears the pending flag and relayouts iff the latest size
// differs from what the pages last saw.
type resizeFlushMsg struct{}

// handleWindowSize implements the leading-edge + trailing throttle for
// WindowSizeMsg. Root's own dimensions (and therefore View) always track
// the last size seen — non-negotiable 3 says "re-layout from current
// frame" — but the expensive part, relaying out every page on the stack
// via forwardAll, runs at most once per coalescing window: the first new
// size is applied synchronously (so the initial layout and single resizes
// stay immediate), later sizes inside the window are stored and applied by
// the trailing resizeFlushMsg.
func (m *RootModel) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	inner := m.innerWS()
	if m.pal != nil {
		m.pal.SetSize(palettePanelWidth(inner.Width), palettePanelHeight(inner.Height))
	}
	if m.help != nil {
		// The §M overlay is pure display state (the page stack cannot
		// change while it owns the keyboard), so a resize only resizes
		// the box; the registry snapshot stays authoritative. The pane
		// budget follows too (Task 8.2b): SetHeight re-clamps the wheel
		// offset into the new window.
		m.help.width = inner.Width
		m.help.SetHeight(max(inner.Height-2, 1))
	}
	if m.filePick != nil {
		// The picker overlay is sized to the modal box's inner width
		// exactly like openFilePicker sizes it (lipgloss Width pads but
		// never truncates: content-area width would overrun the box and
		// corrupt the frame).
		m.filePick.SetSize(modalBoxWidth(inner.Width)-2, max(inner.Height-2, 5))
	}

	if msg.Width == m.appliedW && msg.Height == m.appliedH {
		return m, nil // pages already laid out for this size
	}
	if m.resizePending {
		return m, nil // trailing flush will pick up this (latest) size
	}

	m.appliedW, m.appliedH = msg.Width, msg.Height
	m.resizePending = true

	// Pages derive their own content area via frame.ContentSize, so
	// they receive the raw terminal size; overlays (sized above) render
	// exactly what they are handed.
	return m, tea.Batch(m.forwardAll(msg), m.resizeFlushCmd())
}

// resizeFlushCmd sleeps one coalescing window and returns resizeFlushMsg.
// It runs in the program's command goroutine (v2 tea.go:717-741 detaches
// Cmds and documents they may outlive the loop briefly); the sleep is
// bounded by resizeWindow, so it never delays shutdown meaningfully.
func (m *RootModel) resizeFlushCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(m.resizeWindow)

		return resizeFlushMsg{}
	}
}

// handleResizeFlush closes one coalescing window: clear the pending flag
// and, if the latest stored size was never applied, relayout once with it.
func (m *RootModel) handleResizeFlush() (tea.Model, tea.Cmd) {
	m.resizePending = false

	if m.width == m.appliedW && m.height == m.appliedH {
		return m, nil
	}
	m.appliedW, m.appliedH = m.width, m.height

	return m, m.forwardAll(tea.WindowSizeMsg{Width: m.width, Height: m.height})
}
