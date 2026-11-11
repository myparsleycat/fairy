package motion

import "math"

// Track animates up to four components toward a target.
type Track struct {
	cur, vel, target, from [4]float32
	// k weights the ease term s(1-s)² that carries the velocity a moving ease
	// had when its target changed.
	k [4]float32
	// off is a spring's cur−target at full precision: far from zero, float32
	// cur cannot hold the last fraction of a pixel and the spring would stall.
	off     [4]float64
	elapsed float32
	n       int
	init    bool
	moving  bool
	eps     float32
}

// NewTrack animates n components (1 through 4) with settlement tolerance eps.
func NewTrack(n int, eps float32) *Track { return &Track{n: n, eps: eps} }

// Set jumps to v without animating.
func (t *Track) Set(v [4]float32) {
	t.cur, t.target, t.from = v, v, v
	t.vel, t.k, t.off = [4]float32{}, [4]float32{}, [4]float64{}
	t.moving, t.init = false, true
}

// Step advances by dt seconds toward target and returns the current value.
func (t *Track) Step(target [4]float32, m Spec, dt float32) [4]float32 {
	if !t.init || m.kind == motionNone {
		t.Set(target)
		return t.cur
	}
	if target != t.target {
		if m.kind == motionEase {
			// A fresh ease is the plain ease-out curve; one that was already
			// moving keeps its current velocity at the start of the new curve.
			for i := 0; i < t.n; i++ {
				t.k[i] = 0
				if t.moving {
					t.k[i] = t.vel[i]*m.duration - 3*(target[i]-t.cur[i])
				}
			}
		}
		t.from = t.cur
		t.target = target
		for i := 0; i < t.n; i++ {
			t.off[i] = float64(t.cur[i]) - float64(target[i])
		}
		t.elapsed = 0
		t.moving = true
	}
	if !t.moving {
		return t.cur
	}
	switch m.kind {
	case motionEase:
		t.elapsed += dt
		s := min(t.elapsed/m.duration, 1)
		u := 1 - s
		e, de := 1-u*u*u, 3*u*u // ease-out cubic and its derivative
		g, dg := s*u*u, u*(1-3*s)
		for i := 0; i < t.n; i++ {
			d := t.target[i] - t.from[i]
			t.cur[i] = t.from[i] + d*e + t.k[i]*g
			t.vel[i] = (d*de + t.k[i]*dg) / m.duration
		}
		if s >= 1 {
			t.Set(t.target)
		}
	case motionBezier:
		t.elapsed += dt
		s := min(t.elapsed/m.duration, 1)
		e, de := m.BezierAt(s)
		for i := 0; i < t.n; i++ {
			d := t.target[i] - t.from[i]
			t.cur[i] = t.from[i] + d*e
			t.vel[i] = d * de / m.duration
		}
		if s >= 1 {
			t.Set(t.target)
		}
	default:
		w := 2 * math.Pi / float64(m.duration)
		k := w * w
		c := 2 * w * (1 - float64(m.bounce))
		// Semi-implicit Euler is stable while w·h stays below about 0.8; the
		// shortest springs take smaller steps than 1/240 s.
		maxH := min(1.0/240, 0.5/w)
		settled := true
		for i := 0; i < t.n; i++ {
			x, v := t.off[i], float64(t.vel[i])
			if t.target[i]+float32(x) != t.cur[i] {
				x = float64(t.cur[i]) - float64(t.target[i])
			}
			for remaining := float64(dt); remaining > 0; {
				h := min(remaining, maxH)
				remaining -= h
				v += (-k*x - c*v) * h
				x += v * h
			}
			t.off[i], t.vel[i] = x, float32(v)
			t.cur[i] = t.target[i] + float32(x)
			if math.Abs(x) > float64(t.eps) || math.Abs(v) > float64(t.eps)*10 {
				settled = false
			}
		}
		if settled {
			t.Set(t.target)
		}
	}
	return t.cur
}

// BezierAt returns the progress of a Bezier motion at time fraction x, and
// its slope.
func (m Spec) BezierAt(x float32) (y, slope float32) {
	if x <= 0 {
		return 0, bezierSlopeAt(m, 0)
	}
	if x >= 1 {
		return 1, bezierSlopeAt(m, 1)
	}
	t := x
	for range 8 {
		dx := bezierD(t, m.x1, m.x2)
		if abs32(dx) < 1e-6 {
			break
		}
		t2 := t - (bezierC(t, m.x1, m.x2)-x)/dx
		if t2 < 0 || t2 > 1 {
			break
		}
		t = t2
	}
	if abs32(bezierC(t, m.x1, m.x2)-x) > 1e-5 {
		lo, hi := float32(0), float32(1)
		for range 30 {
			t = (lo + hi) / 2
			if bezierC(t, m.x1, m.x2) < x {
				lo = t
			} else {
				hi = t
			}
		}
	}
	return bezierC(t, m.y1, m.y2), bezierSlopeAt(m, t)
}

// bezierC is one coordinate of a cubic Bezier from 0 to 1 with inner control
// values a and b, at parameter t.
func bezierC(t, a, b float32) float32 {
	u := 1 - t
	return 3*u*u*t*a + 3*u*t*t*b + t*t*t
}

func bezierD(t, a, b float32) float32 {
	u := 1 - t
	return 3*u*u*a + 6*u*t*(b-a) + 3*t*t*(1-b)
}

func bezierSlopeAt(m Spec, t float32) float32 {
	dx := bezierD(t, m.x1, m.x2)
	if abs32(dx) < 1e-6 {
		return 0
	}
	return bezierD(t, m.y1, m.y2) / dx
}

// Initialized reports whether Set or Step has given the track a value.
func (t *Track) Initialized() bool { return t.init }

// Moving reports whether the track is still animating toward its target.
func (t *Track) Moving() bool { return t.moving }

// Current returns the current value.
func (t *Track) Current() [4]float32 { return t.cur }

// Shift moves the current value, the target and the start of the animation
// by delta, so the motion continues unchanged relative to the new position.
func (t *Track) Shift(delta [4]float32) {
	for i := range t.n {
		t.cur[i] += delta[i]
		t.target[i] += delta[i]
		t.from[i] += delta[i]
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
