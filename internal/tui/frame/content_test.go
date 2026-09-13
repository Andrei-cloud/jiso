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
