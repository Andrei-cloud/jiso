// analyze_use_test.go pins the "use it now" keys: after a scenario run has
// been written (Done + write OK), the run step offers [l] to load the
// extract as the session's transactions file and [g] to open the §G server
// start form pre-filled with it as the routes file. In every other state
// the keys are inert and t/r/s stay the goal radios they have always been.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// writeDoneScenarioState: a finished scenario run whose file was written.
func writeDoneScenarioState() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Goal = AnalyzeGoalScenario
	st.Status = AnalyzeStatusDone
	st.WriteOK = true
	st.FileWritten = true
	st.WriteLine = "wrote 12 items to /tmp/extract-scenario.json"
	st.OutputPath = "/tmp/extract-scenario.json"

	return st
}

func TestAnalyzeUseKeysOfferedOnlyAfterScenarioWrite(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, writeDoneScenarioState(), 120, 32)

	if got := cmdMsg(t, keyCmd(t, a, 'l')); got != (AnalyzeUseTxFileMsg{}) {
		t.Errorf("[l] = %#v, want AnalyzeUseTxFileMsg", got)
	}
	a = analyzePage(t, writeDoneScenarioState(), 120, 32)
	if got := cmdMsg(t, keyCmd(t, a, 'g')); got != (AnalyzeUseServerMsg{}) {
		t.Errorf("[g] = %#v, want AnalyzeServerMsg", got)
	}

	// The footer offers them only in this state.
	body := ansi.Strip(analyzePage(t, writeDoneScenarioState(), 120, 32).View().Content)
	if !strings.Contains(body, "tx file") || !strings.Contains(body, "server") {
		t.Errorf("footer lacks the use-it-now keys:\n%s", body)
	}
	// Armed-but-unwritten (the picker applied, no write yet) stays quiet.
	armedOnly := writeDoneScenarioState()
	armedOnly.FileWritten = false
	body = ansi.Strip(analyzePage(t, armedOnly, 120, 32).View().Content)
	if strings.Contains(body, "l tx file") {
		t.Errorf("footer must stay quiet before the write lands:\n%s", body)
	}

	// Keys are inert outside the state (goal chosen, run not done).
	st := writeDoneScenarioState()
	st.Status = AnalyzeStatusIdle
	idle := analyzePage(t, st, 120, 32)
	_, cmd := idle.Update(press('l'))
	if cmd != nil {
		t.Errorf("[l] while idle emitted %v, want nothing", cmd())
	}
}

// keyCmd presses a key and fails when the page returned no command.
func keyCmd(t *testing.T, a *Analyze, r rune) tea.Cmd {
	t.Helper()

	_, cmd := a.Update(press(r))
	if cmd == nil {
		t.Fatalf("key %q returned no cmd", r)
	}

	return cmd
}

// TestAnalyzeUseKeysKeepGoalRadios: t stays the transactions-goal radio even
// in the post-write state, and [l]/[g] stay silent on the matching step
// (page keys are step-local).
func TestAnalyzeUseKeysKeepGoalRadios(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, writeDoneScenarioState(), 120, 32)
	if got := cmdMsg(t, keyCmd(t, a, 't')); got != (AnalyzeChooseGoalMsg{Goal: AnalyzeGoalTransactions}) {
		t.Errorf("[t] = %#v, must stay the transactions goal radio", got)
	}

	st := matchState()
	m := analyzePage(t, st, 120, 32)
	_, cmd := m.Update(press('l'))
	if cmd != nil {
		t.Errorf("[l] on the matching step emitted %v, want nothing", cmd())
	}
}
