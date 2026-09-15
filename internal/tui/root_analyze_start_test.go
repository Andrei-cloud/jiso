// root_analyze_start_test.go pins the §J entry contract (UAT round 8
// finding 6): the wizard's paths start EMPTY — the operator chooses
// capture, spec, and header; nothing is inherited from the config. The
// harness (fakeAnalyze, analyzeTestRoot) lives in root_analyze_test.go,
// same package.
package tui

import (
	"testing"

	"jiso/internal/tui/pages"
)

// TestAnalyzeStartsWithEmptyPaths pins UAT round 8 finding 6: entering §J
// leaves the capture/spec/header paths EMPTY — the operator chooses every
// path, nothing is inherited from the config. The run step's goal default
// is the one prefill that stays, and the header list is offered with
// nothing pre-selected. An unset spec/header must ride the run legs as ""
// (the engine's own default applies there, never a crash).
func TestAnalyzeStartsWithEmptyPaths(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()

	if r.m.analyzeCapturePath != "" {
		t.Errorf("capture path = %q, want empty on entry", r.m.analyzeCapturePath)
	}
	if r.m.analyzeSpecPath != "" {
		t.Errorf("spec path = %q, want empty (chosen, not inherited)", r.m.analyzeSpecPath)
	}
	if r.m.analyzeHeader != "" {
		t.Errorf("header = %q, want unset (chosen, not inherited)", r.m.analyzeHeader)
	}
	if r.m.analyzeGoal != pages.AnalyzeGoalTransactions {
		t.Errorf("goal = %q, want the run-step default %q", r.m.analyzeGoal, pages.AnalyzeGoalTransactions)
	}
	for _, it := range r.m.analyzeHeaderList() {
		if it.Selected {
			t.Errorf("header %q must not be pre-selected on entry", it.Header)
		}
	}

	// Walking through with no spec/header pick stays sane: the legs carry
	// the empty picks to the engine ("" = engine default spec/header) and
	// the wizard reaches the run step without error.
	r.walkToRun(t)
	if f.enumN != 1 || f.enumSpecs[0] != "" || f.enumHeaders[0] != "" {
		t.Fatalf("enumeration args = %v/%v (n=%d), want the empty picks",
			f.enumSpecs, f.enumHeaders, f.enumN)
	}
}
