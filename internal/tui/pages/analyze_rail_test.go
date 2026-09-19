package pages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAnalyzeRailLabelsAllSteps(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)
	body := ansi.Strip(a.View().Content)

	for _, want := range []string{"PCAP ANALYZE", "1 capture", "2 spec", "3 header", "4 run"} {
		if !strings.Contains(body, want) {
			t.Errorf("rail lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "matching") {
		t.Errorf("tx goal must not show the matching step:\n%s", body)
	}
}

// TestAnalyzeRailRoutesGoalInterleaves: the mock-routes goal grows a
// fifth rail entry — the matching wizard — seated between header and run.
func TestAnalyzeRailRoutesGoalInterleaves(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	st.Goal = AnalyzeGoalMockRoutes
	a := analyzePage(t, st, 140, 32)
	body := ansi.Strip(a.View().Content)

	for _, want := range []string{"1 capture", "2 spec", "3 header", "4 matching", "5 run"} {
		if !strings.Contains(body, want) {
			t.Errorf("routes rail lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "4 run") {
		t.Errorf("routes goal must number run as 5:\n%s", body)
	}
}

// TestStepsForGoalPositions pins the navigation math root drives:
// positions, not indices, order the wizard.
func TestStepsForGoalPositions(t *testing.T) {
	t.Parallel()

	// tx/scenario: header -> run directly.
	for _, goal := range []string{AnalyzeGoalTransactions, AnalyzeGoalScenario} {
		if got := StepNext(goal, StepHeader); got != StepRun {
			t.Errorf("%s: next(header) = %d, want run", goal, got)
		}
		if got := StepPrev(goal, StepRun); got != StepHeader {
			t.Errorf("%s: prev(run) = %d, want header", goal, got)
		}
		if got := StepNext(goal, StepRun); got != StepRun {
			t.Errorf("%s: next(run) must clamp at run, got %d", goal, got)
		}
	}
	// routes: header -> matching -> run, both directions.
	if got := StepNext(AnalyzeGoalMockRoutes, StepHeader); got != StepMatching {
		t.Errorf("routes: next(header) = %d, want matching", got)
	}
	if got := StepNext(AnalyzeGoalMockRoutes, StepMatching); got != StepRun {
		t.Errorf("routes: next(matching) = %d, want run", got)
	}
	if got := StepPrev(AnalyzeGoalMockRoutes, StepRun); got != StepMatching {
		t.Errorf("routes: prev(run) = %d, want matching", got)
	}
	if got := StepPrev(AnalyzeGoalMockRoutes, StepMatching); got != StepHeader {
		t.Errorf("routes: prev(matching) = %d, want header", got)
	}
}
