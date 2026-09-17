// layout.go renders the §A body: two card columns at wide/medium content
// widths, a priority stack at narrow, shrinking bodies bottom-up and
// dropping the lowest-priority cards first (CONNECTION and QUICK ACTIONS
// never drop). Sizing comes from frame.ContentSize; the frame owns the
// surrounding chrome.
package pages

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Section titles. The page renders titles + boxes; the frame owns the
// chrome. MOCK SERVER (titleServer) and SERVER LOG (titleLog) reuse the
// §G page's constants.
const (
	titleConnection = "CONNECTION"
	titleLastSend   = "LAST SEND"
	titleLastStress = "LAST STRESS"
	titleSession    = "SESSION"
	titleActions    = "QUICK ACTIONS"

	// Card-body next-action key glyphs (rendered with the HotKey style
	// inside card bodies; the tile TITLES carry no badges).
	hotkeyConnection = "c"
	hotkeyLastSend   = "s"
	hotkeyMockServer = "4"

	// Grid breakpoints; the wide left column is sized relative to the
	// terminal (fixed columns do not adapt); see dashLeftCol.
	dashWideCols   = 130
	dashMediumCols = 100
	// dashColGap is the blank row/column between cards in the two-column
	// grid; the narrow stack uses zero gap so the seven minimum-size
	// cards still fit a 28-row content area.
	dashColGap = 1

	// narrowLogRows caps the SERVER LOG card body rows in the narrow
	// stack; the two-column grid lets the card absorb spare height.
	narrowLogRows = 4

	// cardOverhead is the line cost of one card: title + two border rows.
	cardOverhead = 3
)

// Card identity tags for the fitter (the actions card renders its widget
// lazily at the allocated height).
const (
	cardConn    = "conn"
	cardServer  = "server"
	cardLog     = "log"
	cardSend    = "send"
	cardSession = "session"
	cardActions = "actions"
	cardStress  = "stress"
)

// View renders the §A body for the frame's content area (sizing delegated
// to frame.ContentSize so page and chrome never disagree).
func (d *Dashboard) View() tea.View {
	w, h := frame.ContentSize(d.width, d.height)

	return tea.NewView(d.render(w, h))
}

// dashCard is one grid card: a pre-built body plus the fitter's
// allocation. body renders at most kept lines; the log card renders its
// TAIL so shrinking drops the oldest lines, never the newest.
type dashCard struct {
	id      string
	title   string
	body    func(w, kept int) []string
	natural int
	kept    int
	min     int
	// never marks the cards the height fitter must not drop even when
	// the column cannot fit (CONNECTION and QUICK ACTIONS).
	never bool
	// bottom aligns the body to the last line of the box instead of the
	// first, for the SERVER LOG: a band taller than the buffer must not push the
	// newest line to the top of an empty box, which is what a tail -f does not do.
	bottom bool
}

func (d *Dashboard) newCard(id, title string, body func(w, kept int) []string, natural int) *dashCard {
	if natural < 1 {
		natural = 1
	}

	return &dashCard{
		id: id, title: title, body: body,
		natural: natural, kept: natural, min: 1,
	}
}

// render picks the responsive shape from the content width.
func (d *Dashboard) render(w, h int) string {
	d.sections = d.sections[:0] // redraw the section rects alongside the ink

	switch {
	case w >= dashWideCols:
		return d.renderTwoCol(w, h, dashLeftCol(w))
	case w >= dashMediumCols:
		return d.renderTwoCol(w, h, (w-dashColGap)/2)
	default:
		return d.renderStacked(w, h)
	}
}

// dashLeftCol sizes the wide-grid left column as a 35% ratio of the
// content width with only a floor clamp. The right column absorbs the
// remainder, so one band sums exactly to the content width.
func dashLeftCol(w int) int {
	return max(w*35/100, 40)
}

// cardBox renders the plain title (no key badge) over the body clipped
// into a bordered box of total size w×(bodyH+3), through the shared
// widgets.Section. The body clips to the box's content width so lipgloss
// never word-wraps a card line; the section's Rect is recorded on the
// page with the grid's content-relative origin.
func (d *Dashboard) cardBox(title, body string, x, y, w, bodyH int) string {
	sec := widgets.NewSection(d.th, title)
	out, _ := sec.Render(body, x, y, w, bodyH+3)
	d.sections = append(d.sections, sectionRect(x, y, out))

	return out
}

// titleLine renders one section title (accent: the page's only highlight).
func titleLine(th *theme.Theme, title string) string {
	return th.Accent.Render(title)
}

// paneTitle accents a pane title when the pane holds focus and mutes it
// otherwise.
func paneTitle(th *theme.Theme, title string, focused bool) string {
	if focused {
		return titleLine(th, title)
	}

	return th.TextMuted.Render(title)
}

// clipCells truncates one styled line to w cells.
func clipCells(s string, w int, tail string) string {
	if lipgloss.Width(s) <= w {
		return s
	}

	return ansi.Truncate(s, w, tail)
}

// deem renders one muted body span, ASCII-fying the decorative glyphs
// (· → -, → ->, — -) so the ascii goldens stay 7-bit.
func (d *Dashboard) deem(s string) string {
	return d.th.Deemphasized.Render(plainDecor(d.th, s))
}

// muted renders one dimmer body span with the same decor folding.
func (d *Dashboard) muted(s string) string {
	return d.th.TextMuted.Render(plainDecor(d.th, s))
}

// value renders one card value (dash for the unknown ≠ zero).
func (d *Dashboard) value(v string) string {
	return d.th.TextPrimary.Render(dashIf(d.th, v))
}

// sep is the styled card separator: the theme-owned glyph, so the card lines
// degrade with the rest of the ASCII profile.
func (d *Dashboard) sep() string {
	return d.th.Deemphasized.Render(d.th.Separator())
}

// kv renders one "label value" span: muted label, plain value.
func (d *Dashboard) kv(label, value string) string {
	return d.deem(label+" ") + d.value(value)
}

// --- cards ---------------------------------------------------------------

// Cards carry no title hotkey badge: a badge would advertise a key whose
// action lives on another screen. Keys live in the footer legend and the
// quick-actions rows; card bodies keep only truthful next-action hints.

func (d *Dashboard) connCard() *dashCard {
	body := d.connBodyLines()

	c := d.newCard(cardConn, titleConnection,
		func(int, int) []string { return d.connBodyLines() }, len(body))
	// The card that answers "am I connected?" must never drop.
	c.never = true

	return c
}

func (d *Dashboard) serverCard() *dashCard {
	body := d.serverCardBodyLines()

	return d.newCard(cardServer, titleServer,
		func(int, int) []string { return d.serverCardBodyLines() }, len(body))
}

func (d *Dashboard) sendCard() *dashCard {
	body := d.sendCardBodyLines()

	return d.newCard(cardSend, titleLastSend,
		func(int, int) []string { return d.sendCardBodyLines() }, len(body))
}

func (d *Dashboard) stressCard() *dashCard {
	body := d.stressCardBodyLines()

	return d.newCard(cardStress, titleLastStress,
		func(int, int) []string { return d.stressCardBodyLines() }, len(body))
}

func (d *Dashboard) sessionCard() *dashCard {
	body := d.sessionBodyLines()

	return d.newCard(cardSession, titleSession,
		func(int, int) []string { return d.sessionBodyLines() }, len(body))
}

// logCard sizes the SERVER LOG card: the compacted tail (newest last);
// narrowCap caps the natural body in the narrow stack (0 = no cap).
func (d *Dashboard) logCard(narrowCap int) *dashCard {
	lines := d.logBodyLines()
	natural := len(lines)
	if narrowCap > 0 && natural > narrowCap {
		natural = narrowCap
	}

	c := d.newCard(cardLog, titleLog, func(_, kept int) []string {
		full := d.logBodyLines()
		if kept < len(full) {
			full = full[len(full)-kept:] // tail: newest at the bottom
		}

		return full
	}, natural)
	c.bottom = true // the newest line belongs at the bottom of the box, not the top

	return c
}

// actionsCard sizes the QUICK ACTIONS list to its item count (min 1 for
// the empty message); it is never dropped by the height fitter.
func (d *Dashboard) actionsCard() *dashCard {
	c := d.newCard(cardActions, titleActions, d.actionsBodyLines,
		max(d.actions.Len(), 1))
	c.never = true

	return c
}

// --- formatting helpers ---------------------------------------------------

// shortUptime renders a pointer uptime compactly; a nil or unknown value
// becomes "" so the caller substitutes the dash.
func shortUptime(d *time.Duration) string {
	if d == nil {
		return ""
	}

	return shortDur(*d)
}

// shortDur renders d as MM:SS (minutes may exceed 59) or HH:MM:SS past
// an hour (negatives clamp to 00:00).
func shortDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Hour {
		return FormatUptime(&d)
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60

	return strconv.Itoa(m/10) + strconv.Itoa(m%10) + ":" + strconv.Itoa(s/10) + strconv.Itoa(s%10)
}

// formatElapsed renders the frozen send duration compactly: sub-second
// as "1.9ms", up to a minute as "12.3s", longer as HH:MM:SS.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + "ms"
	}
	if d < time.Minute {
		return strconv.FormatFloat(float64(d)/float64(time.Second), 'f', 1, 64) + "s"
	}

	return FormatUptime(&d)
}
