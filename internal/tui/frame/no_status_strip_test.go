// no_status_strip_test.go pins the frame's D-10C shape: the bottom
// console strip is gone. No rendered line may carry the strip's
// "status" label, and ContentSize must report exactly the drawn
// content band (while the strip lived it claimed a row ContentSize
// ignored — a latent ±1 over-report).
package frame

import (
	"strings"
	"testing"
)

func TestNoBottomStatusStrip(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)

	// Heights at which chromeParts keeps all four chrome rows, so the
	// band runs top rule → content → mid rule with no degenerate cases.
	for _, height := range []int{24, 12, 5} {
		p := matrixProps(th, 120)
		p.Height = height
		out := strings.Split(Render(p), "\n")

		for _, l := range out {
			if strings.Contains(l, "status") {
				t.Fatalf("120x%d: rendered line carries a strip label: %q", height, l)
			}
		}

		mid := -1
		for i := 1; i < len(out); i++ {
			if strings.HasPrefix(out[i], "+") {
				mid = i
				break
			}
		}
		if mid < 0 {
			t.Fatalf("120x%d: no mid rule found:\n%s", height, strings.Join(out, "\n"))
		}
		// The content band is everything between the top rule and the
		// mid rule; ContentSize must report exactly those rows.
		if _, wantH := ContentSize(120, height); mid-1 != wantH {
			t.Errorf("120x%d: drawn content rows = %d, ContentSize reports %d", height, mid-1, wantH)
		}
	}
}
