package frame

// chromeParts is the single shrink oracle shared by Render and
// ContentSize: which chrome parts survive a terminal of the given height.
// The full frame has 4 chrome rows; under height pressure they drop — top
// rule first, then the footer pair, bottom rule last — before the content
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
// page body for a terminal of the given size: Width-4, and the height left
// after the surviving chrome rows — the exact chromeParts decision, so
// pages and Render never disagree mid-shrink. Below MinWidth the area is
// reported at the floor (the too-small state shows instead).
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
// content area starts — the exact chromeParts decision Render composes
// with, so the hit-map and the drawn frame never disagree mid-shrink. X
// is always borderInset/2; Y is 1 while the top rule survives, else 0.
// Below MinWidth the too-small state shows, but the origin is still
// reported normally (a size pages never show, like ContentSize's floor).
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

// FooterOrigin reports the absolute terminal cell where the footer strip's
// content starts — the chromeParts twin of ContentOrigin, so the footer
// hit-map and the drawn frame never disagree mid-shrink. X is
// borderInset/2; Y is height-2 whenever the strip is drawn (the console
// strip never moves it: it yields under height pressure rather than
// composing taller). ok=false when no footer row is drawn at all.
func FooterOrigin(width, height int) (x, y int, ok bool) {
	if width <= 0 {
		width = FallbackWidth
	}
	if height <= 0 {
		height = FallbackHeight
	}
	if width < MinWidth {
		return 0, 0, false
	}
	if _, footerPair, _ := chromeParts(height); !footerPair {
		return 0, 0, false
	}

	return borderInset / 2, height - 2, true
}
