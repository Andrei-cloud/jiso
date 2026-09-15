package widgets

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// --- fidelity replicas of the pages' box helpers (RULING R2) ------------
//
// internal/tui/widgets may not import internal/tui/pages, so these
// functions mirror the live helpers VERBATIM (pages/layout.go boxStyle +
// cardBox, pages/sessions_view.go sectionW, pages/server_view.go
// sectionW, pages/ctf_view.go sectionW, pages/inspector_split.go
// sectionW). section.go must reproduce their bytes exactly; Phase 6
// migrates every call site onto Section and the pages goldens must not
// move.
//
// Mode mapping (documented for Phase 6):
//   - ModeStandard == pages sectionW/cardBox math:
//     Width(max(w,4)).Height(inner+2) — sessions_view.go, ctf_view.go,
//     inspector_split.go (pre-styled title), layout.go cardBox (caller
//     passes h = bodyH+3).
//   - ModeServer == server_view.go sectionW math:
//     Width(max(w-2,1)).Height(inner) — the box renders two cells
//     narrower than w (the UAT round 5 ROUTES wrap fix).

// legacyBoxStyle mirrors the pages boxStyle idiom (all 11 copies are identical).
func legacyBoxStyle(th *theme.Theme) lipgloss.Style {
	b := lipgloss.RoundedBorder()
	if th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return lipgloss.NewStyle().
		Border(b).
		BorderForeground(th.Border.GetBorderTopForeground())
}

// legacyClipCells mirrors pages clipCells.
func legacyClipCells(s string, w int, tail string) string {
	if lipgloss.Width(s) <= w {
		return s
	}

	return ansi.Truncate(s, w, tail)
}

