// root_journey_test.go pins the navigation contract:
// esc unwinds every journey back to the dashboard — page pushes (§D,
// §H, §J, §4) and the wizards (send wizard, worker wizards, connect
// dialog, server form) — and esc on the dashboard remains a no-op.
package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// escLadder walks msg-driven journeys: after the setup fn leaves the
// model wherever it lands, esc must unwind to depth 1 (the dashboard)
// in one press per stack level, never past it.
func escLadder(t *testing.T, setup func(t *testing.T, r *sendTestRoot)) {
	t.Helper()

	r := newSendTestRoot(t)
	if _, cmd := r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32}); cmd != nil && isQuitCmd(cmd) {
		t.Fatal("unexpected quit")
	}

	setup(t, r)

	depth := r.m.StackDepth()
	if depth < 1 {
		t.Fatalf("stack depth %d after setup", depth)
	}

	// Esc unwinds to the dashboard; the dashboard keeps depth 1. The
	// page hands the pop back as a Cmd (its result feeds Update), so
	// the ladder must follow the chain like the live program does.
	for i := 0; i < depth+2; i++ {
		_, cmd := r.m.Update(special(tea.KeyEscape))
		for range 4 {
			if cmd == nil {
				break
			}
			msg := cmd()
			if msg == nil {
				break
			}
			_, cmd = r.m.Update(msg)
		}
		if r.m.Current().ID() == pages.DashboardPageID {
			break
		}
	}
	if got := r.m.Current().ID(); got != pages.DashboardPageID {
		t.Fatalf("esc ladder stopped at %q (depth %d), want dashboard", got, r.m.StackDepth())
	}

	// Esc on the dashboard is a no-op (q is the quit chord).
	r.m.Update(special(tea.KeyEscape))
	if r.m.StackDepth() != 1 {
		t.Fatalf("esc on dashboard changed depth to %d", r.m.StackDepth())
	}
	if r.m.Current().ID() != pages.DashboardPageID {
		t.Fatalf("esc on dashboard left the page: %q", r.m.Current().ID())
	}
}

// pumpKey sends one key and follows the returned Cmd chain (pages
// hand their intents back as Cmds, the live program follows them).
func pumpKey(m interface {
	Update(tea.Msg) (tea.Model, tea.Cmd)
}, msg tea.Msg,
) {
	_, cmd := m.Update(msg)
	for range 6 {
		if cmd == nil {
			return
		}
		next := cmd()
		if next == nil {
			return
		}
		_, cmd = m.Update(next)
	}
}

func isQuitCmd(cmd tea.Cmd) bool {
	msg := cmd()
	_, quit := msg.(tea.QuitMsg)

	return quit
}

// TestJourneySendLadder: §B s → §D exchange → esc → dashboard.
func TestJourneySendLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		r.m.liveConnect = func(context.Context) error { return nil }
		r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
			return cannedExchange(t, r.spec()), nil
		}
		r.m.Update(pages.TxSendMsg{ID: "Purchase"})
		for i := 0; i < pages.SendStageCount; i++ {
			r.pump(t, r.nextStage(t))
		}
		if r.m.Current().ID() != pages.SendPageID {
			t.Fatalf("journey did not reach §D: %q", r.m.Current().ID())
		}
	})
}

// TestJourneyServerFormLadder: hotkey 4 → §4 → c form → esc → esc →
// dashboard (the modal closes first, then the page pops).
func TestJourneyServerFormLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		pumpKey(r.m, ch('4'))
		pumpKey(r.m, ch('c'))
		if r.m.Current().ID() != pages.ServerPageID {
			t.Fatalf("journey did not reach §4: %q", r.m.Current().ID())
		}
		if r.m.serverDlg == nil {
			t.Fatal("c on §4 must open the server start form")
		}
	})
}

// TestJourneyAnalyzeLadder: hotkey 7 → §J wizard → esc → dashboard.
func TestJourneyAnalyzeLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		r.m.Update(ch('7'))
		if r.m.Current().ID() != pages.AnalyzePageID {
			t.Fatalf("journey did not reach §J: %q", r.m.Current().ID())
		}
	})
}

// TestJourneyWorkerWizardLadder: hotkey 5 → t wizard → esc (step 1) →
// esc → dashboard; the wizard closes before the page pops.
func TestJourneyWorkerWizardLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		pumpKey(r.m, ch('5'))
		pumpKey(r.m, ch('t'))
		if r.m.Current().ID() != pages.WorkersPageID {
			t.Fatalf("journey did not reach §H: %q", r.m.Current().ID())
		}
		if r.m.workerWiz == nil {
			t.Fatal("t on §H must open the stress wizard")
		}
	})
}

// TestJourneySendWizardLadder: dashboard s → send wizard → esc closes
// the modal → dashboard underneath.
func TestJourneySendWizardLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		stampOffline(t, r.m) // the wizard fallback is the offline journey
		r.m.Update(ch('s'))
		if r.m.wizard == nil {
			t.Fatal("s on the dashboard must open the send wizard")
		}
		if r.m.Current().ID() != pages.DashboardPageID {
			t.Fatalf("the wizard must be a modal over %q", r.m.Current().ID())
		}
	})
}

// `t` on the dashboard opens the same stress wizard §H's t opens, as a modal.
func TestJourneyDashboardStressWizard(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		pumpKey(r.m, ch('t'))
		if r.m.workerWiz == nil {
			t.Fatal("t on the dashboard must open the stress wizard")
		}
		if got := r.m.workerWiz.Mode(); got != pages.WorkerModeStress {
			t.Fatalf("wizard mode = %q, want %q", got, pages.WorkerModeStress)
		}
		if r.m.Current().ID() != pages.DashboardPageID {
			t.Fatalf("the wizard must be a modal over %q", r.m.Current().ID())
		}
	})
}

// TestJourneyConnectDialogLadder: dashboard c → connect dialog → esc
// closes → dashboard.
func TestJourneyConnectDialogLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		stampOffline(t, r.m) // "c" opens the dialog only without a connection
		r.m.Update(ch('c'))
		if r.m.dlg == nil {
			t.Fatal("c on the dashboard must open the connect dialog")
		}
	})
}

// TestJourneyInspectorLadder: §B enter on a tx → §I inspector → esc →
// §B → esc → dashboard.
func TestJourneyInspectorLadder(t *testing.T) {
	escLadder(t, func(t *testing.T, r *sendTestRoot) {
		pumpKey(r.m, ch('2'))
		pumpKey(r.m, special(tea.KeyEnter))
		if r.m.Current().ID() != pages.InspectorPageID {
			t.Fatalf("journey did not reach §I: %q", r.m.Current().ID())
		}
	})
}
