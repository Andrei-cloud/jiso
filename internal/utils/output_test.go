package utils

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
)

func TestSetOutput(t *testing.T) {
	previous := Output()
	t.Cleanup(func() { SetOutput(previous) })

	var buf bytes.Buffer
	SetOutput(&buf)
	outputf("counter initialized: %d\n", 42)
	if got, want := buf.String(), "counter initialized: 42\n"; got != want {
		t.Errorf("outputf wrote %q, want %q", got, want)
	}
}

func TestSetOutputNilFallsBackToStderr(t *testing.T) {
	previous := Output()
	t.Cleanup(func() { SetOutput(previous) })

	SetOutput(nil)
	if Output() != io.Writer(os.Stderr) {
		t.Errorf("Output() after SetOutput(nil) = %v, want os.Stderr", Output())
	}
}

// lockedWriter is a mutex-guarded io.Writer so the race test can observe
// bytes written while the sink pointer swaps underneath.
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}

// TestSetOutputRaceWithWriter exercises the atomic swap while lines are
// being written: the sink must never be nil-read mid-flight (the TUI
// swaps it while persistence workers are live).
func TestSetOutputRaceWithWriter(t *testing.T) {
	previous := Output()
	t.Cleanup(func() { SetOutput(previous) })

	lw := &lockedWriter{}
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				SetOutput(lw)
				outputf("x\n")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			SetOutput(nil)
		}
	}()
	wg.Wait()

	if lw.String() == "" {
		t.Error("no lines reached the swapped writer")
	}
}
