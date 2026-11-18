package vectorpath

import "math"

// Quad appends the points of the quadratic Bézier curve from p0 through
// control point c to p1, after p0, with no point of the curve farther than
// tol from the lines between them.
func Quad(pts []Point, p0, c, p1 Point, tol float32) []Point {
	// The curve strays from its chord by at most a quarter of the second
	// difference, and n segments divide that by n².
	dev := 0.25 * hypot(p0.X-2*c.X+p1.X, p0.Y-2*c.Y+p1.Y)
	n := segments(dev, tol)
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		pts = append(pts, Point{
			u*u*p0.X + 2*u*t*c.X + t*t*p1.X,
			u*u*p0.Y + 2*u*t*c.Y + t*t*p1.Y,
		})
	}
	return pts
}

// Cubic appends the points of the cubic Bézier curve from p0 through control
// points c0 and c1 to p1, after p0, within tol of the curve.
func Cubic(pts []Point, p0, c0, c1, p1 Point, tol float32) []Point {
	dev := 0.75 * max(
		hypot(p0.X-2*c0.X+c1.X, p0.Y-2*c0.Y+c1.Y),
		hypot(c0.X-2*c1.X+p1.X, c0.Y-2*c1.Y+p1.Y))
	n := segments(dev, tol)
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		pts = append(pts, Point{
			a*p0.X + b*c0.X + c*c1.X + d*p1.X,
			a*p0.Y + b*c0.Y + c*c1.Y + d*p1.Y,
		})
	}
	return pts
}

// Arc appends the points of a circular arc of radius r about (cx, cy), from
// angle start through sweep, both in radians clockwise from 12 o'clock with
// y pointing down, within tol of the circle. The first point is the arc's
// start. A sweep beyond a full turn is drawn as one turn.
func Arc(pts []Point, cx, cy, r, start, sweep, tol float32) []Point {
	sweep = min(max(sweep, -2*math.Pi), 2*math.Pi)
	n := 1
	if r > tol {
		step := 2 * math.Acos(float64(1-tol/r))
		n = min(max(int(math.Ceil(math.Abs(float64(sweep))/step)), 1), maxArcPoints)
	}
	for i := 0; i <= n; i++ {
		a := float64(start + sweep*float32(i)/float32(n))
		pts = append(pts, Point{cx + r*float32(math.Sin(a)), cy - r*float32(math.Cos(a))})
	}
	return pts
}

// maxArcPoints bounds the segments of an arc; a full circle of radius 10⁶
// pixels needs about 5000 within 0.2 pixels.
const maxArcPoints = 4096

func segments(dev, tol float32) int {
	if dev <= tol || tol <= 0 {
		return 1
	}
	return min(int(math.Ceil(math.Sqrt(float64(dev/tol)))), 1000)
}

func hypot(x, y float32) float32 { return float32(math.Hypot(float64(x), float64(y))) }
