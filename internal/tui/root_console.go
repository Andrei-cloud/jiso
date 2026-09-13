// root_console.go is the TUI console pane (UAT): every NON-TUI system
// line the connection manager emits (unsafe read errors, reconnect
// chatter, route notices) used to hit os.Stderr and smash the alternate
// screen mid-frame. The TUI swaps internal/connection's output sink for
// consoleWriter, which turns each line into a consoleLineMsg through
// the program seam; the root keeps a bounded ring and renders the newest
// line in the frame's bottom console strip.
package tui

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
	"jiso/internal/connection"
	"jiso/internal/server"
	"jiso/internal/transactions"
	"jiso/internal/tui/theme"
	"jiso/internal/utils"
)

// consoleLineMsg carries one system output line into the model.
type consoleLineMsg struct{ text string }

// serverLineMsg carries one internal/server (mock server) line into the
// model; unlike consoleLineMsg it never reaches the global strip — the
// router keeps it in the §4 page's own LOG pane (UAT round 3).
type serverLineMsg struct{ text string }

// consoleRingMax bounds the retained lines (oldest dropped).
const consoleRingMax = 200

// consoleWriter is the internal/connection sink during a TUI session:
// Write splits line-buffered input and forwards each complete line via
// send. Lines after the sink is retired are dropped (the program is
// gone; a blocking Send would hang the manager goroutine).
type consoleWriter struct {
	mu   sync.Mutex
	buf  string
	send func(tea.Msg)
	dead atomic.Bool
}

// newConsoleWriter builds the capture sink; retire() stops delivery.
func newConsoleWriter(send func(tea.Msg)) *consoleWriter {
	return &consoleWriter{send: send}
}

func (w *consoleWriter) Write(p []byte) (int, error) {
	if w.dead.Load() {
		return len(p), nil
	}
	w.mu.Lock()
	w.buf += string(p)
	var complete []string
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(w.buf[:i])
		w.buf = w.buf[i+1:]
		if line != "" {
			complete = append(complete, line)
		}
	}
	w.mu.Unlock()
	for _, line := range complete {
		if !w.dead.Load() {
			w.send(consoleLineMsg{text: line})
		}
	}

	return len(p), nil
}

// Close retires the sink (deferred from run; the package sink itself is
// restored by the caller).
func (w *consoleWriter) Close() error {
	w.dead.Store(true)

	return nil
}

// var _ io.Writer keeps the interface contract explicit.
var _ io.Writer = (*consoleWriter)(nil)

// appendConsoleLine stamps one system line into the ring, receipt-
// timestamped (UAT round 4: an undated line reads as current truth even
// when it is an hour old).
func (m *RootModel) appendConsoleLine(text string) {
	m.console = append(m.console, m.stampLine(text))
	if len(m.console) > consoleRingMax {
		m.console = m.console[len(m.console)-consoleRingMax:]
	}
}

// stampLine prefixes the injectable clock's time ("09:22:03 ").
func (m *RootModel) stampLine(text string) string {
	return m.now().Format("15:04:05") + " " + text
}

// stampConnStatus replaces the visible status line on every connection
// state change (UAT round 4: a stale "Connection closed" stuck on
// screen after reconnecting). The newest line is what the strip shows,
// so stamping a fresh truth retires the old one.
func (m *RootModel) stampConnStatus(ev events.ConnectionEvent) {
	th := m.themeOrNil()
	switch ev.State {
	case events.StateConnected:
		text := th.Symbol(theme.KindOK) + " connected"
		if ev.Detail != "" {
			text += " " + ev.Detail
		}

		m.appendConsoleLine(text)
	case events.StateFailed:
		text := "connect failed"
		if ev.Detail != "" {
			text += ": " + ev.Detail
		}

		m.appendConsoleLine(text)
	default:
		m.appendConsoleLine("disconnected")
	}
}

// consoleLine is the strip text: the newest line, error-styled when it
// reads like one.
func (m *RootModel) consoleLine() (string, bool) {
	if len(m.console) == 0 {
		return "", false
	}
	line := m.console[len(m.console)-1]
	low := strings.ToLower(line)
	isErr := strings.Contains(line, "❌") || strings.Contains(line, "🔴") ||
		strings.Contains(low, "error") || strings.Contains(low, "failed") ||
		strings.Contains(low, "unsafe")

	return line, isErr
}

// installConsoleSink points internal/connection's output at the capture
// writer and internal/server's output at a server-tagged capture writer,
// and returns the restore func (deferred by run). Server lines are
// re-tagged as serverLineMsg so the router can keep them inside the §4
// page's LOG pane instead of the global strip (UAT round 3).
//
// UAT round 5: the utils (STAN/RRN) and transactions (collection reload)
// system lines join the capture — their raw stderr writes smashed the
// alt screen mid-frame (the RRN init line rendered through the §F pane
// borders during a scenario run).
func installConsoleSink(send func(tea.Msg)) (restore func()) {
	previous := connection.Output()
	previousServer := server.Output()
	previousUtils := utils.Output()
	previousTx := transactions.Output()
	w := newConsoleWriter(send)
	ws := newConsoleWriter(func(msg tea.Msg) {
		if l, ok := msg.(consoleLineMsg); ok {
			send(serverLineMsg(l))
		}
	})
	cw := newConsoleWriter(send)
	connection.SetOutput(w)
	server.SetOutput(ws)
	utils.SetOutput(cw)
	transactions.SetOutput(cw)

	return func() {
		_ = w.Close()
		_ = ws.Close()
		_ = cw.Close()
		connection.SetOutput(previous)
		server.SetOutput(previousServer)
		utils.SetOutput(previousUtils)
		transactions.SetOutput(previousTx)
	}
}
