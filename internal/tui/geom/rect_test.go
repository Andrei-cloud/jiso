package geom

import "testing"

func TestRectContains(t *testing.T) {
	r := Rect{X: 3, Y: 2, W: 10, H: 5} // covers x 3..12, y 2..6
	for _, c := range []struct{ x, y int }{{3, 2}, {12, 6}, {7, 4}} {
		if !r.Contains(c.x, c.y) {
			t.Fatalf("Contains(%d,%d) = false, want true", c.x, c.y)
		}
	}
	for _, c := range []struct{ x, y int }{{2, 2}, {3, 1}, {13, 6}, {12, 7}} {
		if r.Contains(c.x, c.y) {
			t.Fatalf("Contains(%d,%d) = true, want false", c.x, c.y)
		}
	}
}

func TestRectString(t *testing.T) {
	r := Rect{X: 3, Y: 2, W: 10, H: 5}
	if got, want := r.String(), "Rect{3,2 10x5}"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
