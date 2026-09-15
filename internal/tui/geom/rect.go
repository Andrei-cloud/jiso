package geom

import "fmt"

// Rect is a screen rectangle in inclusive terminal coordinates: origin
// (X,Y) at the upper-left, width W and height H in cells. Coordinates are
// zero-based from the terminal's upper-left corner, matching tea.MouseMsg.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell (x,y) falls inside the rectangle
// (inclusive of both edges).
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) String() string {
	return fmt.Sprintf("Rect{%d,%d %dx%d}", r.X, r.Y, r.W, r.H)
}
