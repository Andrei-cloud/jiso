// ctf_golden_test.go pins the §K body (truecolor + ascii) for the
// populated two-pane view, the filtered list, the preview overlay, the
// empty-eligible state, and the narrow stacked fallback. Fixtures are
// fixed display strings — no clock, no terminal paths — so the bytes
// are deterministic. Regenerate only these with:
// go test ./internal/tui/pages -run TestCtfGoldens -update
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// ctfPreviewState attaches the deterministic preview overlay to the
// populated fixture (record strings are fixed literals).
func ctfPreviewState(th *theme.Theme) CtfState {
	st := ctfFixtureState(th)
	st.Preview = &CtfPreview{
		Headline: []string{"9f3c..a1" + joinSep(th) + "3 records" + joinSep(th) +
			"2 monetary tx" + joinSep(th) + "$ 12.50 total"},
		Records: ctfGoldenRecords(),
		OutPath: "./out/CTF_001.dat",
	}
	st.PreviewID = 1

	return st
}

// ctfGoldenRecords are three fixed 168-char Base II records (space-
// padded): long enough to pass any golden window, so the goldens pin
// the ruler alignment and the column-window clip (UAT round 6).
func ctfGoldenRecords() []string {
	return []string{
		padRight("05004242424242424242000000000000000ARN000100129 090700000000001000 8400000000001000 840GOLDEN MERCHANT ONE", 168),
		padRight("02004242424242424242000000000000000ARN000200129 090700000000001000 8400000000001000 840GOLDEN MERCHANT TWO", 168),
		padRight("9204001290245000000000000001250000000002000001000000000002000000000001250GOLDEN TRAILER RECORD", 168),
	}
}

func TestCtfGoldens(t *testing.T) {
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
		st   func(th *theme.Theme) CtfState
		w    int
		h    int
		keys []tea.Msg
	}
	cases := []golden{
		{"ctf_populated", ctfFixtureState, 120, 32, nil},
		{"ctf_filtered", ctfFixtureState, 120, 32, filterKeys("77b2")},
		{"ctf_preview", ctfPreviewState, 120, 32, nil},
		{"ctf_empty_eligible", func(th *theme.Theme) CtfState {
			return CtfState{DBPath: "./sessions.db"}
		}, 120, 32, nil},
		{"ctf_narrow", ctfFixtureState, 80, 24, nil},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				cl := NewCtf(testTheme(t, p.prof))
				cl.SetState(c.st(cl.th))
				_, _ = cl.Update(windowSize(c.w, c.h))
				for _, k := range c.keys {
					_, _ = cl.Update(k)
				}
				checkGolden(t, c.name+"_"+p.name, cl.View().Content)
			})
		}
	}
}
