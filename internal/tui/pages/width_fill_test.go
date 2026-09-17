// width_fill_test.go pins that every sectioned page body fills 100% of the
// frame's content width: sections split it by ratio (floor-only clamps) and
// the last section absorbs the remainder, so no gap or horizontal clipping.
package pages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

// fillSizes: medium (80), full (120), and ultra-wide (200) terminals.
var fillSizes = []struct {
	name string
	w, h int
}{
	{"w80", 80, 32},
	{"w120", 120, 32},
	{"w200", 200, 40},
}

// isBoxEdge reports a glyph opening a box line at x=0; '-' is excluded
// (a rule dash is not a box opening).
func isBoxEdge(r rune) bool {
	switch r {
	case '+', '|', '│', '┌', '└', '├', '╭', '╰':
		return true
	}

	return false
}

// assertBodyFills: no line past the content width; every box line opening at
// x=0 closes with border ink exactly on the content edge; and enough box
// lines that the check cannot pass vacuously.
func assertBodyFills(t *testing.T, body string, contentW int) {
	t.Helper()

	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("page rendered no body")
	}

	boxLines := 0

	for i, l := range lines {
		r := []rune(l)
		if len(r) > contentW {
			t.Fatalf("line %d is %d cells, clipping past the %d-cell content area: %q",
				i+1, len(r), contentW, l)
		}

		if len(r) > 0 && isBoxEdge(r[0]) {
			boxLines++
			if len(r) < contentW || !sectionInkBorder(r[contentW-1]) {
				t.Errorf("line %d stops at %d cells, leaving a trailing gap before the %d-cell content edge: %q",
					i+1, len(r), contentW, l)
			}
		}

		// Band lines with a blank left half: the rightmost box ink must
		// reach the edge; a trailing '-' is body text, not box ink.
		if trimmed := strings.TrimRight(l, " "); trimmed != "" {
			last := []rune(trimmed)[len([]rune(trimmed))-1]
			if (last == '|' || last == '│' || last == '+' || last == '┐' || last == '┘' || last == '┤' || last == '╮' || last == '╯') &&
				len([]rune(trimmed)) != contentW {
				t.Errorf("line %d: box ink ends at %d cells instead of the content edge (%d): %q",
					i+1, len([]rune(trimmed)), contentW, l)
			}
		}
	}

	if boxLines < 4 {
		t.Errorf("only %d box lines in the body — the page rendered no sectioned layout?", boxLines)
	}
}

// firstBoxWidth measures the first box's drawn width from its top border
// line ("+---+"): the first line opening with box-edge ink at x=0.
func firstBoxWidth(t *testing.T, body string) int {
	t.Helper()

	for _, l := range strings.Split(body, "\n") {
		r := []rune(l)
		if len(r) == 0 || !isBoxEdge(r[0]) {
			continue
		}

		i := 1
		for i < len(r) && (r[i] == '-' || r[i] == '─') {
			i++
		}

		if i < len(r) && sectionInkBorder(r[i]) {
			return i + 1
		}

		break
	}

	t.Fatal("body has no box top border line")

	return 0
}

func TestSectionedPagesFillContentWidth(t *testing.T) {
	t.Parallel()

	views := []struct {
		name string
		view func(t *testing.T, th *theme.Theme, w, h int) string
	}{
		{"dashboard", func(t *testing.T, th *theme.Theme, w, h int) string {
			d := NewDashboard(th)
			d.SetState(logGoldState())
			_, _ = d.Update(windowSize(w, h))

			return d.View().Content
		}},
		{"server_logs", func(t *testing.T, th *theme.Theme, w, h int) string {
			st := serverRunningState()
			st.Log = serverLogFixture()

			return serverPageProf(t, st, w, h, colorprofile.ASCII).View().Content
		}},
		{"server_nolog", func(t *testing.T, th *theme.Theme, w, h int) string {
			return serverPageProf(t, serverRunningState(), w, h, colorprofile.ASCII).View().Content
		}},
		{"sessions", func(t *testing.T, th *theme.Theme, w, h int) string {
			return sessionsPageAt(t, sessionsFixtureState(th), w, h).View().Content
		}},
		{"scenarios", func(t *testing.T, th *theme.Theme, w, h int) string {
			p := NewScenarios(th)
			p.SetState(scenPassState())
			_, _ = p.Update(windowSize(w, h))

			return p.View().Content
		}},
	}

	for _, v := range views {
		for _, s := range fillSizes {
			t.Run(v.name+"_"+s.name, func(t *testing.T) {
				th := testTheme(t, colorprofile.ASCII)
				body := v.view(t, th, s.w, s.h)
				contentW, _ := frame.ContentSize(s.w, s.h)
				assertBodyFills(t, body, contentW)
			})
		}
	}
}

// At 200 cols the first column follows its ratio, not the old fixed maxima.
func TestSplitWidthsAreRatiosAtWide(t *testing.T) {
	t.Parallel()

	const (
		w, h = 200, 40
		// frame.ContentSize(200, 40) content width.
		contentW = 196
	)

	t.Run("dashboard_left_is_35_percent", func(t *testing.T) {
		d := dashDashboard(t, logGoldState(), w, h)
		// 196*35/100 = 68.
		if got := firstBoxWidth(t, d.View().Content); got != 68 {
			t.Errorf("wide left column = %d cells, want 68 (35%% of %d)", got, contentW)
		}
	})

	t.Run("server_stats_is_20_percent", func(t *testing.T) {
		st := serverRunningState()
		st.Log = serverLogFixture()
		s := serverPage(t, st, w, h)
		// 196/5 = 39.
		if got := firstBoxWidth(t, s.View().Content); got != 39 {
			t.Errorf("stats column = %d cells, want 39 (20%% of %d)", got, contentW)
		}
	})

	t.Run("sessions_list_is_25_percent", func(t *testing.T) {
		th := asciiTheme(t)
		s := sessionsPageAt(t, sessionsFixtureState(th), w, h)
		// 196/4 = 49.
		if got := firstBoxWidth(t, s.View().Content); got != 49 {
			t.Errorf("sessions list = %d cells, want 49 (25%% of %d)", got, contentW)
		}
	})

	t.Run("scenarios_list_is_33_percent", func(t *testing.T) {
		p := NewScenarios(asciiTheme(t))
		p.SetState(scenPassState())
		_, _ = p.Update(windowSize(w, h))
		// 196/3 = 65.
		if got := firstBoxWidth(t, p.View().Content); got != 65 {
			t.Errorf("scenarios list = %d cells, want 65 (33%% of %d)", got, contentW)
		}
	})
}
