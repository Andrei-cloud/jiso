// analyze_items_steal_test.go pins that the generated-item roster is a
// RUN-STEP surface: it never steals another step's keystrokes (the UAT
// round-10 regression where wizard keys toggled the write selection and
// enter silently applied a 1-of-192 pick), and the run-step radios stay
// live while it is open - pressing t/r/s/m applies the pending selection
// (Esc semantics) and acts as the radio it is.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// rosterState: a finished run whose picker auto-presents (ItemsID 1).
func rosterState() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.Items = []AnalyzeItemRow{
		{Key: "transaction|Tx 0100 #1", Name: "Tx 0100 #1", Kind: "transaction", Included: true},
		{Key: "dataset|ds_1", Name: "ds_1", Kind: "dataset", Group: "ds_1", Included: true},
		{Key: "scenario|Run", Name: "Run", Kind: "scenario", Included: true},
	}
	st.ItemsID = 1

	return st
}

func openRosterPage(t *testing.T) *Analyze {
	t.Helper()

	a := analyzePage(t, rosterState(), 120, 32)
	if !strings.Contains(ansi.Strip(a.View().Content), "ITEMS") {
		t.Fatal("the picker must auto-present after a run")
	}

	return a
}

// TestRosterClosesOnStepChange: walking away from the run step (forward or
// back) closes the roster without a silent apply; the keystrokes belong
// to the step the operator is actually standing on.
func TestRosterClosesOnStepChange(t *testing.T) {
	t.Parallel()

	a := openRosterPage(t)

	// Stand on the matching step with the same ItemsID: the roster is
	// gone from the view...
	st := rosterState()
	st.Step = StepMatching
	st.Goal = AnalyzeGoalMockRoutes
	st.Conds = []AnalyzeCond{{Side: "req", Field: "0", When: "equals", Value: "0100"}}
	a.SetState(st)
	if body := ansi.Strip(a.View().Content); strings.Contains(body, "ITEMS") {
		t.Errorf("the roster followed onto the matching step and would eat its keys:\n%s", body)
	}

	// ...and its letters now belong to the wizard.
	_, cmd := a.Update(press('a'))
	if cmd == nil {
		t.Fatal("[a] on the matching step returned no cmd")
	}
	if got := cmdMsg(t, cmd); got != (AnalyzeCondAddMsg{}) {
		t.Errorf("[a] = %#v, want the wizard's add-condition", got)
	}

	// Back to run: the picker does NOT silently re-open over the step
	// (it was closed; [x] reopens it fresh).
	st2 := rosterState()
	a.SetState(st2)
	if body := ansi.Strip(a.View().Content); strings.Contains(body, "ITEMS") {
		t.Errorf("the roster must not resurrect without a fresh ItemsID:\n%s", body)
	}
}

// TestRadiosPassThroughOpenRoster: while the roster is open on the run
// step, t/r/s/m apply the pending toggles (exactly what Esc would),
// close the roster, and reach the root as the run-step radios they are.
func TestRadiosPassThroughOpenRoster(t *testing.T) {
	t.Parallel()

	a := openRosterPage(t)

	// Deselect the transaction row (its dataset is not grouped with it
	// here, so the pending exclusion is exactly one key).
	_, _ = a.Update(press(' '))

	_, cmd := a.Update(press('r'))
	if cmd == nil {
		t.Fatal("[r] with the roster open returned no cmd")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("[r] = %#v, want a batch (apply + goal)", cmd())
	}
	msgs := make([]any, 0, len(batch))
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		msgs = append(msgs, sub())
	}
	if len(msgs) != 2 {
		t.Fatalf("batch size = %d, want apply+goal", len(msgs))
	}
	apply, okA := msgs[0].(AnalyzeItemsApplyMsg)
	if !okA {
		t.Fatalf("batch[0] = %#v, want the pending apply", msgs[0])
	}
	if len(apply.Excluded) != 1 || apply.Excluded[0] != "transaction|Tx 0100 #1" {
		t.Errorf("pending exclusion lost: %v", apply.Excluded)
	}
	if goal, okG := msgs[1].(AnalyzeChooseGoalMsg); !okG || goal.Goal != AnalyzeGoalMockRoutes {
		t.Errorf("batch[1] = %#v, want the routes-goal radio", msgs[1])
	}
	if a.itemsOpen {
		t.Error("the radios must close the roster on the way through")
	}
}

// TestRadiosPassWithoutPendingToggles: pressing a radio with nothing
// toggled still lands clean (an empty apply is a no-op at root) - and 'm'
// folds the security radio, 's' the scenario goal.
func TestRadiosPassWithoutPendingToggles(t *testing.T) {
	t.Parallel()

	a := openRosterPage(t)
	_, cmd := a.Update(press('m'))
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("[m] = %#v, want apply+mask batch", cmd())
	}
	if got := batch[1](); got != (AnalyzeChooseMaskMsg{Raw: true}) {
		t.Errorf("[m] second msg = %#v, want the mask radio", got)
	}
}
