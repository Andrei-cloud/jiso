// section.go holds the ONE titled, bordered box every screen section is
// assembled from (TUI remediation Phase 1). It replaces the 13 duplicated
// boxStyle() and 5 sectionW()/cardBox() copies in internal/tui/pages;
// the fidelity tests in section_test.go pin its bytes to the helpers it
// replaces so the Phase 6 migration cannot move a pixel.
package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// SectionMode selects which of the two live box-layout conventions
// Render reproduces byte-for-byte. Both exist in internal/tui/pages
// today; the Phase 6 mapping is:
//
//   - ModeStandard: pages/layout.go cardBox, pages/sessions_view.go
//     sectionW, pages/ctf_view.go sectionW, pages/inspector_split.go
//     sectionW (with TitlePreStyled).
//   - ModeServer: pages/server_view.go sectionW only.
type SectionMode int

const (
	// ModeStandard is the pages sectionW/cardBox convention: the
	// bordered box spans the full width w (lipgloss Width max(w,4),
	// which in lipgloss v2 includes the border) and the body clips to
	// w-4 cells.
	ModeStandard SectionMode = iota
	// ModeServer is the server_view.go sectionW convention: the box is
	// drawn two cells narrower than w (Width max(w-2,1), the UAT round 5
	// ROUTES-table wrap fix) and the body clips to w-4 cells. The total
	// height matches ModeStandard because the clipped body always fills
	// the Height pad target.
	ModeServer
)

// Section is the one titled, bordered box used for every screen section:
// a title line above a body clipped into a bordered box of total size
// w×h (h includes the title line), the focused pane's border in the
// accent colour. Render returns both the string and the geom.Rect it was
// asked to occupy, so the caller can hit-test the section.
//
// Like the helpers it replaces, a height below 4 clamps the body to one
// row and the drawn box is then taller than h; the returned Rect is
// still the requested box (the layout grid, not the ink).
type Section struct {
	Theme   *theme.Theme
	Title   string
	Focused bool
	Mode    SectionMode

	// TitlePreStyled passes Title through unrendered, for callers that
	// style the title themselves (the inspector_split.go convention).
	// Left false, Title is rendered with the Accent style exactly like
	// the pages titleLine does — including the accent-over-muted
	// double-render the sessions/server/ctf call sites produce today.
	TitlePreStyled bool
}

// NewSection builds a ModeStandard section with an Accent-rendered title.
func NewSection(th *theme.Theme, title string) *Section {
	return &Section{Theme: th, Title: title}
}

// Render draws the section at inclusive origin (x,y) sized w×h and
// returns the string plus its geom.Rect. Focused sections draw their
// border in the accent colour (theme.BorderFocused); the rest keep the
// neutral border token.
func (s *Section) Render(body string, x, y, w, h int) (string, geom.Rect) {
	inner := max(h-3, 1) // body rows: total h minus the title line and two border rows
	box := clipBlock(s.Theme, body, inner, max(w-4, 1))

	style := s.boxStyle()
	if s.Mode == ModeServer {
		style = style.Width(max(w-2, 1)).Height(inner)
	} else {
		style = style.Width(max(w, 4)).Height(inner + 2)
	}

	title := s.Title
	if !s.TitlePreStyled {
		title = s.Theme.Accent.Render(title)
	}

	return title + "\n" + style.Render(box), geom.Rect{X: x, Y: y, W: w, H: h}
}

// boxStyle is the pane border: rounded normally, ASCII under
// theme.ASCII; the neutral border token, the accent when focused (the
// pages boxStyle idiom, all 11 copies identical).
func (s *Section) boxStyle() lipgloss.Style {
	st := s.Theme.Border
	if s.Focused {
		st = s.Theme.BorderFocused()
	}

	b := lipgloss.RoundedBorder()
	if s.Theme.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return st.Border(b)
}

// clipBlock flattens a body to exactly h lines of at most maxW cells
// (truncate, never wrap; short bodies pad with empty lines so joins and
// the frame stay aligned). It mirrors the pages clipBlockStyled; the
// per-line clip reuses the package's clip, whose newline flattening is
// a no-op on already-split lines.
func clipBlock(th *theme.Theme, body string, h, maxW int) string {
	src := strings.Split(strings.TrimRight(body, "\n"), "\n")

	lines := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if i < len(src) {
			lines = append(lines, clip(src[i], maxW, truncateTail(th)))
		} else {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}
