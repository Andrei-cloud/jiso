// workers_golden_test.go pins the §H body (truecolor + ascii) for the
// populated table and the stress-summary overlay. Fixtures are fixed
// display strings — no clock, no terminal paths — so the bytes are
// deterministic. Regenerate only these with:
// go test ./internal/tui/pages -run WorkersGolden -update
package pages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestWorkersGoldens(t *testing.T) {
	t.Parallel()

	states := []struct {
		name string
		st   WorkersState
	}{
		{"workers_populated", workersFixtureState()},
		{"workers_summary", func() WorkersState {
			s := workersFixtureState()
			s.Summary = workersSummaryFixture()

			return s
		}()},
	}
	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	for _, c := range states {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				w := NewWorkers(testTheme(t, p.prof))
				w.SetState(c.st)
				_, _ = w.Update(windowSize(132, 32))
				checkGolden(t, c.name+"_"+p.name, w.View().Content)
			})
		}
	}
}

// TestWorkersOneWidthBasis pins that every block on §H shares one width budget.
// The table used to size itself at wt-2 while the status line, the TPS strip and
// the PROGRESS rows clipped to the full content width, so a progress row could
// run past the right edge of the table it sits under — workers_populated pinned
// a 126-cell PROGRESS row over a 125-cell table, which reads as a bar sticking
// out of the box it belongs to.
//
// The page's own box rules are the authority rather than a hardcoded number, so
// the guard still means something after a breakpoint or fixture change.
func TestWorkersOneWidthBasis(t *testing.T) {
	t.Parallel()

	withSummary := workersFixtureState()
	withSummary.Summary = workersSummaryFixture()

	for _, tc := range []struct {
		name string
		st   WorkersState
	}{
		{"table", workersFixtureState()},
		{"summary overlay", withSummary},
	} {
		w := NewWorkers(asciiTheme(t))
		w.SetState(tc.st)
		_, _ = w.Update(windowSize(132, 32))

		lines := strings.Split(w.View().Content, "\n")

		limit := 0
		for _, l := range lines {
			if isBoxRule(l) {
				limit = max(limit, lipgloss.Width(l))
			}
		}

		if limit == 0 {
			t.Fatalf("%s: the page drew no box rule, so the guard has nothing to measure against", tc.name)
		}

		for i, l := range lines {
			if n := lipgloss.Width(l); n > limit {
				t.Errorf("%s: line %d is %d cells wide, past the page's own %d-cell rule — every block on a page takes the same width budget",
					tc.name, i+1, n, limit)
			}
		}
	}
}

// boxRuleRunes are the runes a horizontal box rule is made of, in both glyph
// sets; a line of nothing else is the page's width authority.
const boxRuleRunes = "+-|─│┌┬┐├┼┤└┴┘ "

// isBoxRule reports a line that is entirely a box rule.
func isBoxRule(line string) bool {
	s := lipgloss.NewStyle().Inline(true).Render(line)
	if s == "" {
		return false
	}

	for _, r := range strings.TrimRight(s, " \t") {
		if !strings.ContainsRune(boxRuleRunes, r) {
			return false
		}
	}

	return true
}
