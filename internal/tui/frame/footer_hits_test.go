// footer_hits_test.go pins the frame footer oracle: FooterOrigin is the
// footer strip's absolute cell position, and FooterHits publishes a rect
// only for entries actually packed into the rendered footer row.
package frame

import (
	"strings"
	"testing"
)

// footerHintProps renders one known hint so the footer row is findable.
func footerHintProps(width, height int) Props {
	return Props{
		Width: width, Height: height, Content: "body",
		Hints: []KeyHint{{Key: "q", Desc: "zzquit", Primary: true}},
	}
}

// FooterOrigin must be the absolute row/col Render actually draws the footer
// strip at, and !ok exactly when no footer row is drawn — swept without and
// WITH the console strip, which yields at the chrome floor.
func TestFooterOriginMatchesRender(t *testing.T) {
	t.Parallel()

	for _, width := range []int{120, 80, 79, 48} {
		for _, height := range []int{32, 24, 8, 5, 4, 3, 2, 1} {
			x, y, ok := FooterOrigin(width, height)
			_, footerPair, _ := chromeParts(height)
			wantOK := footerPair && width >= MinWidth
			if ok != wantOK {
				t.Errorf("%dx%d: FooterOrigin ok=%v, want %v (footer pair survives=%v)",
					width, height, ok, wantOK, footerPair)
			}
			if ok && x != borderInset/2 {
				t.Errorf("%dx%d: FooterOrigin x=%d, want %d", width, height, x, borderInset/2)
			}

			for _, console := range []string{"", "clogline"} {
				p := footerHintProps(width, height)
				p.Console = console
				out := strings.Split(Render(p), "\n")

				// Render never composes taller than the window.
				if width >= MinWidth && len(out) > height {
					t.Errorf("%dx%d console=%q: Render composed %d lines, over the %d-row window",
						width, height, console, len(out), height)
				}

				// The line carrying the footer hint sits at exactly y.
				first := -1
				for i, l := range out {
					if strings.Contains(l, "zzquit") {
						first = i
						break
					}
				}
				if ok != (first >= 0) {
					t.Errorf("%dx%d console=%q: footer drawn=%v, FooterOrigin ok=%v",
						width, height, console, first >= 0, ok)
				}
				if ok && first != y {
					t.Errorf("%dx%d console=%q: footer drawn at line %d, FooterOrigin y=%d",
						width, height, console, first, y)
				}
			}
		}
	}
}

// Packed entries get exactly the footer-relative cells the packer drew;
// entries dropped by the narrow filter get no rect at all.
func TestFooterHitsPackVisibleOnly(t *testing.T) {
	t.Parallel()

	hints := []KeyHint{
		{Key: "1", Desc: "dash"},
		{Key: "2", Desc: "tx"},
		{Key: "e", Desc: "export"},
		{Key: "q", Desc: "quit", Primary: true},
	}

	// Wide: every entry packs; rects run left-to-right from the origin.
	hits := FooterHits(nil, hints, 120, 32)
	if len(hits) != len(hints) {
		t.Fatalf("wide footer packed %d rects, want %d: %#v", len(hits), len(hints), hits)
	}
	ox, oy, _ := FooterOrigin(120, 32)
	if hits[0].X != ox || hits[0].Y != oy {
		t.Errorf("first entry at %d,%d, want the footer origin %d,%d", hits[0].X, hits[0].Y, ox, oy)
	}
	for i, h := range hits {
		if h.W <= 0 {
			t.Errorf("entry %d has a zero-width rect: %#v", i, h)
		}
		if i > 0 && h.X <= hits[i-1].X+hits[i-1].W {
			t.Errorf("entry %d at x=%d overlaps entry %d ending at %d", i, h.X, i-1, hits[i-1].X+hits[i-1].W)
		}
		if h.Dispatch != hints[i].Key {
			t.Errorf("entry %d dispatch = %q, want %q", i, h.Dispatch, hints[i].Key)
		}
	}

	// Narrow: the level filter drops the non-primary entries; no rects for them.
	primaries := 0
	for _, h := range hints {
		if h.Primary {
			primaries++
		}
	}
	narrow := FooterHits(nil, hints, 79, 32)
	if len(narrow) != primaries {
		t.Fatalf("narrow footer packed %d rects, want %d (dropped hints must get none): %#v",
			len(narrow), primaries, narrow)
	}
	for _, h := range narrow {
		if h.Dispatch != "q" {
			t.Errorf("dropped entry %q still got a rect: %#v", h.Dispatch, h)
		}
	}

	// No footer is drawn at all: no rects, no phantom hits.
	if got := FooterHits(nil, hints, MinWidth-1, 32); got != nil {
		t.Errorf("too-small frame published %d rects, want none", len(got))
	}
	if got := FooterHits(nil, hints, 120, 3); got != nil {
		t.Errorf("chrome-shrunk frame (h=3) published %d rects, want none", len(got))
	}
	// Fallback sizes behave like Render's zero-size fallbacks.
	if got := FooterHits(nil, hints, 0, 0); len(got) == 0 {
		t.Error("the fallback frame must still publish its packed entries")
	}
}

// Every rect carries the hint's Key spelling verbatim, even unspellable
// ones like "j/k" — the caller's synthKeyPress guard filters those.
func TestFooterHitsDispatchIsKeySpelling(t *testing.T) {
	t.Parallel()

	hits := FooterHits(nil, []KeyHint{
		{Key: "enter", Desc: "run", Primary: true},
		{Key: "j/k", Desc: "move", Primary: true},
	}, 120, 32)
	if len(hits) != 2 {
		t.Fatalf("packed %d rects, want 2: %#v", len(hits), hits)
	}
	if hits[0].Dispatch != "enter" {
		t.Errorf("dispatch = %q, want the Key spelling", hits[0].Dispatch)
	}
	if hits[1].Dispatch != "j/k" {
		t.Errorf("dispatch = %q, want the unspellable Key reported verbatim (caller filters)", hits[1].Dispatch)
	}
}
