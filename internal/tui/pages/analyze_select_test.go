package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// analyze_select_test.go pins the §J wizard cursor semantics that root's
// per-Update SetState re-push must never clobber.

func TestAnalyzeCursorSurvivesResync(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)
	if a.ListCursor() != 1 {
		t.Fatalf("cursor = %d, want the current row at 1", a.ListCursor())
	}

	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if a.ListCursor() != 0 {
		t.Fatalf("cursor after up = %d, want 0", a.ListCursor())
	}

	// The root re-pushes the same-step snapshot (syncPages): the cursor
	// stays where the operator moved it, and Enter commits that row.
	a.SetState(st)
	if a.ListCursor() != 0 {
		t.Errorf("cursor after same-step re-sync = %d, want the moved-to 0", a.ListCursor())
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg, ok := cmdMsg(t, cmd).(AnalyzeCommitCaptureMsg); !ok || msg.Value != "/captures/night.pcap" {
		t.Errorf("Enter = %+v, want the cursor row night.pcap", cmd)
	}

	// A genuine step re-entry (root pushes another step, then this one
	// again) re-homes the cursor onto the current row.
	other := st
	other.Step = StepSpec
	a.SetState(other)
	a.SetState(st)
	if a.ListCursor() != 1 {
		t.Errorf("cursor after step re-entry = %d, want the current row at 1", a.ListCursor())
	}
}
