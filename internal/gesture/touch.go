// Package gesture recognizes touch contacts: taps, long presses and pans, and
// the finger's velocity when a pan lets go.
package gesture

import "time"

const (
	Slop      = 15 // logical pixels a tap may move, as Chromium's touch slop
	LongPress = 500 * time.Millisecond
	// axisLock is tan(30°): a pan closer than 30° to an axis stays on it, as
	// Firefox's apz.axis_lock.lock_angle.
	axisLock = 0.57735

	// The release velocity is fitted to the moves of the last velocityHorizon.
	// A pause longer than velocityMoveGap between moves, or velocityStopGap
	// before the release, means the finger had stopped, as in Chromium.
	velocityHorizon = 100 * time.Millisecond
	velocityMoveGap = 40 * time.Millisecond
	velocityStopGap = 80 * time.Millisecond
	samples         = 20
)

// Phase is how far a contact has been recognized.
type Phase uint8

const (
	Pending Phase = iota // not yet a tap, long press or pan
	Panning
	Pressed // handed to the mouse press path
	Done    // recognized or abandoned; the rest of the contact is ignored
)

type sample struct {
	x, y float32
	t    time.Time
}

// Touch follows one contact.
type Touch struct {
	ID     uint32
	X0, Y0 float32 // where the contact started
	LX, LY float32 // where the panned view last followed the finger
	Phase  Phase
	// Target, CanX and CanY are the scroll container a pan moves and the axes
	// it moves along.
	Target     uint64
	CanX, CanY bool
	// SuppressTap: the contact stopped a fling, which is all it does if lifted.
	SuppressTap bool
	samples     [samples]sample
	n           int
}

// New starts following contact id at (x, y).
func New(id uint32, x, y float32, at time.Time) *Touch {
	t := &Touch{ID: id, X0: x, Y0: y, LX: x, LY: y}
	t.Record(x, y, at)
	return t
}

// Record adds a position of the contact.
func (t *Touch) Record(x, y float32, at time.Time) {
	t.samples[t.n%samples] = sample{x, y, at}
	t.n++
}

// PastSlop reports whether (x, y) lies further than Slop from the start.
func (t *Touch) PastSlop(x, y float32) bool {
	dx, dy := x-t.X0, y-t.Y0
	return dx*dx+dy*dy > Slop*Slop
}

// Axes reports whether a pan by (dx, dy) stays on the horizontal or the
// vertical axis.
func Axes(dx, dy float32) (onlyX, onlyY bool) {
	ax, ay := abs(dx), abs(dy)
	return ay <= ax*axisLock, ax <= ay*axisLock
}

// Follow returns how far the finger moved since the view last followed it,
// along the axes the pan moves, and makes (x, y) the point followed.
func (t *Touch) Follow(x, y float32) (dx, dy float32) {
	dx, dy = x-t.LX, y-t.LY
	t.LX, t.LY = x, y
	if !t.CanX {
		dx = 0
	}
	if !t.CanY {
		dy = 0
	}
	return dx, dy
}

// ReleaseVelocity is Velocity along the axes the pan moves.
func (t *Touch) ReleaseVelocity() (float32, float32) {
	vx, vy := t.Velocity()
	if !t.CanX {
		vx = 0
	}
	if !t.CanY {
		vy = 0
	}
	return vx, vy
}

// Velocity fits the finger's velocity, in logical pixels per second, to the
// recent samples by least squares.
func (t *Touch) Velocity() (float32, float32) {
	n := min(t.n, samples)
	if n < 2 {
		return 0, 0
	}
	var pts [samples]sample
	newest := t.samples[(t.n-1)%samples]
	prev, k := newest.t, 0
	for i := range n {
		s := t.samples[(t.n-1-i)%samples]
		gap := velocityMoveGap
		if i == 1 {
			gap = velocityStopGap
		}
		if newest.t.Sub(s.t) > velocityHorizon || prev.Sub(s.t) > gap {
			break
		}
		pts[k], prev = s, s.t
		k++
	}
	if k < 2 {
		return 0, 0
	}
	var mt, mx, my float64
	for _, s := range pts[:k] {
		mt += s.t.Sub(newest.t).Seconds()
		mx += float64(s.x)
		my += float64(s.y)
	}
	mt, mx, my = mt/float64(k), mx/float64(k), my/float64(k)
	var stt, stx, sty float64
	for _, s := range pts[:k] {
		dt := s.t.Sub(newest.t).Seconds() - mt
		stt += dt * dt
		stx += dt * (float64(s.x) - mx)
		sty += dt * (float64(s.y) - my)
	}
	if stt == 0 {
		return 0, 0
	}
	return float32(stx / stt), float32(sty / stt)
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
