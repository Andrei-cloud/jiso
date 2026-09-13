package transactions

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSetOutput(t *testing.T) {
	previous := Output()
	t.Cleanup(func() { SetOutput(previous) })

	var buf bytes.Buffer
	SetOutput(&buf)
	outputf("Warning: Failed to load transaction state: %v\n", "boom")
	if got, want := buf.String(), "Warning: Failed to load transaction state: boom\n"; got != want {
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
