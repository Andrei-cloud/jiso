// root_server_log.go is the §4 LOG pane's feed: every internal/server
// (mock server) line used to hit os.Stderr and smash the alternate
// screen mid-frame. The TUI swaps internal/server's output sink for
// serverLogWriter, which turns each complete line into a serverLineMsg
// through the program seam; the router timestamp-stamps it into the §4
// page's own ring. Other system output (internal/connection, utils,
// transactions) is not captured — the bottom console strip is gone and
// an alt-screen TUI must not surface stdout noise, so those lines fall
// to the terminal's scrollback.
package tui

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/server"
)

// serverLineMsg carries one internal/server (mock server) line into the
// model; the router keeps it in the §4 page's own LOG ring, nowhere else.
type serverLineMsg struct{ text string }

// serverLogRingMax bounds the retained LOG lines (oldest dropped).
const serverLogRingMax = 200

// stampLine prefixes the injectable clock's time ("09:22:03 ") so the
// LOG ring reads as a timeline.
func (m *RootModel) stampLine(text string) string {
	return m.now().Format("15:04:05") + " " + text
}

// serverLogWriter is the internal/server sink during a TUI session:
// Write splits line-buffered input and forwards each complete line via
// send as a serverLineMsg. Lines after the sink is retired are dropped
// (the program is gone; a blocking Send would hang the engine goroutine).
type serverLogWriter struct {
	mu   sync.Mutex
	buf  string
	send func(tea.Msg)
	dead atomic.Bool
}

// newServerLogWriter builds the capture sink; Close stops delivery.
func newServerLogWriter(send func(tea.Msg)) *serverLogWriter {
	return &serverLogWriter{send: send}
}

func (w *serverLogWriter) Write(p []byte) (int, error) {
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
			w.send(serverLineMsg{text: line})
		}
	}

	return len(p), nil
}

// Close retires the sink (deferred from run; the package sink itself is
// restored by the caller).
func (w *serverLogWriter) Close() error {
	w.dead.Store(true)

	return nil
}

// var _ io.Writer keeps the interface contract explicit.
var _ io.Writer = (*serverLogWriter)(nil)

// installServerLogSink points internal/server's output at the capture
// writer and returns the restore func (deferred by run).
func installServerLogSink(send func(tea.Msg)) (restore func()) {
	previous := server.Output()
	w := newServerLogWriter(send)
	server.SetOutput(w)

	return func() {
		_ = w.Close()
		server.SetOutput(previous)
	}
}
