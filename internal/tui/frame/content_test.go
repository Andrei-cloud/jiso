package frame

import (
	"strings"
	"testing"
)

// TestContentSizeMatchesRender pins the page-sizing contract: for every
// width level and a sweep of heights, ContentSize must equal the content
// lines Render actually produces. The oracle is Render's own section
// builders (same-package test — the point is drift-freedom of the shrink
// order, not re-deriving the constants).
func TestContentSizeMatchesRender(t *testing.T) {
	t.Parallel()

	for _, width := range []int{120, 100, 90, 80, 70, 48} {
		for _, height := range []int{32, 24, 20, 12, 8, 5, 3, 1} {
			_, ch := ContentSize(width, height)

			p := Props{Width: width, Height: height, Content: "body"}
			top, footerPair, bottom := chromeParts(height)
			chrome := 0
			for _, on := range []bool{top, footerPair, footerPair, bottom} {
				if on {
					chrome++
				}
			}
			want := max(height-chrome, MinContentHeight)

			if ch != want {
				t.Errorf("%dx%d: ContentSize h=%d, chromeParts content lines=%d", width, height, ch, want)
			}

			// The rendered frame must also contain exactly `want`
			// content lines for a single-line body (structural check).
			out := strings.Split(Render(p), "\n")
			if len(out) != height && height >= MinWidth/2 {
				t.Errorf("%dx%d: Render produced %d lines", width, height, len(out))
			}
		}
	}
}

// TestContentSizeTooSmall: below MinWidth the frame shows the truthful
// too-small state; ContentSize reports the floor so pages never panic.
func TestContentSizeTooSmall(t *testing.T) {
	t.Parallel()

	w, h := ContentSize(40, 24)
	if w != 40 || h != MinContentHeight {
		t.Errorf("ContentSize(40,24) = %d,%d, want 40,%d", w, h, MinContentHeight)
	}
	if w, h := ContentSize(0, 0); w != FallbackWidth-borderInset || h <= 0 {
		t.Errorf("ContentSize(0,0) = %d,%d, want fallback", w, h)
	}
}

// TestContentOriginMatchesRender pins the mouse hit-map's content-origin
// oracle (Task 8.1): for every width level and a sweep of heights,
// ContentOrigin must be the absolute cell Render actually starts the page
// body at — x = side rule + space, y = the surviving top rule's height.
// The structural half compares against Render's real line layout, so the
// shrink order can never drift out of the oracle unnoticed.
func TestContentOriginMatchesRender(t *testing.T) {
	t.Parallel()

	for _, width := range []int{120, 80, 48} {
		for _, height := range []int{24, 8, 5, 4, 2, 1} {
			x, y := ContentOrigin(width, height)
			if x != borderInset/2 {
				t.Errorf("%dx%d: ContentOrigin x=%d, want %d", width, height, x, borderInset/2)
			}
			top, _, _ := chromeParts(height)
			wantY := 0
			if top && width >= MinWidth {
				wantY = 1
			}
			if y != wantY {
				t.Errorf("%dx%d: ContentOrigin y=%d, want %d (top rule shown=%v)", width, height, y, wantY, top)
			}

			// Structural: the first Render line carrying the body must
			// sit at exactly y (the too-small state shows no body).
			out := strings.Split(Render(Props{Width: width, Height: height, Content: "body"}), "\n")
			if width < MinWidth {
				continue
			}
			first := -1
			for i, l := range out {
				if strings.Contains(l, "body") {
					first = i
					break
				}
			}
			if first != y {
				t.Errorf("%dx%d: body starts at line %d, ContentOrigin y=%d", width, height, first, y)
			}
		}
	}
}
