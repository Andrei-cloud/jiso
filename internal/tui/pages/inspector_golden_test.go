package pages

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestInspectorGoldens pins the §C body (no frame chrome) at the
// Baseline 120x32 for the fields tree (composite expanded)
// and the packed hex pane, in both glyph/colour modes (same harness as
// the dashboard/transactions goldens).
func TestInspectorGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tab  int
	}{
		{"inspector_fields", viewFields},
		{"inspector_packed", viewPacked},
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
				in := NewInspector(testTheme(t, p.prof))
				in.SetState(inspPurchase())
				_, _ = in.Update(windowSize(120, 32))
				// Static state: the fields tab is the Describe
				// output (UAT), so no tree interaction is pinned.
				inspToTab(t, in, c.tab)
				checkGolden(t, c.name+"_"+p.name, in.View().Content)
			})
		}
	}
}
