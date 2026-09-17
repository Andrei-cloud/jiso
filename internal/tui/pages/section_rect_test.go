// section_rect_test.go pins that every Rect a page records lands on the
// DRAWN box ink (border glyphs at the recorded columns, ink at the title
// cell), and that re-rendering neither grows nor moves the record.
package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// sectionInkBorder is the rune set a box line may show at its recorded
// left/right columns.
func sectionInkBorder(r rune) bool {
	switch r {
	case '|', '+', '-',
		'│', '┌', '┐', '└', '┘', '─',
		'╭', '╮', '╰', '╯':
		return true
	}

	return false
}

// cells lays the ANSI-stripped line out by CELLS: a wide rune occupies two
// entries, its filler entry 0 so a corner landing on it fails the check.
func cells(line string) []rune {
	plain := ansi.Strip(line)
	out := make([]rune, 0, len(plain))
	for _, r := range plain {
		w := ansi.StringWidth(string(r))
		out = append(out, r)
		for i := 1; i < w; i++ {
			out = append(out, 0)
		}
	}

	return out
}

// assertSectionInk checks each Rect against the rendered body: box glyphs at
// cells X and X+W-1 on every visible box line, non-blank ink at the title
// cell. Lines the final clip chops off the bottom are skipped.
func assertSectionInk(t *testing.T, page string, sections []geom.Rect) {
	t.Helper()

	if len(sections) == 0 {
		t.Fatal("page recorded no section rects")
	}

	lines := strings.Split(strings.TrimRight(page, "\n"), "\n")

	for i, r := range sections {
		if r.W < 3 || r.H < 3 {
			t.Fatalf("section %d: degenerate rect %v", i, r)
		}

		visible := min(r.Y+r.H, len(lines))
		if visible < r.Y+2 {
			t.Fatalf("section %d: rect %v does not even show its title line and top border in the %d-line body",
				i, r, len(lines))
		}

		title := cells(lines[r.Y])
		if len(title) <= r.X || title[r.X] == ' ' || title[r.X] == 0 {
			t.Errorf("section %d: title line %d cell %d lacks title ink (line %q)",
				i, r.Y, r.X, ansi.Strip(lines[r.Y]))
		}

		for li := r.Y + 1; li < visible; li++ {
			row := cells(lines[li])
			if len(row) < r.X+r.W {
				t.Fatalf("section %d: line %d is %d cells wide, want at least %d (rect %v): %q",
					i, li, len(row), r.X+r.W, r, ansi.Strip(lines[li]))
			}
			left, right := row[r.X], row[r.X+r.W-1]
			if !sectionInkBorder(left) || !sectionInkBorder(right) {
				t.Errorf("section %d: line %d edges (%d)=%q (%d)=%q are not border glyphs; rect %v line %q",
					i, li, r.X, left, r.X+r.W-1, right, r, ansi.Strip(lines[li]))
			}
		}
	}
}

// rectPage renders one page at one profile/size; again re-renders the SAME
// page so the test can pin the per-render reset.
type rectPage struct {
	name  string
	build func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect)
}

func assertRectPages(t *testing.T, cases []rectPage) {
	t.Helper()

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
				th := testTheme(t, p.prof)
				body, sections, again := c.build(t, th)

				assertSectionInk(t, body, sections)

				resheld := again()
				if len(resheld) != len(sections) {
					t.Fatalf("re-render recorded %d rects, want %d (the per-render reset leaked)",
						len(resheld), len(sections))
				}
				for i := range resheld {
					if resheld[i] != sections[i] {
						t.Fatalf("rect %d moved between identical renders: %v vs %v", i, sections[i], resheld[i])
					}
				}
			})
		}
	}
}

// recorded copies the live slice so first-render values survive re-renders.
func recorded(rs []geom.Rect) []geom.Rect {
	return append([]geom.Rect(nil), rs...)
}

func TestDashboardSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	dash := func(w, h int) func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
		return func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			d := NewDashboard(th)
			d.SetState(logGoldState())
			_, _ = d.Update(windowSize(w, h))
			body := d.View().Content
			first := recorded(d.sections)

			return body, first, func() []geom.Rect {
				d.View()

				return d.sections
			}
		}
	}

	assertRectPages(t, []rectPage{
		{"dashboard_two_col", dash(150, 44)},
		{"dashboard_medium", dash(110, 36)},
		{"dashboard_stacked", dash(80, 32)},
	})
}

func TestSessionsSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	assertRectPages(t, []rectPage{
		{"sessions_split", func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewSessions(th)
			s.SetState(sessionsFixtureState(th))
			_, _ = s.Update(windowSize(120, 32))
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}},
		{"sessions_drill", func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewSessions(th)
			s.SetState(sessionsFixtureState(th))
			_, _ = s.Update(windowSize(80, 24))
			s.drill = true
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}},
	})
}

func TestCtfSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	ctf := func(w, h int) func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
		return func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			c := NewCtf(th)
			c.SetState(ctfFixtureState(th))
			_, _ = c.Update(windowSize(w, h))
			body := c.View().Content
			first := recorded(c.sections)

			return body, first, func() []geom.Rect {
				c.View()

				return c.sections
			}
		}
	}

	assertRectPages(t, []rectPage{
		{"ctf_wide", ctf(120, 32)},
		{"ctf_stacked", ctf(80, 24)},
	})
}

func TestServerSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	server := func(w, h int, withLog bool) func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
		return func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewServer(th)
			st := serverRunningState()
			if withLog {
				st.Log = serverLogFixture()
			}
			s.SetState(st)
			_, _ = s.Update(windowSize(w, h))
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}
	}

	assertRectPages(t, []rectPage{
		{"server_nolog_wide", server(120, 32, false)},
		{"server_log_wide", server(160, 40, true)},
		{"server_log_narrow", server(80, 24, true)},
		{"server_nolog_narrow", server(80, 24, false)},
	})
}

func TestInspectorSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	assertRectPages(t, []rectPage{
		{"inspector_split", func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			in := NewInspector(th)
			in.SetState(inspPurchase())
			_, _ = in.Update(windowSize(120, 32))
			body := in.View().Content
			first := recorded(in.sections)

			return body, first, func() []geom.Rect {
				in.View()

				return in.sections
			}
		}},
		{"inspector_packed_full", func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			in := NewInspector(th)
			in.SetState(inspPurchase())
			_, _ = in.Update(windowSize(120, 32))
			inspToTab(t, in, viewPacked)
			body := in.View().Content
			first := recorded(in.sections)

			return body, first, func() []geom.Rect {
				in.View()

				return in.sections
			}
		}},
	})
}

func TestScenariosSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	scen := func(w, h int) func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
		return func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewScenarios(th)
			s.SetState(scenPassState())
			_, _ = s.Update(windowSize(w, h))
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}
	}

	assertRectPages(t, []rectPage{
		{"scenarios_wide", scen(120, 32)},
		{"scenarios_stacked", scen(80, 24)},
	})
}

func TestSendSectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	send := func(w, h int) func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
		return func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewSend(th)
			s.SetState(sendApprovedState())
			_, _ = s.Update(windowSize(w, h))
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}
	}

	assertRectPages(t, []rectPage{
		{"send_side_by_side", send(120, 32)},
		{"send_stacked", send(80, 24)},
	})
}

func TestSendHistorySectionRectsLandOnInk(t *testing.T) {
	t.Parallel()

	assertRectPages(t, []rectPage{
		{"send_history", func(t *testing.T, th *theme.Theme) (string, []geom.Rect, func() []geom.Rect) {
			s := NewSendHistory(th)
			s.SetEntries([]SendHistoryEntry{
				{State: sendApprovedState(), At: time.Unix(0, 0).Add(time.Hour)},
				{State: sendTimeoutState(), At: time.Unix(0, 0).Add(2 * time.Hour)},
			})
			_, _ = s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			body := s.View().Content
			first := recorded(s.sections)

			return body, first, func() []geom.Rect {
				s.View()

				return s.sections
			}
		}},
	})
}
