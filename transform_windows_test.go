package fairy

import (
	"math"
	"testing"
)

// A turned element is hit where it is drawn, not where its upright box lies.
func TestHitTestFollowsRotation(t *testing.T) {
	w := newListWindow(t, func() *El {
		return Box(Box().Size(100, 20).Left(50).Top(40).Key("bar").Rotate(math.Pi / 2).OnClick(func() {}))
	})
	w.build()
	w.placeAll()
	w.placeAll() // the turn springs from its first value, which is the target
	// The bar's center is (100, 50); turned a quarter, it spans y 0..100 at x 90..110.
	for _, c := range []struct {
		x, y float32
		hit  bool
	}{
		{100, 5, true},  // above the upright box, inside the turned one
		{100, 95, true}, // below it
		{60, 50, false}, // inside the upright box only
		{100, 50, true},
	} {
		e := w.hitTest(c.x, c.y)
		if got := e != nil && e.key == "bar"; got != c.hit {
			t.Errorf("hit at (%v, %v) = %v, want %v", c.x, c.y, got, c.hit)
		}
	}
}
