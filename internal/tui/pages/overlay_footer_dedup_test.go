// overlay_footer_dedup_test.go pins the page-owned inline overlays: while
// one is open its badged in-body hint line is the single hotkey surface, so
// the page's Hints() return no entry whose key that line already shows;
// closing the overlay restores the page's ordinary context hints. The
// truecolor pass proves every in-body key token carries the Theme.Key
// badge (never a hand-rolled style).
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// hintsEqual compares hint lists by value.
func hintsEqual(a, b []frame.KeyHint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// dedupCase drives one page-owned overlay: opened() pushes the overlay
// state and reports the hints while it is open plus a reader of the hints
// after the overlay closes; clean() is the same page without the overlay.
// bodyKeys are the keys the footer must NOT carry while open — listed by
// the in-body line or hijacked by the overlay's own cursor.
type dedupCase struct {
	name     string
	bodyKeys []string
	opened   func(t *testing.T) (whileOpen []frame.KeyHint, afterClose func() []frame.KeyHint)
	clean    func(t *testing.T) []frame.KeyHint
}

// TestPageOverlayHintsDedupWhileOpen — one table, six overlays: no footer
// entry repeats a key the in-body line already lists while the overlay is
// open, and the ordinary hints return after the close.
func TestPageOverlayHintsDedupWhileOpen(t *testing.T) {
	t.Parallel()

	cases := []dedupCase{
		{
			name:     "analyze items",
			bodyKeys: []string{"space", "a", "enter", theme.KeyEsc, theme.KeyNavJK},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				a := analyzePage(t, analyzeTallItemsState(6), 120, 32)
				if !a.itemsOpen {
					t.Fatal("fixture: a fresh ItemsID must arm the picker")
				}

				return a.Hints(), func() []frame.KeyHint {
					a.Update(special(tea.KeyEscape))

					return a.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return analyzePage(t, analyzeFixtureState(), 120, 32).Hints()
			},
		},
		{
			name:     "analyze unparsable",
			bodyKeys: []string{theme.KeyNavJK, theme.KeyEsc},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				a := analyzePage(t, analyzeDoneWithUnparsable(), 120, 32)
				a.Update(press('u'))
				if !a.unparsableOpen {
					t.Fatal("fixture: u must open the unparsable viewer")
				}

				return a.Hints(), func() []frame.KeyHint {
					a.Update(special(tea.KeyEscape))

					return a.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return analyzePage(t, analyzeFixtureState(), 120, 32).Hints()
			},
		},
		{
			name:     "sessions review",
			bodyKeys: []string{"j", "k", theme.KeyEsc},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				st := sessionsFixtureState(asciiTheme(t))
				st.Review = sessionsReviewFixture()
				p := sessionsPageAt(t, st, 120, 40)
				if !p.ReviewOpen() {
					t.Fatal("fixture: the review push must arm the overlay")
				}

				return p.Hints(), func() []frame.KeyHint {
					p.Update(special(tea.KeyEscape))

					return p.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return sessionsPageAt(t, sessionsFixtureState(asciiTheme(t)), 120, 40).Hints()
			},
		},
		{
			name:     "workers summary",
			bodyKeys: []string{theme.KeyEsc},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				st := workersFixtureState()
				st.Summary = workersSummaryFixture()
				p := workersPageAt(t, st, 120, 40)
				if !p.SummaryOpen() {
					t.Fatal("fixture: the summary push must arm the overlay")
				}

				return p.Hints(), func() []frame.KeyHint {
					p.Update(special(tea.KeyEscape))

					return p.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return workersPageAt(t, workersFixtureState(), 120, 40).Hints()
			},
		},
		{
			name:     "ctf records viewer",
			bodyKeys: []string{"w", theme.KeyEsc},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				c := ctfPage(t, 120, 32)
				st := c.state
				st.Preview = &CtfPreview{
					Headline: []string{"2 records"},
					Records:  []string{"0500 ARN0001 GOLDEN", "9204 TRAILER"},
					OutPath:  "./out/CTF_001.dat",
				}
				st.PreviewID = 1
				c.SetState(st)
				if !c.PreviewOpen() {
					t.Fatal("fixture: the preview push must arm the overlay")
				}

				return c.Hints(), func() []frame.KeyHint {
					c.Update(special(tea.KeyEscape))

					return c.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return ctfPage(t, 120, 32).Hints()
			},
		},
		{
			name:     "scenarios step preview",
			bodyKeys: []string{theme.KeyNavJK, theme.KeyEsc},
			opened: func(t *testing.T) ([]frame.KeyHint, func() []frame.KeyHint) {
				st := scenStepsState()
				st.Preview = &ScenarioStepPreview{StepIndex: 2, ScenarioID: "E2E Purchase and Reversal", Loading: true}
				s := scenPage(t, st, 120, 32)
				if !s.StepPreviewOpen() {
					t.Fatal("fixture: the preview push must arm the overlay")
				}

				return s.Hints(), func() []frame.KeyHint {
					s.Update(scenPress(tea.KeyEscape))

					return s.Hints()
				}
			},
			clean: func(t *testing.T) []frame.KeyHint {
				return scenPage(t, scenStepsState(), 120, 32).Hints()
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			whileOpen, afterClose := tc.opened(t)
			for _, key := range tc.bodyKeys {
				for _, h := range whileOpen {
					if h.Key == key {
						t.Errorf("footer hint %+v duplicates the key %q the in-body line shows while %s is open", h, key, tc.name)
					}
				}
			}

			if got, want := afterClose(), tc.clean(t); !hintsEqual(got, want) {
				t.Errorf("after the close the ordinary hints must return:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestPageOverlayBodyHintLinesBadged: the discoverability surface. Each
// overlay's in-body hint line renders every key token with the Theme.Key
// badge — the same style the footer strip uses.
func TestPageOverlayBodyHintLinesBadged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   func(t *testing.T) (string, *theme.Theme)
		tokens []string
	}{
		{"analyze items", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			a := NewAnalyze(th)
			a.SetState(analyzeTallItemsState(6))
			_, _ = a.Update(windowSize(120, 32))

			return a.View().Content, th
		}, []string{"space", "a", "enter", "esc"}},
		{"analyze unparsable", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			a := NewAnalyze(th)
			a.SetState(analyzeDoneWithUnparsable())
			_, _ = a.Update(windowSize(120, 32))
			a.Update(press('u'))

			return a.View().Content, th
		}, []string{"j/k", "esc"}},
		{"sessions review", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			st := sessionsFixtureState(th)
			st.Review = sessionsReviewFixture()
			p := NewSessions(th)
			p.SetState(st)
			_, _ = p.Update(windowSize(120, 40))

			return p.View().Content, th
		}, []string{"j", "k", "esc"}},
		{"workers summary", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			st := workersFixtureState()
			st.Summary = workersSummaryFixture()
			p := NewWorkers(th)
			p.SetState(st)
			_, _ = p.Update(windowSize(120, 40))

			return p.View().Content, th
		}, []string{"esc"}},
		{"ctf records viewer", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			c := NewCtf(th)
			c.SetState(ctfFixtureState(th))
			_, _ = c.Update(windowSize(120, 32))
			st := c.state
			st.Preview = &CtfPreview{
				Headline: []string{"2 records"},
				Records:  []string{"0500 ARN0001", "9204 TRAILER"}, OutPath: "./out/CTF_001.dat",
			}
			st.PreviewID = 1
			c.SetState(st)

			return c.View().Content, th
		}, []string{"w", "esc"}},
		{"scenarios preview", func(t *testing.T) (string, *theme.Theme) {
			th := testTheme(t, colorprofile.TrueColor)
			st := scenStepsState()
			st.Preview = &ScenarioStepPreview{StepIndex: 2, ScenarioID: "E2E Purchase and Reversal", Loading: true}
			s := NewScenarios(th)
			s.SetState(st)
			_, _ = s.Update(windowSize(120, 32))

			return s.View().Content, th
		}, []string{"j", "k", "esc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, th := tc.body(t)
			for _, tok := range tc.tokens {
				if want := th.Key(tok); !strings.Contains(body, want) {
					t.Errorf("%s: in-body hint line lacks the Theme.Key badge for %q", tc.name, tok)
				}
			}
		})
	}
}
