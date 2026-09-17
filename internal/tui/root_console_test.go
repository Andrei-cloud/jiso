package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/connection"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// TestInstallConsoleSinkCapturesCounterOutput pins: the
// utils (STAN/RRN) and transactions (collection reload) system lines
// must arrive as consoleLineMsg instead of hitting raw stderr and
// smashing the alt screen mid-frame, and restore must put the
// package sinks back.
func TestInstallConsoleSinkCapturesCounterOutput(t *testing.T) {
	beforeUtils := utils.Output()
	beforeTx := transactions.Output()
	beforeConn := connection.Output()

	var got []tea.Msg
	restore := installConsoleSink(func(msg tea.Msg) { got = append(got, msg) })
	t.Cleanup(restore)

	if _, err := utils.Output().Write([]byte("RRN counter initialized with persisted value: 7\n")); err != nil {
		t.Fatalf("write to utils sink: %v", err)
	}
	if _, err := transactions.Output().Write([]byte("Warning: Failed to load transaction state: boom\n")); err != nil {
		t.Fatalf("write to transactions sink: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("captured %d messages, want 2: %#v", len(got), got)
	}
	first, ok := got[0].(consoleLineMsg)
	if !ok || first.text != "RRN counter initialized with persisted value: 7" {
		t.Errorf("first message = %#v, want consoleLineMsg with the RRN init line", got[0])
	}
	second, ok := got[1].(consoleLineMsg)
	if !ok || second.text != "Warning: Failed to load transaction state: boom" {
		t.Errorf("second message = %#v, want consoleLineMsg with the collection warning", got[1])
	}

	restore()

	if utils.Output() != beforeUtils {
		t.Error("utils sink not restored")
	}
	if transactions.Output() != beforeTx {
		t.Error("transactions sink not restored")
	}
	if connection.Output() != beforeConn {
		t.Error("connection sink not restored")
	}
}
