// sessions_golden_test.go pins the §I body (truecolor + ascii) for the
// populated three-pane view, the filtered list, the empty database, the
// narrow-width fallback, and the reconstructed tx review overlay.
// Fixtures carry fixed data — no clock, no terminal paths — and any cell the
// theme renders (the short ids) is derived from it per profile, so the bytes are
// deterministic and match what the root would hand the page. Regenerate with:
// go test ./internal/tui/pages -run SessionsGolden -update
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

func TestSessionsGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	// st is a constructor, not a value: the state carries cells the theme renders
	// (the short ids), so each profile has to build its own.
	type golden struct {
		name string
		st   func(th *theme.Theme) SessionsState
		w    int
		h    int
		keys []tea.Msg // typed after SetState (filter mode etc.)
	}
	cases := []golden{
		{"sessions_populated", sessionsFixtureState, 120, 32, nil},
		{"sessions_filtered", sessionsFixtureState, 120, 32, filterKeys("77b2")},
		{"sessions_empty", func(*theme.Theme) SessionsState {
			return SessionsState{DBPath: "./sessions.db"}
		}, 120, 32, nil},
		{"sessions_narrow", sessionsFixtureState, 80, 24, nil},
		{"sessions_review", func(th *theme.Theme) SessionsState {
			st := sessionsFixtureState(th)
			st.Review = sessionsReviewFixture()

			return st
		}, 120, 32, nil},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				th := testTheme(t, p.prof)
				s := NewSessions(th)
				s.SetState(c.st(th))
				_, _ = s.Update(windowSize(c.w, c.h))
				for _, k := range c.keys {
					_, _ = s.Update(k)
				}
				checkGolden(t, c.name+"_"+p.name, s.View().Content)
			})
		}
	}
}

// filterKeys types a filter and commits it (Enter yields the keyboard,
// so the caret stays out of the golden bytes).
func filterKeys(text string) []tea.Msg {
	keys := []tea.Msg{press('/')}
	for _, r := range text {
		keys = append(keys, press(r))
	}

	return append(keys, tea.KeyPressMsg{Code: tea.KeyEnter})
}
