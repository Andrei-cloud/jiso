// analyze_golden_test.go pins the §J wizard body (truecolor + ascii):
// the rail, the capture/spec candidate lists (populated and empty), the
// header list, the run step with its folded inline option rows and
// results preview, the inline field error, the running state, the
// no-match note, and the narrow render. Fixtures are fixed display
// strings — no clock, no real paths — so the bytes are deterministic.
// Regenerate only these with:
// go test ./internal/tui/pages -run TestAnalyzeGoldens -update
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"
)

func TestAnalyzeGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	preview := "dry-run: would write 6 generated ConfigItem(s) to 'transactions/transaction.json'; nothing was written\n" +
		"  transactions: Purchase (0200), Echo (0800)\n" +
		"  datasets: card_pool\n" +
		"  mock routes: route-0200-00"

	cases := []struct {
		name string
		st   AnalyzeState
		w    int
		h    int
	}{
		{"analyze_capture", analyzeGoldenCapture(), 120, 32},
		{"analyze_capture_empty", analyzeGoldenCaptureEmpty(), 120, 32},
		{"analyze_spec", analyzeGoldenSpec(), 120, 32},
		{"analyze_header", analyzeGoldenHeader(), 120, 32},
		{"analyze_run", analyzeGoldenRun(), 120, 32},
		{"analyze_run_done", analyzeGoldenRunDone(preview), 120, 32},
		{"analyze_run_note", analyzeGoldenRunNote(), 120, 32},
		{"analyze_error", analyzeGoldenError(), 120, 32},
		{"analyze_running", analyzeGoldenRunning(), 120, 32},
		{"analyze_narrow", analyzeGoldenRunDone(preview), 80, 24},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				a := NewAnalyze(testTheme(t, p.prof))
				a.SetState(c.st)
				_, _ = a.Update(tea.WindowSizeMsg{Width: c.w, Height: c.h})
				checkGolden(t, c.name+"_"+p.name, a.View().Content)
			})
		}
	}
}

// analyzeGoldenCapture is step 1 with the .pcap candidates listed.
func analyzeGoldenCapture() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepCapture
	st.Status = AnalyzeStatusIdle

	return st
}

// analyzeGoldenCaptureEmpty is step 1 with nothing to offer yet.
func analyzeGoldenCaptureEmpty() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepCapture
	st.Status = AnalyzeStatusIdle
	st.CaptureItems = nil
	st.CapturePath = ""

	return st
}

// analyzeGoldenSpec is step 2 with the *.json candidates listed.
func analyzeGoldenSpec() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepSpec
	st.Status = AnalyzeStatusIdle

	return st
}

// analyzeGoldenHeader is step 3 with the current framing leading.
func analyzeGoldenHeader() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepHeader
	st.Status = AnalyzeStatusIdle

	return st
}

// analyzeGoldenRun is step 4 before the first run: summary, inline
// option rows, and the enumerated flows block.
func analyzeGoldenRun() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle

	return st
}

// analyzeGoldenRunDone is step 4 with the results preview ready to
// write.
func analyzeGoldenRunDone(preview string) AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.Elapsed = "1.2s"
	st.Preview = preview

	return st
}

// analyzeGoldenRunNote is step 4 with a flow filter that matched
// nothing.
func analyzeGoldenRunNote() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	st.FlowFilter = "zzz"
	st.Note = "no flows match the filter - clear it or fix it"

	return st
}

// analyzeGoldenError is step 1 with an inline field error.
func analyzeGoldenError() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepCapture
	st.Status = AnalyzeStatusError
	st.CapturePath = "/tmp/gone.pcap"
	st.CaptureError = "no such file: /tmp/gone.pcap"

	return st
}

// analyzeGoldenRunning is step 4 while the engine runs.
func analyzeGoldenRunning() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusRunning

	return st
}
