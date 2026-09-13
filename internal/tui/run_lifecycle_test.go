package tui

import (
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
)

// runOptions is the TTY-free program wiring the run-level tests share:
// injected input/output/size + no signal handler (v2 installs its own
// SIGINT/SIGTERM handler inside Program.handleSignals, tea.go:656-692; the
// contract forbids app-side handlers). A nil reader disables input
// entirely (tea.WithInput(nil), options.go:37-42) so no unreadable
// cancelreader goroutine exists at shutdown.
func runOptions(in io.Reader, out *syncBuf) []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithWindowSize(100, 30),
		tea.WithoutSignalHandler(),
	}
}

// TestRunQuitStopsBridgeAndExitsAltScreen: normal quit ('q' at root →
// tea.Quit). Proves clean exit returns nil, the alt screen is entered and
// exited — View.AltScreen makes the cursed renderer emit
// SetModeAltScreenSaveCursor on first flush (cursed_renderer.go:354-362,
// 562-572), and Program.Run's deferred shutdown → cursedRenderer.close
// emits the exit (cursed_renderer.go:1180-1187 + 175-201) — and that no
// goroutine, above all the bridge pump, outlives Run.
func TestRunQuitStopsBridgeAndExitsAltScreen(t *testing.T) {
	a := newLifecycleApp(t)
	m := NewRootModel(a)
	m.Replace(bridgeQuitPage{}) // never triggered here: no bus publish

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, pw := openPipeInput(t)
	out := &syncBuf{}

	go func() {
		time.Sleep(80 * time.Millisecond)
		_, _ = pw.Write([]byte("\x03"))
	}()

	base := runtime.NumGoroutine()

	err := run(ctx, m, runOptions(in, out)...)
	_ = pw.Close() // unblock v2's cancelreader goroutine before the leak poll

	if err != nil {
		t.Fatalf("clean quit returned %v, want nil", err)
	}
	if m.bridge == nil {
		t.Error("bridge never armed: quit test would pass vacuously")
	}

	assertGoroutinesSettled(t, base)
	wantAltScreenRestore(t, out)
}

// TestRunBridgeEventDrivenQuitStopsPump: the program quits only because the
// pump delivered a bridge.Msg — proof the pump goroutine ran inside
// the session — and assertGoroutinesSettled then proves run's stopBridge
// reaped it before returning (shutdown ordering, TUI-408).
func TestRunBridgeEventDrivenQuitStopsPump(t *testing.T) {
	a := newLifecycleApp(t)
	m := NewRootModel(a)
	m.Replace(bridgeQuitPage{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuf{}

	go func() {
		time.Sleep(40 * time.Millisecond)
		a.Events().Publish(events.ConnectionEvent{State: events.StateConnected, Detail: "127.0.0.1:8583"})
	}()

	base := runtime.NumGoroutine()

	err := run(ctx, m, runOptions(nil, out)...)
	if err != nil {
		t.Fatalf("bridge-driven quit returned %v, want nil", err)
	}

	top, ok := m.Current().(bridgeQuitPage)
	if !ok || !top.sawBridge {
		t.Fatal("program exited without a bridge event: pump liveness not proven")
	}
	assertGoroutinesSettled(t, base)
	wantAltScreenRestore(t, out)
}

// TestRunContextCancelStopsBridgeOnErrorExit: the error exit path.
// Cancelling the caller ctx makes Program.Run return ErrProgramKilled
// (tea.go:1160-1172); run must still stop the bridge before returning and
// the renderer must still leave the alt screen.
func TestRunContextCancelStopsBridgeOnErrorExit(t *testing.T) {
	a := newLifecycleApp(t)
	m := NewRootModel(a)

	in, pw := openPipeInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuf{}

	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	base := runtime.NumGoroutine()

	err := run(ctx, m, runOptions(in, out)...)
	_ = pw.Close()

	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("ctx cancel err = %v, want ErrProgramKilled", err)
	}
	if m.bridge == nil {
		t.Error("bridge never armed: error-exit test would pass vacuously")
	}

	assertGoroutinesSettled(t, base)
	wantAltScreenRestore(t, out)
}
