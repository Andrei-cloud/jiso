// transactions_width_test.go pins the §B table's relative widths: the
// five columns share the content width as minima plus a weighted split
// of the surplus, the table fills 100% of the content area at every
// size that pays the minima, and below that every column keeps its
// minimum and rows clip at the content edge, never wrap.
package pages

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
)

// The relative widths are exact integer math: surplus = content width −
// the minima (42) − the grid chrome (18); each column gets surplus ×
// weight / 12 (NAME 3, DESCRIPTION 5, DATASET 2, SPEC 2, MTI fixed),
// and the rounding remainder lands on DESCRIPTION, the heaviest column.
func TestTxWidthsAreRelative(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		contentW int
		want     []int
	}{
		{196, []int{44, 4, 70, 30, 30}}, // 200-col terminal
		{116, []int{24, 4, 36, 17, 17}}, // 120-col terminal
		{76, []int{14, 4, 20, 10, 10}},  // 80-col terminal
		{66, []int{11, 4, 15, 9, 9}},
		{60, []int{10, 4, 12, 8, 8}}, // surplus exhausted: every minima
	} {
		if got := txWidths(c.contentW); !reflect.DeepEqual(got, c.want) {
			t.Errorf("txWidths(%d) = %v, want %v", c.contentW, got, c.want)
		}
	}
}

// At wide, full and medium terminals every composed table line is
// exactly the content width — no trailing gap, no clipping past it.
func TestTxTableLinesFillContentWidth(t *testing.T) {
	t.Parallel()

	for _, s := range fillSizes {
		p := txPage(t, populatedState(), s.w, s.h)
		contentW, _ := frame.ContentSize(s.w, s.h)
		ls := strings.Split(txBody(t, p), "\n")
		for i, l := range ls {
			if i == 0 {
				continue // the title row is header chrome, not the table
			}
			if w := lipgloss.Width(l); w != contentW {
				t.Errorf("%s: line %d is %d cells, want exactly the %d-cell content width: %q",
					s.name, i+1, w, contentW, l)
			}
		}
	}
}

// Below what the content area pays for five minimum-readable columns
// (42 cells + 18 chrome), every column keeps its minimum: the table
// draws at its natural width and the page clips the rows at the
// content edge — no column is ever shaved past its minima, nothing
// wraps, and the last column is what drops off the line.
func TestTxFloorKeepsMinima(t *testing.T) {
	t.Parallel()

	if got := txWidths(50); !reflect.DeepEqual(got, []int{10, 4, 12, 8, 8}) {
		t.Errorf("txWidths(50) = %v, want every minima", got)
	}

	p := txPage(t, populatedState(), 54, 32) // 50-cell content area
	ls := strings.Split(txBody(t, p), "\n")
	if len(ls) != 1+8 {
		t.Fatalf("%d body lines, want 9 (title + table, rows never wrap):\n%s", len(ls), txBody(t, p))
	}
	for i, l := range ls {
		if w := lipgloss.Width(l); w > 50 {
			t.Errorf("line %d is %d cells, past the 50-cell content area: %q", i+1, w, l)
		}
	}
	if w := lipgloss.Width(ls[1]); w != 50 {
		t.Errorf("table top rule is %d cells, want the natural 60 clipped to exactly 50", w)
	}
	// First data row (name asc: Echo MC): the minima columns survive the
	// clip; the SPEC column past the edge does not.
	if !strings.Contains(ls[4], "0800") || strings.Contains(ls[4], "mastercard") {
		t.Errorf("floor row lost a minima column or kept a clipped one: %q", ls[4])
	}
}

// Cells truncate at their computed column width (theme ellipsis) and a
// row stays one line exactly the content width wide: the table never
// wraps inside its rows.
func TestTxCellsTruncateAtComputedWidth(t *testing.T) {
	t.Parallel()

	p := txPage(t, TransactionsState{FileName: "pool.json", TxCount: 1, Rows: []TxRow{
		{
			ID: "long", Name: "Purchase", MTI: "0200", Dataset: "pool (3)", Spec: "flex.json",
			Description: "Purchase authorization request for the shared card gateway",
		},
	}}, 120, 32)
	ls := strings.Split(txBody(t, p), "\n")
	if len(ls) != 1+5 {
		t.Fatalf("%d body lines, want 6 (title + rules + one row; a truncated cell must not add a line):\n%s", len(ls), txBody(t, p))
	}
	if !strings.Contains(ls[4], "Purchase authorization request for") ||
		strings.Contains(ls[4], "the shared card gateway") {
		t.Errorf("DESCRIPTION must truncate at its computed 36-cell width: %q", ls[4])
	}
	for i, l := range ls[1:] {
		if w := lipgloss.Width(l); w != 116 {
			t.Errorf("line %d is %d cells, want exactly the 116-cell content width: %q", i+2, w, l)
		}
	}
}
