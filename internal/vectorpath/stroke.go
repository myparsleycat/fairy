package vectorpath

import (
	"math"

	"golang.org/x/image/vector"
)

type Point struct{ X, Y float32 }

// Stroke adds to r the outline of a line of half-width half through pts. It
// traces connected stroke boundaries because overlapping segment rectangles
// and discs would add coverage at antialiased edges in vector.Rasterizer,
// making curves bolder.
func Stroke(r *vector.Rasterizer, pts []Point, closed bool, half float32, roundCap bool) {
	clean := make([]Point, 0, len(pts))
	for _, p := range pts {
		if len(clean) == 0 || p != clean[len(clean)-1] {
			clean = append(clean, p)
		}
	}
	pts = clean
	if closed && len(pts) > 1 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	if len(pts) == 0 || half <= 0 {
		return
	}
	normal := func(a, b Point) Point {
		dx, dy := b.X-a.X, b.Y-a.Y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		return Point{-dy / l * half, dx / l * half}
	}
	arc := func(c, from Point, sweep float64) {
		a := math.Atan2(float64(from.Y), float64(from.X))
		// Limit arc deviation to 0.01px, with at least 32 segments per circle.
		n := max(1, int(math.Ceil(math.Abs(sweep)*max(16/math.Pi, math.Sqrt(float64(half)/0.08)))))
		for i := 1; i <= n; i++ {
			t := a + sweep*float64(i)/float64(n)
			r.LineTo(c.X+half*float32(math.Cos(t)), c.Y+half*float32(math.Sin(t)))
		}
	}
	if len(pts) == 1 {
		if !closed && roundCap {
			r.MoveTo(pts[0].X+half, pts[0].Y)
			arc(pts[0], Point{half, 0}, -2*math.Pi)
			r.ClosePath()
		}
		return
	}
	trace := func(reverse bool) {
		point := func(i int) Point {
			if reverse {
				return pts[len(pts)-1-i]
			}
			return pts[i]
		}
		p := point(0)
		prev := normal(p, point(1))
		if closed {
			prev = normal(point(len(pts)-1), p)
		}
		if closed || !reverse {
			r.MoveTo(p.X+prev.X, p.Y+prev.Y)
		} else {
			r.LineTo(p.X+prev.X, p.Y+prev.Y)
		}
		for i := range pts {
			p = point(i)
			r.LineTo(p.X+prev.X, p.Y+prev.Y)
			if !closed && i == len(pts)-1 {
				break
			}
			next := normal(p, point((i+1)%len(pts)))
			cross := prev.X*next.Y - prev.Y*next.X
			dot := prev.X*next.X + prev.Y*next.Y
			if cross < 0 {
				arc(p, prev, math.Atan2(float64(cross), float64(dot)))
			} else if denom := half*half + dot; denom > half*half*1e-6 {
				// Intersect the inside edges; only the outside of a join is round.
				k := half * half / denom
				before, after := point((i+len(pts)-1)%len(pts)), point((i+1)%len(pts))
				reach := math.Abs(float64(cross * half / denom))
				limit := min(math.Hypot(float64(p.X-before.X), float64(p.Y-before.Y)),
					math.Hypot(float64(after.X-p.X), float64(after.Y-p.Y)))
				if reach <= limit {
					r.LineTo(p.X+(prev.X+next.X)*k, p.Y+(prev.Y+next.Y)*k)
				} else {
					// A near reversal can put the intersection beyond both segments.
					r.LineTo(p.X, p.Y)
				}
			} else {
				arc(p, prev, -math.Pi)
			}
			r.LineTo(p.X+next.X, p.Y+next.Y)
			prev = next
		}
		if closed {
			r.ClosePath()
		} else if roundCap {
			arc(p, prev, -math.Pi)
		}
	}
	trace(false)
	trace(true)
	if !closed {
		r.ClosePath()
	}
}
