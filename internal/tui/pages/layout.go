// layout.go renders the §A body — the proposal-05 §3 information grid:
// live signals get the big panes, static config gets compact cards. Wide
// (>= dashWideCols) runs two columns (left dashLeftColW / right flex):
// CONNECTION, LAST SEND and LAST STRESS stack left; MOCK SERVER, SERVER
// LOG, SESSION and QUICK ACTIONS stack right, the SERVER LOG absorbing
// the spare height. Medium (>= dashMediumCols) keeps the two columns
// with wrapped (clipped) card rows and no fixed 46. Narrow stacks the
// cards in priority order — CONNECTION, MOCK SERVER, SERVER LOG (capped
// at narrowLogRows body rows), LAST SEND, SESSION, QUICK ACTIONS, LAST
// STRESS — shrinking bodies bottom-up and dropping the lowest-priority
// cards first when the terminal is too short (CONNECTION and QUICK
// ACTIONS never drop: the page must always answer "am I connected?" and
// always expose its action list). The EVENT FEED pane is gone: the
// CONNECTION card, the timestamped status strip and the SERVER LOG card
// carry that truth. Sizing comes from frame.ContentSize; the frame owns
// the surrounding chrome.
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

// Section titles (proposal 05 §3 wireframe). The page renders titles +
// boxes only; the frame owns the surrounding chrome. MOCK SERVER
// (titleServer) and SERVER LOG (titleLog) reuse the §G page's constants.
const (
	titleConnection = "CONNECTION"
	titleLastSend   = "LAST SEND"
	titleLastStress = "LAST STRESS"
	titleSession    = "SESSION"
	titleActions    = "QUICK ACTIONS"

	// Card-body next-action key glyphs (rendered with the HotKey style
	// inside card bodies; the tile TITLES carry no badges, UAT round 5).
	hotkeyConnection = "c"
	hotkeyLastSend   = "s"
	hotkeyMockServer = "4"

	// Grid breakpoints (proposal 05 §3). The wide left column is sized
	// relative to the terminal (UAT round 5: fixed columns do not
	// adapt); see dashLeftCol.
	dashWideCols   = 130
	dashMediumCols = 100
	// dashColGap is the blank row/column between cards in the two-column
	// grid; the narrow stack uses zero gap so the seven minimum-size
	// cards still fit a 28-row content area.
	dashColGap = 1

	// narrowLogRows caps the SERVER LOG card body rows in the narrow
	// stack (wireframe: 4 rows); the two-column grid lets the card
	// absorb the spare right-column height instead.
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
// allocation. body renders at most kept lines (the actions card renders
// its list lazily at the allocated height; the log card renders its
// TAIL so shrinking drops the oldest lines, never the newest).
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

// dashLeftCol sizes the wide-grid left column relative to the terminal
// (UAT round 5: panes adopt to the terminal size), clamped so the
// compact cards neither starve below 40 cells nor stretch past
// readability at 64.
func dashLeftCol(w int) int {
	return min(max(w*35/100, 40), 64)
}

// cardBox renders the plain "TITLE" (no key badge, UAT round 5) over the
// body clipped into a bordered box of total size w×(bodyH+3), so joins
// stay aligned, through the one shared widgets.Section (the body clips
// to the box's CONTENT width w-4 so lipgloss never word-wraps a card
// line into an extra row). The section's Rect is recorded on the page
// with the grid's content-relative origin.
func (d *Dashboard) cardBox(title, body string, x, y, w, bodyH int) string {
	sec := widgets.NewSection(d.th, title)
	out, r := sec.Render(body, x, y, w, bodyH+3)
	d.sections = append(d.sections, r)

	return out
}

// titleLine renders one section title (accent: the page's only highlight).
func titleLine(th *theme.Theme, title string) string {
	return th.Accent.Render(title)
}

// paneTitle accents a pane title when the pane holds focus and mutes
// it otherwise (UAT round 5: every title accented at once made the
// active pane ambiguous; the focused pane also lights its border).
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

// Cards carry NO title hotkey badge (UAT round 5): the badges advertised
// keys whose action lived on another screen, duplicating the footer's
// legend and lying about what the key does to the tile. Keys live in the
// footer legend and the quick-actions rows; card bodies keep only
// truthful next-action hints.

func (d *Dashboard) connCard() *dashCard {
	body := d.connBodyLines()

	c := d.newCard(cardConn, titleConnection,
		func(int, int) []string { return d.connBodyLines() }, len(body))
	// The card that answers "am I connected?" outranks every other one, as the
	// never field always claimed -- but only QUICK ACTIONS actually set it, so the
	// two-column grid could drop the connection truth while the doc and the stack
	// test promised it could not.
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

// shortUptime renders a pointer uptime in the wireframe's short form
// ("05:40"); a nil or non-positive-unknown value becomes "" so the
// caller substitutes the dash. Hours appear only past one hour.
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
