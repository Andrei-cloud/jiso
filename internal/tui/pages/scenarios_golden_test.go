// scenarios_golden_test.go pins the §F page at the wireframe baseline
// 120x32 for the three canonical states (all-pass list+steps, live
// running stream, failed step with validation diff) and both
// glyph/colour modes (SCR-506; same harness as the dashboard goldens).
package pages

import (
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
)

func scenPassState() ScenariosState {
	st := scenListState()
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", RC: "00", Status: StepPass, Latency: time.Millisecond},
		{
			Index: 2, Name: "Purchase Authorization", MTI: "0200", RC: "00",
			Note: "extract AuthId=482913 · validate 39=00", Status: StepPass, Latency: 3 * time.Millisecond,
		},
		{
			Index: 3, Name: "Reversal", MTI: "0420", RC: "00",
			Note: "validate 39=00", Status: StepPass, Latency: 3 * time.Millisecond,
		},
	}
	st.Summary = "3/3 passed · 7ms total"

	return st
}

func scenRunningState() ScenariosState {
	st := scenListState()
	st.Running = true
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", RC: "00", Status: StepPass, Latency: time.Millisecond},
		{
			Index: 2, Name: "Purchase Authorization", MTI: "0200", RC: "00",
			Note: "extract AuthId=482913 · validate 39=00", Status: StepPass, Latency: 3 * time.Millisecond,
		},
		{Index: 3, Name: "Reversal", MTI: "0420", Status: StepRunning},
		{Index: 4, Name: "Advice", MTI: "0400", Status: StepPending},
	}

	return st
}

func scenFailState() ScenariosState {
	st := scenListState()
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", RC: "00", Status: StepPass, Latency: time.Millisecond},
		{
			Index: 2, Name: "Purchase Authorization", MTI: "0200", RC: "96",
			Note: `39 expect "00" got "96"`, Status: StepFail, Latency: 2 * time.Millisecond,
		},
		{Index: 3, Name: "Reversal", MTI: "0420", Status: StepPending},
	}
	st.Summary = "1/2 passed · 3ms total"
	st.StatusLine = "report → scenario-report.json"

	return st
}

// scenPreviewLoadedState pins the step message-preview overlay with a
// reconstructed request/response payload (UAT round 9 F-9e c): the
// overlay replaces the panes and the step cursor marker is visible
// again on the next plain frame.
func scenPreviewLoadedState() ScenariosState {
	st := scenPassState()
	st.Preview = &ScenarioStepPreview{
		StepIndex: 2, ScenarioID: "E2E Purchase and Reversal",
		Request: &TxReviewMessage{
			HEX: "0200 F2 3B 38 30 31 38 30 30 30 30 30 30 30 30 30 30",
			Describe: " 2  \"4242424242424242\"\n" +
				" 3  \"000000\"\n" +
				"11  \"0916120000\"",
		},
		Response: &TxReviewMessage{
			HEX:      "0210 F2 3B 38 30 31 38 30 30 30 30 30 30 30 30 30 30",
			Describe: " 2  \"4242424242424242\"\n39  \"00\"",
		},
	}

	return st
}

// scenPreviewLoadingState pins the overlay's in-flight frame: root
// armed the detail load (Loading:true) but the payload has not
// arrived — the honest ".. loading" marker, never an empty pane.
func scenPreviewLoadingState() ScenariosState {
	st := scenPassState()
	st.Preview = &ScenarioStepPreview{
		StepIndex: 3, ScenarioID: "E2E Purchase and Reversal", Loading: true,
	}

	return st
}

func TestScenariosGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state ScenariosState
	}{
		{"scenarios_pass", scenPassState()},
		{"scenarios_running", scenRunningState()},
		{"scenarios_faildiff", scenFailState()},
		{"scenarios_steppreview", scenPreviewLoadedState()},
		{"scenarios_preview_loading", scenPreviewLoadingState()},
	}
	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				page := NewScenarios(testTheme(t, p.prof))
				page.SetState(c.state)
				_, _ = page.Update(windowSize(120, 32))
				checkGolden(t, c.name+"_"+p.name, page.View().Content)
			})
		}
	}
}
