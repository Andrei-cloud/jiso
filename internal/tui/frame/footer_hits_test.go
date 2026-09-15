// footer_hits_test.go pins the Task 8.4 frame-side contract: FooterOrigin
// is the footer strip's absolute-cell oracle (the chromeParts twin of
// ContentOrigin), and FooterHits replays the EXACT packing Render draws,
// publishing a rect only for entries that actually made it into the
// rendered footer row. The hit map is invisible: these rects are metadata,
// and the goldens pinning the footer bytes stay untouched.
package frame

import (
	"strings"
	"testing"
)

// footerHintProps renders one known hint so the footer row is findable in
// Render's output by its desc text.
func footerHintProps(width, height int) Props {
	return Props{
		Width: width, Height: height, Content: "body",
		Hints: []KeyHint{{Key: "q", Desc: "zzquit", Primary: true}},
	}
}

// TestFooterOriginMatchesRender pins the mouse hit-map's footer-origin
// oracle (Task 8.4) the way TestContentOriginMatchesRender pins the
// content one: for every width level and a sweep of heights, FooterOrigin
// must be the absolute row/col Render actually draws the footer strip at —
// x = side rule + space, y = height-2 while the footer pair survives
// chromeParts — and report !ok exactly when no footer row is drawn
// (chrome dropped the pair, or the too-small state).
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

			// Structural: the Render line carrying the footer hint text
			// must sit at exactly y — chrome shrink included.
			out := strings.Split(Render(footerHintProps(width, height)), "\n")
			first := -1
			for i, l := range out {
				if strings.Contains(l, "zzquit") {
					first = i
					break
				}
			}
			if ok != (first >= 0) {
				t.Errorf("%dx%d: footer drawn=%v, FooterOrigin ok=%v", width, height, first >= 0, ok)
			}
			if ok && first != y {
				t.Errorf("%dx%d: footer drawn at line %d, FooterOrigin y=%d", width, height, first, y)
			}
		}
	}
}

// TestFooterHitsPackVisibleOnly pins the rect pipeline the hit map
// consumes: every PACKED entry gets exactly the footer-relative cell
// range the packer drew it at (gap-aware), while entries dropped by the
// narrow level filter get NO rect at all — only visible hints are
// clickable.
func TestFooterHitsPackVisibleOnly(t *testing.T) {
	t.Parallel()

	hints := []KeyHint{
		{Key: "1", Desc: "dash"},
		{Key: "2", Desc: "tx"},
		{Key: "e", Desc: "export"},
		{Key: "q", Desc: "quit", Primary: true},
	}

	// Wide: every entry packs; rects are left-to-right, start at the
	// footer origin, and leave exactly the two-cell gap between entries.
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

	// Narrow: the level filter hides the non-primary entries behind the
	// "…+N" marker, so only the primary trio gets a rect.
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

// TestFooterHitsDispatchFallsBackToKey pins the dispatch resolution: a
// hint carrying an explicit Dispatch is reported with it (display text
// untouched), and one without falls back to its Key spelling — the same
// matching vocabulary the bindings use (theme/keys.go).
func TestFooterHitsDispatchFallsBackToKey(t *testing.T) {
	t.Parallel()

	hits := FooterHits(nil, []KeyHint{
		{Key: "PgDn", Desc: "more", Dispatch: "pgdown", Primary: true},
		{Key: "j/k", Desc: "move", Primary: true},
	}, 120, 32)
	if len(hits) != 2 {
		t.Fatalf("packed %d rects, want 2: %#v", len(hits), hits)
	}
	if hits[0].Dispatch != "pgdown" {
		t.Errorf("explicit Dispatch lost: %q", hits[0].Dispatch)
	}
	if hits[1].Dispatch != "j/k" {
		t.Errorf("fallback dispatch = %q, want the Key spelling", hits[1].Dispatch)
	}
}
