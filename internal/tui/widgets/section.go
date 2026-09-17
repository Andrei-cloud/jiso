// section.go holds the ONE titled, bordered box every screen section is
// assembled from; fidelity tests pin its bytes to the helpers it
// replaced, so migration cannot move a pixel.
package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// SectionMode selects which of the two live box-layout conventions
// Render reproduces byte-for-byte.
type SectionMode int

const (
	// ModeStandard is the pages sectionW/cardBox convention: the box
	// draws exactly w wide (v2 Width includes the border) and the body
	// clips to w-4 cells.
	ModeStandard SectionMode = iota
	// ModeServer is the server_view sectionW convention: the box is
	// drawn w-2 wide and the body clips to w-4 cells; the clipped body
	// always fills the Height target, so the total height matches
	// ModeStandard.
	ModeServer
)

// Section is the one titled, bordered box used for every screen section:
// a title line above a body clipped into a bordered box of total size
// w×h (h includes the title line), the focused pane's border in the
// accent colour. Render returns the string plus the requested geom.Rect
// (the layout grid, not the ink); a height below 4 clamps the body to
// one row and draws a box taller than h. Callers advance neighboring
// origins by the DRAWN width.
type Section struct {
	Theme   *theme.Theme
	Title   string
	Focused bool
	Mode    SectionMode

	// TitlePreStyled passes Title through unrendered for callers that
	// style it themselves; left false, Title gets the Accent style
	// (including its accent-over-muted double-render).
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

	style := s.Border()
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

// Border is the title-less mode of the shared box: the frame style of a
// section carrying NO title line, composed by callers with their own
// Width/Height/clip maths (its bytes are pinned by the fidelity tests).
// A titled box goes through Section.Render instead, which draws this
// border under its title line.
func Border(th *theme.Theme, focused bool) lipgloss.Style {
	st := th.Border
	if focused {
		st = th.BorderFocused()
	}

	b := lipgloss.RoundedBorder()
	if th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return st.Border(b)
}

// Border returns this section's frame style: the neutral border token,
// accent when Focused.
func (s *Section) Border() lipgloss.Style { return Border(s.Theme, s.Focused) }

// clipBlock flattens a body to exactly h lines of at most maxW cells
// (truncate, never wrap; short bodies pad with empty lines so joins and
// the frame stay aligned).
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
