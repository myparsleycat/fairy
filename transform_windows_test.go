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

// ScaleLive moves hit testing with its value without building the view.
func TestScaleLiveHitTest(t *testing.T) {
	scale := NewLive[float32](1)
	builds := 0
	w := newListWindow(t, func() *El {
		builds++
		return Box(Box().Size(20, 20).Left(40).Top(40).Key("box").ScaleLive(scale).OnClick(func() {}))
	})
	w.build()
	w.placeAll()
	hit := func(x, y float32) bool {
		e := w.hitTest(x, y)
		return e != nil && e.key == "box"
	}
	if !hit(50, 50) || hit(35, 35) {
		t.Fatal("unscaled box is not hit where it is laid out")
	}
	scale.Set(2)
	if w.dirty {
		t.Fatal("Set marked the view for building")
	}
	w.placeAll()
	if !hit(35, 35) || builds != 1 {
		t.Fatalf("after Set(2): hit at the scaled corner %v with %d builds", hit(35, 35), builds)
	}
}
