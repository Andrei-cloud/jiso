// connect_golden_test.go pins the §E dialog body at the wireframe baseline
// 120x32 for the three canonical states (editable form, in-flight progress
// line, final-failure line) and both glyph/colour modes — the
// dashboard/send golden idiom.
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestConnectGoldens(t *testing.T) {
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
		state func() ConnectFormState
	}{
		{"connect_form", func() ConnectFormState {
			st := connectTestState()
			st.Fields[6].Note, st.Fields[6].NoteKind = "loaded", NotePass

			return st
		}},
		{"connect_inflight", func() ConnectFormState {
			st := connectTestState()
			st.InFlight = true
			st.Progress = "attempt 2/3"
			st.Backoff = "backoff 1.5s"

			return st
		}},
		{"connect_failure", func() ConnectFormState {
			st := connectTestState()
			st.Error = "dial tcp: connection refused"

			return st
		}},
	}

	for _, c := range states {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				d := NewConnectDialog(testTheme(t, p.prof))
				d.SetState(c.state())
				_, _ = d.Update(windowSize(120, 32))
				checkGolden(t, c.name+"_"+p.name, d.View())
			})
		}
	}
}

// The header picker overlay gets its own goldens (proposal 04 §A.3): the
// collapsed row, the open list, and the filtered list.
func TestConnectPickerGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	for _, p := range profiles {
		t.Run("connect_picker_open_"+p.name, func(t *testing.T) {
			d := NewConnectDialog(testTheme(t, p.prof))
			d.SetState(connectTestState())
			_, _ = d.Update(windowSize(120, 32))
			_, _ = d.Update(special(tea.KeyTab))
			_, _ = d.Update(special(tea.KeyTab))
			_, _ = d.Update(special(tea.KeyTab)) // focus header
			d.OpenPicker()
			checkGolden(t, "connect_picker_open_"+p.name, d.View())
		})
		t.Run("connect_picker_filter_"+p.name, func(t *testing.T) {
			d := NewConnectDialog(testTheme(t, p.prof))
			d.SetState(connectTestState())
			_, _ = d.Update(windowSize(120, 32))
			_, _ = d.Update(special(tea.KeyTab))
			_, _ = d.Update(special(tea.KeyTab))
			_, _ = d.Update(special(tea.KeyTab))
			d.OpenPicker()
			for _, r := range "vi" {
				_, _ = d.Update(pressKey(r, string(r)))
			}
			checkGolden(t, "connect_picker_filter_"+p.name, d.View())
		})
	}
}
