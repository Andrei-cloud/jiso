package pages

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestTransactionsGoldens pins the §B body (no frame chrome) at the
// wireframe baseline 120x32 for the two canonical states and both
// glyph/colour modes (same harness as the dashboard goldens).
func TestTransactionsGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state TransactionsState
	}{
		{"transactions_empty", TransactionsState{}},
		{"transactions_populated", populatedState()},
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
				page := NewTransactions(testTheme(t, p.prof))
				page.SetState(c.state)
				_, _ = page.Update(windowSize(120, 32))
				checkGolden(t, c.name+"_"+p.name, page.View().Content)
			})
		}
	}
}
