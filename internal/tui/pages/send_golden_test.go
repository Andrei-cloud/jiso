// send_golden_test.go pins the §D exchange view at the wireframe baseline
// 120x32 for the two terminal states (approved, timeout) and both
// glyph/colour modes (SCR-504; same harness as the dashboard goldens).
package pages

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestSendGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state SendState
	}{
		{"send_done_approved", sendApprovedState()},
		{"send_timeout", sendTimeoutState()},
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
				s := NewSend(testTheme(t, p.prof))
				s.SetState(c.state)
				_, _ = s.Update(windowSize(120, 32))
				checkGolden(t, c.name+"_"+p.name, s.View().Content)
			})
		}
	}
}
