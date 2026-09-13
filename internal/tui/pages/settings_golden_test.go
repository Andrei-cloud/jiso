// settings_golden_test.go pins the §L body (truecolor + ascii) for the
// populated grid, the narrow stacked fallback, the save-confirm
// overlay with the changed-keys diff, and the invalid-field state
// (attempted draft + inline error). Fixtures are fixed display
// strings — no clock, no terminal paths — so the bytes are
// deterministic. Regenerate only these with:
// go test ./internal/tui/pages -run TestSettingsGoldens -update
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// settingsOverlayState attaches the deterministic save-confirm overlay
// to the populated fixture (two changed keys, one absent from the
// file).
func settingsOverlayState(th *theme.Theme) SettingsState {
	st := settingsFixtureState(th)
	st.Save = &SettingsSaveOverlay{
		Path: "./user/config.yaml",
		Diff: []string{"connect-timeout: 5s -> 8s", "hex: off -> on"},
	}

	return st
}

// settingsInvalidState replays a rejected commit: the draft "bogus"
// stays visible beside the inline validation error (root folds the
// per-field ApplySettings error back into the row).
func settingsInvalidState(th *theme.Theme) SettingsState {
	return settingsFixtureState(th)
}

func TestSettingsGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	type golden struct {
		name string
		st   func(th *theme.Theme) SettingsState
		w    int
		h    int
		keys []tea.Msg
		post func(s *Settings)
	}
	cases := []golden{
		{"settings_populated", settingsFixtureState, 120, 32, nil, nil},
		{"settings_narrow", settingsFixtureState, 80, 24, nil, nil},
		{"settings_save_overlay", settingsOverlayState, 120, 32, nil, nil},
		{"settings_invalid_field", settingsInvalidState, 120, 32, []tea.Msg{
			press('j'),
			tea.KeyPressMsg{Code: tea.KeyEnter},
			tea.KeyPressMsg{Code: tea.KeyBackspace},
			tea.KeyPressMsg{Code: tea.KeyBackspace},
			press('b'), press('o'), press('g'), press('u'), press('s'),
			tea.KeyPressMsg{Code: tea.KeyEnter},
		}, func(s *Settings) {
			st := settingsFixtureState(s.th)
			st.Rows[1].Error = "invalid duration \"bogus\""
			s.SetState(st)
		}},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				s := NewSettings(testTheme(t, p.prof))
				s.SetState(c.st(s.th))
				_, _ = s.Update(windowSize(c.w, c.h))
				for _, k := range c.keys {
					_, _ = s.Update(k)
				}
				if c.post != nil {
					c.post(s)
				}
				checkGolden(t, c.name+"_"+p.name, s.View().Content)
			})
		}
	}
}
