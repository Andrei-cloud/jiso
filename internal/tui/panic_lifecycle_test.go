package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestRunPanicInUpdateRestoresTerminal drives the real program with a page
// whose Update panics on the 'x' key (typed 200ms in, after the fps ticker
// has flushed real alt-screen frames) and pins v2.0.9's built-in panic
// contract end to end:
//
//   - Program.Run's deferred recover (tea.go:1026-1033) catches panics from
//     model.Update / View (both run on Run's goroutine via eventLoop) and
//     converts them to ErrProgramKilled + ErrProgramPanic — the same
//     assertions bubbletea's own TestTeaPanics makes (tea_test.go:568-577).
//   - recoverFromPanic (tea.go:1284-1306) calls p.shutdown(true), which
//     stops the renderer; cursedRenderer.close emits the alt-screen exit
//     and flushes the writer even when the last frame is skipped
//     (cursed_renderer.go:175-201, 257-265). So the terminal is restored on
//     the panic path without any app-side recover.
//   - run's stopBridge still runs after Run returns an error, and the pump
//     goroutine is reaped before this test observes the process again.
//
// The test process surviving to the final assertion is itself proof no
// os.Exit ran (the import guard separately forbids os.Exit in this
// package).
func TestRunPanicInUpdateRestoresTerminal(t *testing.T) {
	// v2 prints the panic + stack to os.Stderr (recoverFromPanic);
	// redirect it so the test log stays readable and the trace is assertable.
	RedirectStderr(t)

	a := newLifecycleApp(t)
	m := NewRootModel(a)
	m.Replace(panicPage{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, pw := openPipeInput(t)
	out := &syncBuf{}

	go func() {
		time.Sleep(200 * time.Millisecond) // after ≥12 ticker flushes: real frames on the wire
		_, _ = pw.Write([]byte("x"))
	}()

	base := runtime.NumGoroutine()

	err := run(ctx, m, runOptions(in, out)...)
	_ = pw.Close()

	if !errors.Is(err, tea.ErrProgramPanic) {
		t.Fatalf("panic exit err = %v, want ErrProgramPanic", err)
	}
	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("panic exit err = %v, want ErrProgramKilled too", err)
	}
	if m.bridge == nil {
		t.Error("bridge never armed before the panic: test would pass vacuously")
	}

	assertGoroutinesSettled(t, base)
	wantAltScreenRestore(t, out)

	// The stack still holds the last saved page value: the panicking copy
	// unwound before forward could store it — the panic came from Update,
	// not from a later stage (the exact count depends on which startup
	// message was routed first).
	if _, ok := m.Current().(panicPage); !ok {
		t.Errorf("stored page after panic = %T, want panicPage", m.Current())
	}
}

// RedirectStderr sends os.Stderr to a temp file for the duration of the
// test and asserts the file caught the v2 panic banner.
func RedirectStderr(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stderr.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create stderr stand-in: %v", err)
	}

	orig := os.Stderr
	os.Stderr = f

	t.Cleanup(func() {
		os.Stderr = orig
		_ = f.Close()

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read stderr stand-in: %v", err)

			return
		}
		if !strings.Contains(string(data), "boom from Update") {
			t.Errorf("stderr lacks the v2 panic banner, got:\n%.300s", data)
		}
	})
}
