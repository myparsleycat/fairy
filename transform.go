package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/motion"
)

type transformProps struct {
	scale      float32 // 1 when unset
	rotate     float32 // radians, clockwise
	ox, oy     float32 // pivot in units of the element's size
	originSet  bool
	pressScale float32 // 0 when unset
	pressX     float32 // PressOffset
	pressY     float32
	motion     *motion.Spec
}

func (e *El) xfProps() *transformProps {
	if e.xfp == nil {
		e.xfp = &transformProps{scale: 1, ox: 0.5, oy: 0.5}
	}
	return e.xfp
}

// Scale draws the element and everything in it k times its size about its
// Origin: fills, borders, shadows, text, icons and images all scale. Layout
// keeps the unscaled size, so siblings don't move; hit testing follows the
// scaled box. A change springs on ScaleMotion, else the element's
// AnimateLayout motion, else motion.Default; with ReducedMotion it lands at
// once. Anything not finite or below zero is 1.
func (e *El) Scale(k float32) *El {
	if !finite(k) || k < 0 {
		k = 1
	}
	e.xfProps().scale = k
	return e
}

// Rotate draws the element and everything in it turned radians clockwise
// about its Origin. Layout keeps the upright box; hit testing follows the
// turned box. A change springs like Scale. Native surfaces (video, web views)
// inside it stay upright.
func (e *El) Rotate(radians float32) *El {
	if !finite(radians) {
		radians = 0
	}
	e.xfProps().rotate = radians
	return e
}

// Origin sets the point the element scales and rotates about, in units of
// its own size: (0.5, 0.5), the default, is its center; (0.5, 1) the middle
// of its bottom edge. Transition scales pivot here too.
func (e *El) Origin(x, y float32) *El {
	if !finite(x) || !finite(y) {
		x, y = 0.5, 0.5
	}
	p := e.xfProps()
	p.ox, p.oy, p.originSet = x, y, true
	return e
}

// origin returns the point the element scales about: its Origin, else the
// anchor side of a popover root, else its center.
func (e *El) origin() (float32, float32) {
	if p := e.xfp; p != nil && p.originSet {
		return p.ox, p.oy
	}
	if e.popSide != 0 {
		return e.popOX, e.popOY
	}
	return 0.5, 0.5
}

// PressScale shrinks the element to k about its Origin while a press on it is
// held (0.96 to 0.97 is typical), springing back on release or when the
// pointer leaves. Put it on an element with OnClick. Layout and hit testing
// keep the resting size, and ReducedMotion turns it off.
func (e *El) PressScale(k float32) *El {
	if !finite(k) || k <= 0 {
		k = 0
	}
	e.xfProps().pressScale = k
	return e
}

// PressOffset draws the element moved by (dx, dy) while a press on it is
// held, like Tailwind's active:translate-y-px, moving on motion.Fast. Layout and hit
// testing keep the resting place, and ReducedMotion turns it off.
func (e *El) PressOffset(dx, dy float32) *El {
	if !finite(dx) || !finite(dy) {
		dx, dy = 0, 0
	}
	p := e.xfProps()
	p.pressX, p.pressY = dx, dy
	return e
}

// ScaleMotion sets the motion Scale and Rotate changes run on.
func (e *El) ScaleMotion(m motion.Spec) *El { e.xfProps().motion = &m; return e }

func finite(f float32) bool { return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) }

// xform is a 2D affine map: (x, y) → (a x + c y + tx, b x + d y + ty).
type xform struct{ a, b, c, d, tx, ty float32 }

var identityXform = xform{a: 1, d: 1}

func (m xform) identity() bool { return m == identityXform }

func (m xform) apply(x, y float32) (float32, float32) {
	return m.a*x + m.c*y + m.tx, m.b*x + m.d*y + m.ty
}

// then returns the map that applies n, then m.
func (m xform) then(n xform) xform {
	return xform{
		a:  m.a*n.a + m.c*n.b,
		b:  m.b*n.a + m.d*n.b,
		c:  m.a*n.c + m.c*n.d,
		d:  m.b*n.c + m.d*n.d,
		tx: m.a*n.tx + m.c*n.ty + m.tx,
		ty: m.b*n.tx + m.d*n.ty + m.ty,
	}
}

func (m xform) inverse() xform {
	det := m.a*m.d - m.b*m.c
	if det == 0 {
		return xform{tx: float32(math.Inf(1)), ty: float32(math.Inf(1))} // maps every point away
	}
	ia, ib, ic, id := m.d/det, -m.b/det, -m.c/det, m.a/det
	return xform{a: ia, b: ib, c: ic, d: id, tx: -(ia*m.tx + ic*m.ty), ty: -(ib*m.tx + id*m.ty)}
}

// scaled returns the same map in a space s times larger, as from logical to device pixels.
func (m xform) scaled(s float32) xform {
	m.tx *= s
	m.ty *= s
	return m
}

// turnAbout returns the map that scales by k and turns by radians about (px, py).
func turnAbout(px, py, k, radians float32) xform {
	sin, cos := math.Sincos(float64(radians))
	a, b := k*float32(cos), k*float32(sin)
	m := xform{a: a, b: b, c: -b, d: a}
	m.tx = px - (m.a*px + m.c*py)
	m.ty = py - (m.b*px + m.d*py)
	return m
}
