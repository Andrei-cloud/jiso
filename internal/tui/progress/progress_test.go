package progress

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

var epoch = time.Unix(1_700_000_000, 0).UTC()

func trueTheme(t *testing.T) *theme.Theme {
	t.Helper()

	return theme.NewWith(colorprofile.TrueColor, true)
}

func asciiTheme(t *testing.T) *theme.Theme {
	t.Helper()

	return theme.NewWith(colorprofile.ASCII, true)
}

func TestBarPercentClamp(t *testing.T) {
	t.Parallel()

	b := NewBar(trueTheme(t), "tx", 100)
	b.SetProgress(150, 100)
	if p := b.Percent(); p != 100 {
		t.Errorf("150/100 = %d%%, want 100", p)
	}
	b.SetProgress(-5, 100)
	if p := b.Percent(); p != 0 {
		t.Errorf("-5/100 = %d%%, want 0", p)
	}
	b.SetProgress(-5, -1)
	if p := b.Percent(); p != -1 {
		t.Errorf("unknown percent = %d, want -1", p)
	}
}

func TestBarNoPanicWidths(t *testing.T) {
	t.Parallel()

	themes := map[string]*theme.Theme{"truecolor": trueTheme(t), "ascii": asciiTheme(t)}
	widths := []int{-3, 0, 1, 3, 7, 8, 9, 12, 20, 40, 80}
	states := [][2]int{{0, 10}, {5, 10}, {10, 10}, {99, 10}, {7, 0}, {7, -1}}

	for name, th := range themes {
		for _, st := range states {
			for _, w := range widths {
				b := NewBar(th, "send", st[1])
				b.now = func() time.Time { return epoch }
				b.SetProgress(st[0], st[1])

				out := b.View(w)
				if w >= BarMinWidth {
					if n := lipgloss.Width(out); n > w {
						t.Errorf("%s %d/%d width %d: rendered %d cells %q", name, st[0], st[1], w, n, out)
					}
				}
			}
		}
	}
}

func TestBarUnknownPulsesNeverFreezes(t *testing.T) {
	t.Parallel()

	th := trueTheme(t)
	b := NewBar(th, "stress", 10)
	b.now = func() time.Time { return epoch }
	b.SetProgress(10, 10)
	if !strings.Contains(b.View(40), FillFull) {
		t.Fatal("known-complete bar should show fill")
	}

	b.SetProgress(42, 0) // total becomes unknown mid-flight
	v := b.View(40)
	if strings.ContainsAny(v, FillFull+ASCIIFillFull) {
		t.Errorf("unknown total rendered a bar glyph (frozen full bar): %q", v)
	}
	if !strings.Contains(v, "42") {
		t.Errorf("pulse lost the count: %q", v)
	}
	if !strings.ContainsAny(v, DefaultSpinFrames) {
		t.Errorf("pulse lost the spinner frame: %q", v)
	}
}

func TestBarDegradesToPercentOnly(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	b := NewBar(th, "a-very-long-label-indeed", 10)
	b.now = func() time.Time { return epoch }
	b.SetProgress(5, 10)

	v := b.View(6) // "[#] 50%" needs 7 cells; 6 must drop the track
	if strings.Contains(v, "[") {
		t.Errorf("width 6 still shows a bracketed track: %q", v)
	}
	if !strings.Contains(v, "50%") {
		t.Errorf("narrow bar lost the percent: %q", v)
	}
}

func TestBarElapsedAndAsciiFill(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	b := NewBar(th, "tx", 10)
	b.Started = epoch.Add(-3 * time.Second)
	b.now = func() time.Time { return epoch }
	b.SetProgress(5, 10)

	v := b.View(40)
	if !strings.Contains(v, "3s") {
		t.Errorf("elapsed label missing: %q", v)
	}
	if !strings.Contains(v, ASCIIFillFull) || strings.Contains(v, FillFull) {
		t.Errorf("ascii bar must use # fill, not █: %q", v)
	}
}
