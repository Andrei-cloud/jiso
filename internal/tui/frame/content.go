package frame

// chromeParts is the single shrink oracle shared by Render and
// ContentSize: which chrome parts survive a terminal of the given height.
// The full frame has 4 chrome rows (top rule, mid rule, footer row,
// bottom rule); under height pressure they drop in order — top rule
// first, then the footer pair, bottom rule last — before the content
// floor (MinContentHeight) is ever violated.
func chromeParts(height int) (top, footerPair, bottom bool) {
	chrome := 4
	for height-chrome < MinContentHeight {
		switch chrome {
		case 4:
			chrome = 3 // drop top rule
		case 3:
			chrome = 1 // drop mid rule + footer row
		default:
			chrome = 0 // drop bottom rule
		}
	}
	switch chrome {
	case 4:
		return true, true, true
	case 3:
		return false, true, true
	case 1:
		return false, false, true
	default:
		return false, false, false
	}
}

// ContentSize reports the content area the Render pipeline would give a
// page body for a terminal of the given size: Width-4 (side rules plus one
// space each), and the height left after the surviving chrome rows — the
// exact chromeParts decision, so pages and Render never disagree
// mid-shrink. Below MinWidth the frame replaces everything with the
// too-small state; the content area is reported as the floor (pages still
// get a size, it simply never shows).
func ContentSize(width, height int) (w, h int) {
	if width <= 0 {
		width = FallbackWidth
	}
	if height <= 0 {
		height = FallbackHeight
	}
	if width < MinWidth {
		return width, MinContentHeight
	}

	top, footerPair, bottom := chromeParts(height)
	chrome := 0
	for _, on := range []bool{top, footerPair, footerPair, bottom} {
		if on {
			chrome++
		}
	}

	return width - borderInset, max(height-chrome, MinContentHeight)
}
