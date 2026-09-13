package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Suspend (TUI-408, design §lifecycle non-negotiable 4): v2.0.9 owns the
// whole SIGTSTP dance natively — tea.Suspend()/SuspendMsg (tea.go:571-582)
// is consumed by the event loop (tea.go:779-782) which calls
// (*Program).suspend (tty.go:12-21): releaseTerminal(true) →
// suspendProcess (tty_unix.go:39-47) sends SIGTSTP to the process group and
// blocks until SIGCONT → RestoreTerminal → a ResumeMsg is injected. On
// Windows suspendSupported=false (tty_windows.go:62) and the msg is a
// no-op, matching "not required on Windows".
//
// What v2 does NOT ship is a ctrl+z → SuspendMsg binding: raw mode clears
// ISIG, so ctrl+z arrives as an ordinary tea.KeyPressMsg and only a model
// can turn it into a suspend. The design contract (§keybinding paradigm)
// lists Ctrl+Z among the keys "Reserved, never bound", so the root must
// NOT hand-roll kill(-SIGTSTP) — that would also fight v2's own
// suspend bookkeeping. Reported gap: with ctrl+z unbound, in-app suspend
// is unreachable until a future ticket opts in by returning a cmd yielding
// tea.Suspend() (the mechanism below already works unchanged).
func TestSuspendIsV2NativeAndCtrlZStaysUnbound(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	// ctrl+z: forwarded untouched, never quits (pairs with
	// TestReservedKeysStayUnbound; pinned here for the lifecycle contract).
	_, cmd := m.Update(mod('z', tea.ModCtrl))
	if isQuit(t, cmd) {
		t.Fatal("ctrl+z must stay unbound (v2 owns suspend via SuspendMsg)")
	}

	// The v2 msg path stays intact through the router: if a later ticket
	// binds ctrl+z to tea.Suspend(), or a ResumeMsg arrives after a shell
	// kill -SIGCONT cycle, the root forwards rather than swallows.
	for _, msg := range []tea.Msg{tea.Suspend(), tea.ResumeMsg{}} {
		_, cmd := m.Update(msg)
		if isQuit(t, cmd) {
			t.Fatalf("%T must not quit at the root", msg)
		}
	}

	seen := seenOf(m)
	if len(seen) != 3 {
		t.Fatalf("page saw %d msgs, want 3 (ctrl+z, SuspendMsg, ResumeMsg forwarded)", len(seen))
	}
	if _, ok := seen[1].(tea.SuspendMsg); !ok {
		t.Errorf("SuspendMsg mangled in routing: %T", seen[1])
	}
	if _, ok := seen[2].(tea.ResumeMsg); !ok {
		t.Errorf("ResumeMsg mangled in routing: %T", seen[2])
	}
}
