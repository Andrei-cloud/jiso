package geom

import "fmt"

// Rect is a screen rectangle: origin (X,Y) at the upper-left, W and H in
// cells, zero-based terminal coordinates matching tea.MouseMsg.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell (x,y) falls inside the rectangle:
// inclusive of the origin edges, exclusive of the far edges.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) String() string {
	return fmt.Sprintf("Rect{%d,%d %dx%d}", r.X, r.Y, r.W, r.H)
}
