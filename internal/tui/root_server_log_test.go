// root_server_log_test.go pins the §4 LOG pane's feed: internal/server's
// mock-server output must arrive as serverLineMsg instead of hitting raw
// stderr and smashing the alt screen mid-frame, and restore must put the
// package sink back.
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/server"
)

func TestInstallServerLogSinkCapturesServerOutput(t *testing.T) {
	before := server.Output()

	var got []tea.Msg
	restore := installServerLogSink(func(msg tea.Msg) { got = append(got, msg) })
	t.Cleanup(restore)

	const line = "[SERVER] Matched Route Echo for MTI 0800 -> Responding 0810"
	if _, err := server.Output().Write([]byte(line + "\nincomplete")); err != nil {
		t.Fatalf("write to server sink: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("captured %d messages, want 1 (the unterminated tail must wait): %#v", len(got), got)
	}
	first, ok := got[0].(serverLineMsg)
	if !ok || first.text != line {
		t.Errorf("first message = %#v, want serverLineMsg with the complete line", got[0])
	}

	restore()

	if server.Output() != before {
		t.Error("server sink not restored")
	}
}
