// worker_wizard_golden_test.go pins the §H worker wizard body
// (truecolor + ascii): the stress tx step with its 5-row
// scroll window (checked boxes, the "N selected" line, and the
// below/above markers), the empty state, the scrolled window, the
// rate step (focused row + inline bounds error), the run step (ready,
// in-flight progress, failure line), and the bgsend tx/params/run
// steps with their radios. Fixtures are fixed display strings — no
// clock, no real paths — so the bytes are deterministic. Regenerate
// only these with:
// go test ./internal/tui/pages -run TestWorkerWizardGoldens -update
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"
)

func TestWorkerWizardGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	cases := []struct {
		name  string
		mode  string
		n     int
		keys  []tea.Msg
		stamp func(*WorkerWizard)
	}{
		{
			"worker_stress_tx", WorkerModeStress, 7,
			[]tea.Msg{space(), ch('j')},
			nil,
		},
		{"worker_stress_tx_empty", WorkerModeStress, 0, nil, nil},
		{
			"worker_stress_tx_scrolled", WorkerModeStress, 12,
			[]tea.Msg{ch('j'), ch('j'), ch('j'), ch('j'), ch('j'), ch('j')},
			nil,
		},
		{
			"worker_stress_rate", WorkerModeStress, 2,
			[]tea.Msg{space(), enter()},
			nil,
		},
		{
			"worker_stress_rate_error", WorkerModeStress, 2,
			[]tea.Msg{space(), enter(), backspace(), backspace(), ch('0'), enter()},
			nil,
		},
		{
			"worker_stress_run", WorkerModeStress, 2,
			[]tea.Msg{space(), enter(), enter()},
			nil,
		},
		{
			"worker_stress_run_inflight", WorkerModeStress, 2,
			[]tea.Msg{space(), enter(), enter()},
			func(wz *WorkerWizard) {
				st := wz.State()
				st.InFlight = true
				st.Progress = "starting 10 tps x1 (1m0s)"
				wz.SetState(st)
			},
		},
		{
			"worker_stress_run_error", WorkerModeStress, 2,
			[]tea.Msg{space(), enter(), enter()},
			func(wz *WorkerWizard) {
				st := wz.State()
				st.Error = "connection refused"
				wz.SetState(st)
			},
		},
		{"worker_bg_tx", WorkerModeBg, 2, nil, nil},
		{"worker_bg_params", WorkerModeBg, 2, []tea.Msg{enter()}, nil},
		{"worker_bg_run", WorkerModeBg, 2, []tea.Msg{enter(), enter()}, nil},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				wz := NewWorkerWizard(testTheme(t, p.prof), c.mode)
				wz.SetState(WorkerWizardState{TxItems: workerTxNames(c.n)})
				wz.HomeSelection()
				_, _ = wz.Update(windowSize(120, 32))
				for _, k := range c.keys {
					_, _ = wz.Update(k)
				}
				if c.stamp != nil {
					c.stamp(wz)
				}
				checkGolden(t, c.name+"_"+p.name, wz.View())
			})
		}
	}
}

func enter() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func backspace() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

func space() tea.Msg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }
