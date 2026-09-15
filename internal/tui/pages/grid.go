// grid.go is the §A grid: how the seven cards are banded into rows and how the
// rows are fitted to the terminal height. layout.go owns the box a card is drawn
// in and the clip/style primitives it uses; nothing here knows what a card says.
//
// The two-column grid is banded into rows on purpose. It used to fit the left and
// right columns independently and join them top-aligned, which meant that from the
// second band down nothing lined up: CONNECTION's bottom border sat a line above
// MOCK SERVER's, LAST SEND's a line below SERVER LOG's, and the page had no
// horizontal line running across it. A band gives both of its cards the same
// height, so their borders land on the same lines.
package pages

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// dashPriority is the grid's card order, most important first. renderStacked
// renders in this order, and the fitter drops in the reverse order, so the narrow
// stack and the two-column grid agree on what an operator loses first when the
// terminal is too short. (CONNECTION and QUICK ACTIONS set never and survive.)
var dashPriority = []string{cardConn, cardServer, cardLog, cardSend, cardSession, cardActions, cardStress}

// dashRow is one band of the two-column grid: the card in the left column and the
// card that shares its baseline. right is always set; left is nil only for the
// final band, which QUICK ACTIONS has to itself.
type dashRow struct {
	left  *dashCard
	right *dashCard
}

// members returns the band's cards, left first. Either side can be nil: the last
// band has no left card, and dropping a card leaves its band with one. The fitter
// must not deref the gap, which is what a plain two-element literal did.
func (r dashRow) members() []*dashCard {
	cards := make([]*dashCard, 0, 2)
	for _, c := range []*dashCard{r.left, r.right} {
		if c != nil {
			cards = append(cards, c)
		}
	}

	return cards
}

// height is what the band costs: the tallest member's body. The shorter member is
// padded inside its own box rather than shortening the band.
func (r dashRow) height() int {
	h := 1
	for _, c := range r.members() {
		h = max(h, max(c.kept, 1))
	}

	return h
}

// rowHeight is the total height of the banded grid, boxes and gaps included.
func rowHeight(rows []dashRow, gap int) int {
	total := 0
	for _, r := range rows {
		total += r.height() + cardOverhead
	}
	if n := len(rows) - 1; n > 0 {
		total += n * gap
	}

	return total
}

// fitRows shrinks bands bottom-up to their minimum, drops the lowest-priority
// cards while the grid still overflows, then grows what is left. It always
// terminates: shrink and grow move one body line per step against a bounded
// natural height, and a drop strictly removes a card.
func fitRows(rows []dashRow, h, gap int) []dashRow {
	for rowHeight(rows, gap) > h {
		if shrinkRow(rows) {
			continue
		}

		next, dropped := dropCard(rows)
		if !dropped {
			break // only the never-drop cards remain
		}

		rows = next
	}

	for rowHeight(rows, gap) < h {
		if !growRow(rows) {
			break
		}
	}

	return rows
}

// shrinkRow takes one body line off the lowest-priority band that can still pay
// for it, and within a band off the lower-priority card that sets the band's
// height. Taking a line from a card that is already shorter than the band would
// not shrink the grid, so the caller's loop would never converge.
func shrinkRow(rows []dashRow) bool {
	for i := len(rows) - 1; i >= 0; i-- {
		h := rows[i].height()
		for _, id := range reversedPriority(dashPriority) {
			for _, c := range rows[i].members() {
				if c.id == id && c.kept == h && c.kept > c.min {
					c.kept--

					return true
				}
			}
		}
	}

	return false
}

// growRow gives one body line to the card furthest below its natural height,
// scanning bands top-down. That is what makes the SERVER LOG absorb spare height
// (its natural is the whole compacted tail), and a band grows once the card that
// needed the catch-up passes its neighbour.
func growRow(rows []dashRow) bool {
	for _, r := range rows {
		var best *dashCard

		for _, c := range r.members() {
			if c.kept >= c.natural {
				continue
			}

			if best == nil || c.natural-c.kept > best.natural-best.kept {
				best = c
			}
		}

		if best != nil {
			best.kept++

			return true
		}
	}

	return false
}

// dropCard removes the lowest-priority card that may be dropped. The band keeps its
// remaining card and disappears when both of its cards are gone; the caller must
// adopt the result, since removing a band is not visible through the shared slice
// header.
//
// A never-droppable card goes only as a last resort, once the grid still does not
// fit with nothing else left. That is the fitter choosing to lose a card whole
// rather than letting clampHeight slice its bottom border off, which is what used
// to happen below two bands of content height: QUICK ACTIONS is drawn last, so the
// card the fitter was told to keep was precisely the one the terminal chopped in
// half. CONNECTION outranks it, so the connection truth is what survives.
func dropCard(rows []dashRow) ([]dashRow, bool) {
	for _, lastResort := range []bool{false, true} {
		for _, id := range reversedPriority(dashPriority) {
			for i, r := range rows {
				for _, c := range r.members() {
					if c.id != id || (c.never && !lastResort) {
						continue
					}

					rows[i].remove(c)
					if len(rows[i].members()) == 0 {
						return append(rows[:i], rows[i+1:]...), true
					}

					return rows, true
				}
			}
		}
	}

	return rows, false
}

// remove takes one card out of the band.
func (r *dashRow) remove(c *dashCard) {
	if r.left == c {
		r.left = nil

		return
	}

	r.right = nil
}

// reversedPriority is the order the fitter drops and shrinks in: least important
// card first.
func reversedPriority(order []string) []string {
	out := make([]string, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, order[i])
	}

	return out
}

// renderTwoCol lays out the banded grid: three cards in the left column, four in
// the right, so the last band is QUICK ACTIONS on its own.
func (d *Dashboard) renderTwoCol(w, h, leftW int) string {
	rightW := max(w-leftW-dashColGap, 20)

	rows := fitRows([]dashRow{
		{left: d.connCard(), right: d.serverCard()},
		{left: d.sendCard(), right: d.logCard(narrowOff)},
		{left: d.stressCard(), right: d.sessionCard()},
		{right: d.actionsCard()},
	}, h, dashColGap)

	return clampHeight(d.renderRows(rows, leftW, rightW, dashColGap), h)
}

// renderStacked is the narrow single column in priority order. A single column
// has no baselines to share, so it fits card by card.
func (d *Dashboard) renderStacked(w, h int) string {
	cards := fitCards([]*dashCard{
		d.connCard(), d.serverCard(), d.logCard(narrowLogRows),
		d.sendCard(), d.sessionCard(), d.actionsCard(), d.stressCard(),
	}, h, 0)

	return clampHeight(d.renderColumn(cards, w, 0), h)
}

// narrowOff disables the narrow log body cap for the two-column grid
// (there the card shrinks via the fitter instead of being pre-cut).
const narrowOff = 0

// renderRows stacks the bands, each band's two cards side by side at the band
// height, so the boxes of one band share a top and a bottom border. The band
// scan also knows each card's content-relative origin, which is exactly what
// cardBox records into the page's section rects.
func (d *Dashboard) renderRows(rows []dashRow, leftW, rightW, gap int) string {
	bands := make([]string, 0, len(rows))

	y := 0
	for _, r := range rows {
		h := r.height()

		left := blankCell(leftW, h+cardOverhead)
		if r.left != nil {
			left = d.cardCell(r.left, 0, y, leftW, h)
		}

		// The right column starts where the drawn left segment ends plus
		// the gap column (measured, not nominal: the hit-map origin must
		// land on ink), and the next band where this band's drawn lines
		// end (bands join on a single "\n" with no gap rows).
		rightX := lipgloss.Width(left) + gap
		right := blankCell(rightW, h+cardOverhead)
		if r.right != nil {
			right = d.cardCell(r.right, rightX, y, rightW, h)
		}

		band := lipgloss.JoinHorizontal(lipgloss.Top,
			left, strings.Repeat(" ", gap), right)
		bands = append(bands, band)

		y += lipgloss.Height(band)
	}

	return strings.Join(bands, "\n")
}

// cardCell draws one card at the band's height. The body is asked for the band's
// lines, which is how the log takes the band's full height (newest last) while a
// card with less to say is padded by cardBox to the same height.
func (d *Dashboard) cardCell(c *dashCard, x, y, w, bandH int) string {
	lines := c.body(w, bandH)
	if c.bottom && len(lines) < bandH {
		lines = append(blankLines(bandH-len(lines)), lines...)
	}

	return d.cardBox(c.title, strings.Join(lines, "\n"), x, y, w, bandH)
}

