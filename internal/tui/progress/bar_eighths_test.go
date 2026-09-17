// bar_eighths_test.go pins the opt-in sub-cell fill (§H): with
// Eighths set on a non-ascii theme the boundary cell becomes the partial
// block for the remainder eighths; ascii themes and Eighths=false stay
// on the whole-cell fill. Also pins that Note reaches the known-total
// track form (the §H counts/ETA suffix).
package progress

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plain strips SGR runs so contiguous glyphs can be matched.
func plain(s string) string { return ansi.Strip(s) }

func TestBarEighthsSubCellFill(t *testing.T) {
	t.Parallel()

	th := trueTheme(t)
	b := NewBar(th, "w", 100)
	b.Eighths = true
	b.SetProgress(41, 100)

	// width 28, label 1, pct "41%": track = 28 - (3+3) - (1+1) = 20.
	// units = 20*41*8/100 = 65 -> 8 full + remainder 1/8 ("▏") + 11 empty.
	got := plain(b.View(28))
	if !strings.Contains(got, "████████▏") {
		t.Fatalf("41%% of 20 cells must end 8 full + ▏, got %q", got)
	}
	if strings.Contains(got, "█████████") {
		t.Fatalf("sub-cell fill must not round 41%% up to 9 full cells: %q", got)
	}
}

func TestBarEighthsBounds(t *testing.T) {
	t.Parallel()

	th := trueTheme(t)

	zero := NewBar(th, "", 100)
	zero.Eighths = true
	zero.SetProgress(0, 100)
	if s := plain(zero.View(24)); strings.ContainsAny(s, "█▏▎▍▌▋▊▉") {
		t.Fatalf("0%% must render an empty track, got %q", s)
	}

	full := NewBar(th, "", 100)
	full.Eighths = true
	full.SetProgress(100, 100)
	if s := plain(full.View(24)); strings.ContainsAny(s, "▏▎▍▌▋▊▉░") {
		t.Fatalf("100%% must render solid, no partials/empties, got %q", s)
	}
}

func TestBarEighthsRespectsAsciiTheme(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	b := NewBar(th, "w", 100)
	b.Eighths = true
	b.SetProgress(41, 100)

	got := plain(b.View(28))
	if strings.ContainsAny(got, "█░▏▎▍▌▋▊▉") {
		t.Fatalf("ascii theme must keep #/. fill even with Eighths, got %q", got)
	}
	if !strings.Contains(got, "#") || !strings.Contains(got, ".") {
		t.Fatalf("ascii fill degraded unexpectedly: %q", got)
	}
}

func TestBarEighthsOffUsesWholeCells(t *testing.T) {
	t.Parallel()

	th := trueTheme(t)
	b := NewBar(th, "w", 100)
	b.SetProgress(41, 100)

	got := plain(b.View(28))
	if strings.ContainsAny(got, "▏▎▍▌▋▊▉") {
		t.Fatalf("Eighths=false must not emit partial blocks: %q", got)
	}
}

func TestBarNoteRendersInTrackForm(t *testing.T) {
	t.Parallel()

	th := trueTheme(t)
	b := NewBar(th, "w-2", 19200)
	b.SetProgress(41, 100)
	b.Note = "7,940/19,200 sent | ETA 01:12"

	got := plain(b.View(80))
	for _, want := range []string{"w-2", "41%", "7,940/19,200 sent", "ETA 01:12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("track form lacks %q: %q", want, got)
		}
	}
}
