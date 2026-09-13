// grid_test.go pins the §A grid: the two columns are banded, so a band's boxes
// share a top and a bottom line, and the fitter holds the grid inside the terminal
// at sizes where cards have to shrink or go away.
package pages

import (
	"reflect"
	"strings"
	"testing"

	"jiso/internal/tui/frame"
)

// TestGridSharesBaselines pins the banded grid. The two columns used to be fitted
// independently and joined top-aligned, which let each column's boxes drift: with
// CONNECTION two lines tall beside MOCK SERVER one line tall, LAST SEND's title sat
// a line below SERVER LOG's and no horizontal rule ran across the page.
//
// The assert compares the terminal lines each column draws its box rules on, which
// is what an operator actually sees, rather than the fitter's internal allocations.
func TestGridSharesBaselines(t *testing.T) {
	t.Parallel()

	for _, sz := range [][2]int{{120, 32}, {150, 44}, {110, 36}} {
		w, h := sz[0], sz[1]

		lines := bodyLines(t, dashDashboard(t, onlineState(), w, h))

		rightX := firstRuleAfterMargin(lines)
		if rightX == 0 {
			t.Fatalf("%dx%d: no second column drew a box rule, so this is not the two-column grid", w, h)
		}

		leftRows, rightRows := ruleRows(lines, rightX)

		// Three cards in the left column, four in the right: QUICK ACTIONS owns
		// the last band on its own, so its two rules have no left partner.
		if len(rightRows) != len(leftRows)+2 {
			t.Fatalf("%dx%d: right column drew %d rules, left %d; expected the right to own one band alone",
				w, h, len(rightRows), len(leftRows))
		}

		if rightRows = rightRows[:len(rightRows)-2]; !reflect.DeepEqual(leftRows, rightRows) {
			t.Errorf("%dx%d: the bands are ragged -- the left column's box rules are on lines %v, the right's on %v\n%s",
				w, h, leftRows, rightRows, strings.Join(lines, "\n"))
		}
	}
}

// TestGridDropsInPriorityOrder: at a height where cards cannot all fit, the ones
// that survive are the ones the fitter is told to keep, and they stay on the page
// in priority order. This is the row fitter's version of the column fitter's
// never/drop rules, and dropping in the wrong order would quietly cost an operator
// the connection truth to keep the last stress run on screen.
func TestGridDropsInPriorityOrder(t *testing.T) {
	t.Parallel()

	// 14 terminal lines is 10 lines of content (frame.ContentSize), which is room
	// for two bands: the connection band and QUICK ACTIONS. Everything between
	// them has to go first.
	body := strings.Join(bodyLines(t, dashDashboard(t, onlineState(), 120, 14)), "\n")

	for _, must := range []string{titleConnection, titleActions} {
		if !strings.Contains(body, must) {
			t.Errorf("a two-band terminal dropped %q; it is never-droppable\n%s", must, body)
		}
	}

	if strings.Contains(body, titleLog) {
		t.Errorf("a two-band terminal kept %q; the lower-priority cards go first\n%s", titleLog, body)
	}
}

// TestGridNeverCutsACardInHalf: at a height where even the never-droppable cards
// cannot all fit, the fitter gives one of them up rather than emitting a box whose
// bottom border the frame then chops off. A cut card is worse than a missing card:
// it reads as a card that rendered wrong, and QUICK ACTIONS is drawn last, so it
// was always the one that got cut.
func TestGridNeverCutsACardInHalf(t *testing.T) {
	t.Parallel()

	lines := bodyLines(t, dashDashboard(t, onlineState(), 120, 12))

	_, contentH := frame.ContentSize(120, 12)
	if len(lines) > contentH {
		t.Fatalf("the body is %d lines, past the %d-line content area", len(lines), contentH)
	}

	if !strings.Contains(strings.Join(lines, "\n"), titleConnection) {
		t.Errorf("the highest-priority card is missing at %d content lines:\n%s", contentH, strings.Join(lines, "\n"))
	}

	// A box that was cut off has a top rule with no matching bottom rule: the
	// rules come in pairs, so an odd count means one was chopped.
	rules := leftRules(lines)
	if n := len(rules) % 2; n != 0 {
		t.Errorf("%d box rules at the left margin is an odd count, so a box is missing its bottom rule\n%s",
			len(rules), strings.Join(lines, "\n"))
	}
}

// leftRules is the left column's box rules: the lines whose rule starts at cell 0.
func leftRules(lines []string) []int {
	left, _ := ruleRows(lines, 0)

	return left
}

// ruleRows returns the lines on which each column draws a horizontal box rule:
// the left column's rules start at cell 0, the right's at rightX.
func ruleRows(lines []string, rightX int) (left, right []int) {
	for i, l := range lines {
		if startsRule(l, 0) {
			left = append(left, i)
		}

		if startsRule(l, rightX) {
			right = append(right, i)
		}
	}

	return left, right
}

// firstRuleAfterMargin is where the right column begins: the first box rule that
// does not start at the left margin. Derived from the render rather than from the
// layout constants, so the test cannot pass by agreeing with a wrong constant.
func firstRuleAfterMargin(lines []string) int {
	for _, l := range lines {
		for x := 1; x+2 < len(l); x++ {
			if startsRule(l, x) {
				return x
			}
		}
	}

	return 0
}

// startsRule reports a horizontal rule ("+---") beginning at cell x.
func startsRule(line string, x int) bool {
	if x+2 >= len(line) {
		return false
	}

	return strings.HasPrefix(line[x:], "+-")
}
