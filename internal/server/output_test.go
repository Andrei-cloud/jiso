package server

import (
	"strings"
	"sync"
	"testing"
)

type syncWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}

// TestSetOutputCaptures pins the sink contract: while a capture writer
// is installed every outputf line lands there (never stderr), and nil
// restores the previous sink for the next owner.
func TestSetOutputCaptures(t *testing.T) {
	orig := Output()

	w := &syncWriter{}
	SetOutput(w)
	if Output() != w {
		t.Fatalf("Output() = %v, want the capture writer", Output())
	}

	outputf("\n[SERVER] 🟢 Matched Route '%s' for MTI %s -> Responding %s (RC: %s)\n", "Echo", "0800", "0810", "00")

	SetOutput(orig)
	got := w.String()
	if !strings.Contains(got, "Responding 0810 (RC: 00)") {
		t.Errorf("capture = %q, want the route-match line", got)
	}

	want := orig
	if Output() != want {
		t.Fatalf("Output() after restore = %v, want %v", Output(), want)
	}
}
