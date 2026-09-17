// send_wizard_golden_test.go pins the wizard at the
// baseline 120x32 for its canonical steps (connect form, spec list,
// filtered file list, template list) in both glyph/colour modes.
package pages

import (
	"testing"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

func TestWizardGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	states := []struct {
		name  string
		form  bool
		state func(th *theme.Theme) WizardState
		pre   func(w *SendWizard)
	}{
		{"wizard_connect", true, wizardState, nil},
		{"wizard_spec", false, func(th *theme.Theme) WizardState {
			st := wizardState(th)
			st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}

			return st
		}, nil},
		{"wizard_file_filter", false, func(th *theme.Theme) WizardState {
			st := wizardState(th)
			st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
			st.FileItems[1].Hint = "recents"

			return st
		}, func(w *SendWizard) {
			w.AdvanceStep()
			for _, r := range "pur" {
				_, _ = w.Update(pressKey(r, string(r)))
			}
		}},
		{"wizard_send", false, func(th *theme.Theme) WizardState {
			st := wizardState(th)
			st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
			st.TargetOK = true

			return st
		}, func(w *SendWizard) {
			w.AdvanceStep()
			w.AdvanceStep()
		}},
	}

	for _, c := range states {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				th := testTheme(t, p.prof)
				w := NewSendWizard(th)
				if c.form {
					w.SetConnectForm(connectTestState())
				}
				w.SetState(c.state(th))
				_, _ = w.Update(windowSize(120, 32))
				if c.pre != nil {
					c.pre(w)
				}
				checkGolden(t, c.name+"_"+p.name, w.View())
			})
		}
	}
}
