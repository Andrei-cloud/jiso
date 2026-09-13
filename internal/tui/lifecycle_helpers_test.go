package tui

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// Run-level lifecycle harness (TUI-408). All tests drive the real
// tea.Program.Run through the run seam with the injection idiom from
// bubbletea's own tea_test.go (WithInput/WithOutput on in-memory buffers +
// WithWindowSize), so no TTY is required and no custom signal handling
// exists anywhere.

// altScreenEnter / altScreenExit are the exact sequences v2's cursed
// renderer writes: ansi.SetModeAltScreenSaveCursor /
// ResetModeAltScreenSaveCursor (cursed_renderer.go:569, 572; the close path
// at cursed_renderer.go:200-201 always emits the exit and flushes the
// buffer to the writer, even when shutdown skips the last frame).
const (
	altScreenEnter = "\x1b[?1049h"
	altScreenExit  = "\x1b[?1049l"
)

// syncBuf is the concurrent program-output sink (renderer, ticker, and the
// test reader all touch it).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

// wantAltScreenRestore asserts the output entered the alt screen and left
// it again, in that order — the observable contract of "terminal state
// restored" for quit, error, and panic exits.
func wantAltScreenRestore(t *testing.T, out *syncBuf) {
	t.Helper()

	s := out.String()
	iEnter, iExit := strings.Index(s, altScreenEnter), strings.Index(s, altScreenExit)
	if iEnter < 0 {
		t.Fatalf("output never entered alt screen (%d bytes captured)", len(s))
	}
	if iExit < 0 {
		t.Fatal("terminal not restored: alt-screen exit missing from output")
	}
	if iExit < iEnter {
		t.Fatalf("alt-screen exit (%d) precedes enter (%d)", iExit, iEnter)
	}
}

// panicPage panics inside Update when it sees the key 'x' — input-driven so
// the test can fire it after the renderer has flushed real frames (the
// alt-screen enter only reaches the writer on the first flush, which the
// fps ticker does 16ms into the session).
type panicPage struct{ updates int }

func (p panicPage) ID() string { return "panic" }

func (p panicPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	p.updates++
	if kc, ok := msg.(tea.KeyPressMsg); ok && kc.Code == 'x' && kc.Mod == 0 {
		panic("boom from Update (TUI-408 panic-restore probe)")
	}

	return p, nil
}

func (p panicPage) View() tea.View   { return tea.NewView("panic") }
func (p panicPage) Hints() []KeyHint { return nil }

// bridgeQuitPage quits only when a bus event reaches the page, making the
// clean-quit path proof that the pump goroutine actually ran (and that its
// program.Send delivery reached Update) before shutdown. SCR-501: pages see
// the event as pages.EventMsg (root stamps the clock; pages never receive
// the bridge wrapper).
type bridgeQuitPage struct{ sawBridge bool }

func (p bridgeQuitPage) ID() string { return "bridge-quit" }

func (p bridgeQuitPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	if _, ok := msg.(pages.EventMsg); ok {
		p.sawBridge = true

		return p, tea.Quit
	}

	return p, nil
}

func (p bridgeQuitPage) View() tea.View   { return tea.NewView("bridge-quit") }
func (p bridgeQuitPage) Hints() []KeyHint { return nil }

// newLifecycleApp builds a real internal/app with a live event bus on the
// shared config singleton (the app_test.go idiom; never run in parallel).
func newLifecycleApp(t *testing.T) *app.App {
	t.Helper()

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	return a
}

// openPipeInput returns an input reader plus the writer the test uses to
// type keys; the writer is closed in cleanup after Run has returned.
func openPipeInput(t *testing.T) (io.Reader, *io.PipeWriter) {
	t.Helper()

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	return pr, pw
}

// assertGoroutinesSettled polls until the goroutine count is back at the
// pre-run baseline: every v2 handler joins inside Program.Run, and the one
// detached goroutine (the bridge pump) must be reaped by run's stopBridge.
func assertGoroutinesSettled(t *testing.T, base int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)

	var n int
	for {
		runtime.GC()
		if n = runtime.NumGoroutine(); n <= base {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("goroutines after run: %d > baseline %d\n%s", n, base, buf)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
