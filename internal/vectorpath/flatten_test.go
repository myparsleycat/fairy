package vectorpath

import (
	"math"
	"testing"
)

// Flattened curves stay within the tolerance of the curve and end on it.
func TestFlattenWithinTolerance(t *testing.T) {
	const tol = 0.25
	p0, c, p1 := Point{0, 0}, Point{50, 100}, Point{100, 0}
	q := Quad(nil, p0, c, p1, tol)
	if q[len(q)-1] != p1 || len(q) < 2 {
		t.Fatalf("quad ends at %v after %d points", q[len(q)-1], len(q))
	}
	// The midpoint of the curve lies within tol of the polyline.
	mid := Point{50, 50}
	best := float32(math.Inf(1))
	prev := p0
	for _, p := range q {
		best = min(best, segDist(mid, prev, p))
		prev = p
	}
	if best > tol {
		t.Fatalf("quad midpoint is %v from the polyline, want at most %v", best, tol)
	}
	a := Arc(nil, 0, 0, 100, 0, math.Pi/2, tol)
	if first, last := a[0], a[len(a)-1]; first != (Point{0, -100}) || math.Abs(float64(last.X-100)) > 1e-3 || math.Abs(float64(last.Y)) > 1e-3 {
		t.Fatalf("arc runs from %v to %v, want 12 to 3 o'clock", first, last)
	}
	for i := 1; i < len(a); i++ {
		m := Point{(a[i-1].X + a[i].X) / 2, (a[i-1].Y + a[i].Y) / 2}
		if d := 100 - hypot(m.X, m.Y); d > tol {
			t.Fatalf("arc chord %d strays %v from the circle", i, d)
		}
	}
}

// An arc of many turns flattens to a bounded number of points.
func TestArcPointsBounded(t *testing.T) {
	if n := len(Arc(nil, 0, 0, 100, 0, 1e5, 0.2)); n > maxArcPoints+1 {
		t.Fatalf("arc of 1e5 radians has %d points", n)
	}
}

func segDist(p, a, b Point) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := dx*dx + dy*dy
	t := float32(0)
	if l > 0 {
		t = min(max(((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l, 0), 1)
	}
	return hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
}