// blankLines is n empty body lines, for a card that aligns to the bottom of its
// box (the log) rather than the top.
func blankLines(n int) []string {
	return make([]string, max(n, 0))
}

// blankCell is the empty half of a band: real space for its width, so
// JoinHorizontal cannot slide the card that is there into the left column.
func blankCell(w, lines int) string {
	blank := strings.Repeat(" ", max(w, 1))
	out := make([]string, max(lines, 1))
	for i := range out {
		out[i] = blank
	}

	return strings.Join(out, "\n")
}

// renderColumn renders the fitted cards stacked at width w with gap
// blank rows between them, recording each card's stacked origin.
func (d *Dashboard) renderColumn(cards []*dashCard, w, gap int) string {
	parts := make([]string, 0, len(cards))

	y := 0
	for _, c := range cards {
		body := strings.Join(c.body(w, c.kept), "\n")
		card := d.cardBox(c.title, body, 0, y, w, c.kept)
		parts = append(parts, card)
		// The column joins cards with gap+1 newlines: the terminator
		// costs no line of its own, only the gap blank rows do (and the
		// narrow stack passes gap 0 — the phantom +1 there was exactly
		// the drift this measurement removes).
		y += lipgloss.Height(card) + gap
	}

	return strings.Join(parts, strings.Repeat("\n", gap+1))
}

// fitCards shrinks card bodies bottom-up (lowest priority first) to their min,
// drops the lowest-priority droppable cards while the column still overflows, and
// finally grows bodies top-down (priority order, so the SERVER LOG absorbs the
// spare height) while room remains. It always terminates: shrink/grow move one
// line per step and drop strictly removes cards.
func fitCards(cards []*dashCard, h, gap int) []*dashCard {
	for colHeight(cards, gap) > h {
		if shrinkOne(cards) {
			continue
		}

		next, dropped := dropOne(cards)
		if !dropped {
			break // only the never-drop cards remain
		}

		cards = next
	}
	for colHeight(cards, gap) < h {
		if !growOne(cards) {
			break
		}
	}

	return cards
}

func colHeight(cards []*dashCard, gap int) int {
	total := 0
	for _, c := range cards {
		total += max(c.kept, 1) + cardOverhead
	}
	if n := len(cards) - 1; n > 0 {
		total += n * gap
	}

	return total
}

// shrinkOne drops one body line from the lowest-priority card that is
// still above its minimum.
func shrinkOne(cards []*dashCard) bool {
	for i := len(cards) - 1; i >= 0; i-- {
		if cards[i].kept > cards[i].min {
			cards[i].kept--

			return true
		}
	}

	return false
}

// dropOne removes the lowest-priority card that may be dropped and returns the
// shortened column (the caller must adopt the result: the truncation is not
// visible through the shared slice header).
//
// It drops by dashPriority rather than by position; the narrow stack is built in
// that order, so this is the same bottom-up drop it always did, with one addition
// it did not have: a never-droppable card goes as a last resort rather than being
// left for clampHeight to cut in half.
func dropOne(cards []*dashCard) ([]*dashCard, bool) {
	for _, lastResort := range []bool{false, true} {
		for _, id := range reversedPriority(dashPriority) {
			for i, c := range cards {
				if c.id != id || (c.never && !lastResort) {
					continue
				}

				return append(cards[:i], cards[i+1:]...), true
			}
		}
	}

	return cards, false
}

// growOne adds one body line to the highest-priority card still below
// its natural height (priority order: earlier cards refill first, and
// the log card's natural body is the whole compacted tail).
func growOne(cards []*dashCard) bool {
	for _, c := range cards {
		if c.kept < c.natural {
			c.kept++

			return true
		}
	}

	return false
}

// clampHeight cuts a body that (only at degenerate heights) still
// exceeds the content area; width is already clipped per card.
func clampHeight(body string, h int) string {
	lines := strings.Split(body, "\n")
	if len(lines) <= h {
		return body
	}

	return strings.Join(lines[:h], "\n")
}