// legacyClipBlock mirrors pages clipBlockStyled.
func legacyClipBlock(th *theme.Theme, body string, h, maxW int) string {
	src := strings.Split(strings.TrimRight(body, "\n"), "\n")

	lines := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if i < len(src) {
			lines = append(lines, legacyClipCells(src[i], maxW, th.Ellipsis()))
		} else {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

// legacySectionW mirrors the pages sectionW idiom. serverMath selects the
// server_view.go width/height parameters; the title is re-rendered through
// the Accent style exactly as titleLine does (the sessions/server/ctf
// convention, including the double-render when callers pass a styled
// paneTitle).
func legacySectionW(th *theme.Theme, title, body string, w, h int, focused, serverMath bool) string {
	inner := max(h-3, 1)
	box := legacyClipBlock(th, body, inner, max(w-4, 1))
	style := legacyBoxStyle(th)
	if focused {
		style = style.BorderForeground(th.Accent.GetForeground())
	}
	if serverMath {
		return th.Accent.Render(title) + "\n" +
			style.Width(max(w-2, 1)).Height(inner).Render(box)
	}

	return th.Accent.Render(title) + "\n" +
		style.Width(max(w, 4)).Height(inner+2).Render(box)
}

// --- behaviour tests (task brief) ----------------------------------------

func TestSectionReturnsRectAtOrigin(t *testing.T) {
	s := NewSection(theme.NewWith(colorprofile.TrueColor, true), "ROUTES")
	_, r := s.Render("body", 20, 10, 40, 6)
	if r != (geom.Rect{X: 20, Y: 10, W: 40, H: 6}) {
		t.Fatalf("Rect = %v, want {20 10 40 6}", r)
	}
}

func TestSectionDrawsTitleAndBorder(t *testing.T) {
	s := NewSection(theme.NewWith(colorprofile.ASCII, true), "ROUTES")
	got, _ := s.Render("hello", 0, 0, 12, 4)
	if !strings.Contains(got, "ROUTES") {
		t.Fatalf("section must show its title, got:\n%s", got)
	}
	// The ASCII profile draws the ASCII border set, and stays 7-bit.
	if !strings.Contains(got, "+") || !strings.Contains(got, "|") {
		t.Fatalf("ascii section must draw the ASCII border, got:\n%s", got)
	}
	for _, r := range got {
		if r > 127 || r == 0x1b {
			t.Fatalf("ascii section emitted non-7-bit byte %q in:\n%s", r, got)
		}
	}
}

func TestSectionFocusedChangesBorder(t *testing.T) {
	th := theme.NewWith(colorprofile.TrueColor, true)

	plain := NewSection(th, "ROUTES")
	lit := NewSection(th, "ROUTES")
	lit.Focused = true

	unfocused, _ := plain.Render("body", 0, 0, 20, 5)
	focused, _ := lit.Render("body", 0, 0, 20, 5)
	if focused == unfocused {
		t.Fatal("focused section must render its border differently")
	}
	// The exact accent bytes are pinned by the fidelity tests below.
}

// --- fidelity tests (RULING R2: byte-identical to the pages helpers) -----

type fidelityFixture struct {
	bodies []string
	sizes  [][2]int
}

func fidelityFixtures() fidelityFixture {
	return fidelityFixture{
		bodies: []string{
			"body",
			"hello\nworld",
			"a\n\nlong line that will definitely exceed the box width and must clip",
			"multi\nline\nbody\nwith\ntrailing\nblank\nlines\n\n\n",
			"",
		},
		// The last four guard the degenerate boxes: w<4 and h<4.
		sizes: [][2]int{{12, 4}, {40, 6}, {20, 10}, {60, 20}, {4, 4}, {3, 5}, {5, 3}, {6, 2}},
	}
}

func fidelityProfiles() []struct {
	name string
	prof colorprofile.Profile
} {
	return []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}
}

// TestSectionFidelityStandardMode asserts Section.Render equals the
// sessions/ctf/inspector sectionW and dashboard cardBox math, unfocused
// and focused, under both pinned profiles.
func TestSectionFidelityStandardMode(t *testing.T) {
	t.Parallel()

	fx := fidelityFixtures()
	for _, prof := range fidelityProfiles() {
		for _, focused := range []bool{false, true} {
			for _, size := range fx.sizes {
				for _, body := range fx.bodies {
					w, h := size[0], size[1]
					th := theme.NewWith(prof.prof, true)
					want := legacySectionW(th, "ROUTES", body, w, h, focused, false)

					s := NewSection(th, "ROUTES")
					s.Focused = focused
					got, _ := s.Render(body, 0, 0, w, h)

					if got != want {
						t.Errorf("%s focused=%v w=%d h=%d body=%q:\ngot:  %q\nwant: %q",
							prof.name, focused, w, h, body, got, want)
					}
				}
			}
		}
	}
}

// TestSectionFidelityServerMode asserts Section.Render with ModeServer
// equals the server_view.go sectionW math (box two cells narrower).
func TestSectionFidelityServerMode(t *testing.T) {
	t.Parallel()

	fx := fidelityFixtures()
	for _, prof := range fidelityProfiles() {
		for _, focused := range []bool{false, true} {
			for _, size := range fx.sizes {
				for _, body := range fx.bodies {
					w, h := size[0], size[1]
					th := theme.NewWith(prof.prof, true)
					want := legacySectionW(th, "ROUTES", body, w, h, focused, true)

					s := NewSection(th, "ROUTES")
					s.Mode = ModeServer
					s.Focused = focused
					got, _ := s.Render(body, 0, 0, w, h)

					if got != want {
						t.Errorf("%s focused=%v w=%d h=%d body=%q:\ngot:  %q\nwant: %q",
							prof.name, focused, w, h, body, got, want)
					}
				}
			}
		}
	}
}

// TestSectionRectIsInputSizeInBothModes pins the hit-test contract: the
// Rect is the requested box, even when the drawn output is taller than h
// (h < 4 clamps the body to one row, the existing sectionW behaviour).
func TestSectionRectIsInputSizeInBothModes(t *testing.T) {
	th := theme.NewWith(colorprofile.TrueColor, true)

	for _, mode := range []SectionMode{ModeStandard, ModeServer} {
		s := NewSection(th, "T")
		s.Mode = mode
		_, r := s.Render("body", 7, 3, 10, 2)
		if r != (geom.Rect{X: 7, Y: 3, W: 10, H: 2}) {
			t.Errorf("mode %v: Rect = %v, want {7 3 10 2}", mode, r)
		}
	}
}

// TestSectionPreStyledTitlePassesThrough pins the inspector_split.go
// convention: a title the caller already styled is emitted byte-for-byte,
// never re-rendered through the Accent style.
func TestSectionPreStyledTitlePassesThrough(t *testing.T) {
	th := theme.NewWith(colorprofile.TrueColor, true)
	styled := th.Accent.Render("PACKED MESSAGE")

	s := NewSection(th, styled)
	s.TitlePreStyled = true
	got, _ := s.Render("body", 0, 0, 30, 5)

	if !strings.HasPrefix(got, styled+"\n") {
		t.Fatalf("pre-styled title must pass through verbatim, got:\n%q", got)
	}

	// The default path mirrors titleLine: the raw title is Accent-rendered.
	d := NewSection(th, "ROUTES")
	gotDefault, _ := d.Render("body", 0, 0, 30, 5)
	if !strings.HasPrefix(gotDefault, th.Accent.Render("ROUTES")+"\n") {
		t.Fatalf("title must be accent-rendered, got:\n%q", gotDefault)
	}
}
