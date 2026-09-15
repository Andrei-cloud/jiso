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

// ContentOrigin reports the absolute terminal cell where the page body's
// content area starts for a terminal of the given size — the exact
// chromeParts decision Render composes with, so the mouse hit-map and the
// drawn frame never disagree mid-shrink. X is always borderInset/2: the
// side rule plus one space (wrapRow). Y is the top rule's height: 1 while
// it survives, 0 once chromeParts drops it. Below MinWidth Render shows
// the too-small state instead of a page body; the origin is still reported
// as the normal x with y=0 (pages get a size they never show, exactly like
// ContentSize's floor report).
func ContentOrigin(width, height int) (x, y int) {
	if width <= 0 {
		width = FallbackWidth
	}
	if height <= 0 {
		height = FallbackHeight
	}
	top, _, _ := chromeParts(height)

	return borderInset / 2, b2i(top && width >= MinWidth)
}
