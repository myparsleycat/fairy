package fairy

import (
	"math"
	"time"
)

// Touch follows one contact, as browsers do on Windows. A touch on an element
// that drags with the left button, or on a scroll bar, is a left press held
// for as long as the finger stays down. Anywhere else it does nothing until it
// is recognized: lifted within touchSlop it is a left click, held for
// touchLongPress a right click, and moved further it pans the innermost scroll
// container that can move that way, then keeps going with inertia when lifted
// while still moving. Pen input stays with Windows, which turns it into mouse
// messages.

const (
	touchSlop      = 15 // logical pixels a tap may move, as Chromium's touch slop
	touchLongPress = 500 * time.Millisecond
	// touchAxisLock is tan(30°): a pan closer than 30° to an axis stays on it,
	// as Firefox's apz.axis_lock.lock_angle.
	touchAxisLock = 0.57735

	// A fling slows by flingFriction every millisecond, as Firefox's
	// apz.fling_friction. Speeds are in logical pixels per second.
	flingFriction = 0.002
	flingMinStart = 500
	flingStop     = 10
	flingMax      = 8000

	// The release velocity is fitted to the moves of the last velocityHorizon.
	// A pause longer than velocityMoveGap between moves, or velocityStopGap
	// before the release, means the finger had stopped, as in Chromium.
	velocityHorizon = 100 * time.Millisecond
	velocityMoveGap = 40 * time.Millisecond
	velocityStopGap = 80 * time.Millisecond
	touchSamples    = 20
)

// flingDecay is flingFriction as an exponential decay rate per second.
var flingDecay = -math.Log1p(-flingFriction) * 1000

type touchPhase uint8

const (
	touchPending touchPhase = iota // not yet a tap, long press or pan
	touchPanning
	touchPressed // handed to the mouse press path
	touchDone    // recognized or abandoned; the rest of the contact is ignored
)

type touchSample struct {
	x, y float32
	t    time.Time
}

type touchState struct {
	id         uint32
	x0, y0     float32 // where the contact started
	lx, ly     float32 // where the panned view last followed the finger
	phase      touchPhase
	el         uint64 // the scroll container a pan moves
	canX, canY bool
	// suppressTap: the contact stopped a fling, which is all it does if lifted.
	suppressTap bool
	samples     [touchSamples]touchSample
	n           int
}

func (t *touchState) record(x, y float32, at time.Time) {
	t.samples[t.n%touchSamples] = touchSample{x, y, at}
	t.n++
}

// velocity fits the finger's velocity, in logical pixels per second, to the
// recent samples by least squares.
func (t *touchState) velocity() (float32, float32) {
	n := min(t.n, touchSamples)
	if n < 2 {
		return 0, 0
	}
	var pts [touchSamples]touchSample
	newest := t.samples[(t.n-1)%touchSamples]
	prev, k := newest.t, 0
	for i := range n {
		s := t.samples[(t.n-1-i)%touchSamples]
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

func (w *Window) onTouchDown(id uint32, x, y float32, at time.Time) {
	if w.touch != nil {
		// A new primary contact means the last one ended without its up
		// reaching the window.
		w.onTouchCancel(w.touch.id)
	}
	w.hideTooltip()
	w.stopAutoScroll()
	t := &touchState{id: id, x0: x, y0: y, lx: x, ly: y}
	t.record(x, y, at)
	w.touch = t
	for e := w.pointerHitTest(x, y); e != nil; e = e.parent {
		if e.state != nil && e.state.fling {
			e.state.fling = false
			t.suppressTap = true
		}
	}
	if w.touchPresses(x, y) {
		t.phase = touchPressed
		w.touchButtonDown(x, y, 0)
		if w.press == nil {
			t.phase = touchDone
		}
		return
	}
	w.setTimer(timerTouch, uint32(touchLongPress.Milliseconds()))
}

// touchPresses reports whether a touch at (x, y) goes straight to the mouse
// press path: it is on a scroll bar or on an element that drags with the left
// button, which takes the touch instead of letting it pan.
func (w *Window) touchPresses(x, y float32) bool {
	top := w.chromeHitTest(x, y)
	chrome := top != nil
	if !chrome {
		if w.barAt(x, y) != nil {
			return true
		}
		top = w.hitTest(x, y)
	}
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		if e.dragsWith(0) {
			return true
		}
	}
	return false
}

func (w *Window) onTouchMove(id uint32, x, y float32, at time.Time) {
	t := w.touch
	if t == nil || t.id != id {
		return
	}
	t.record(x, y, at)
	switch t.phase {
	case touchPressed:
		w.onMouseMove(x, y)
	case touchPending:
		dx, dy := x-t.x0, y-t.y0
		if dx*dx+dy*dy <= touchSlop*touchSlop {
			return
		}
		w.killTimer(timerTouch)
		if !w.startTouchPan(t, dx, dy) {
			t.phase = touchDone
			return
		}
		// The view starts following from here, so it does not jump by the slop.
		t.lx, t.ly = x, y
	case touchPanning:
		dx, dy := x-t.lx, y-t.ly
		t.lx, t.ly = x, y
		e := w.byID[t.el]
		if e == nil || e.state == nil {
			t.phase = touchDone
			return
		}
		if !t.canX {
			dx = 0
		}
		if !t.canY {
			dy = 0
		}
		w.scrollNow(e, -dx, -dy)
		if w.dirty {
			w.Update()
		}
	}
}

// startTouchPan picks the scroll container a pan moved by (dx, dy) from its
// start drives: the innermost one under the start that can still move that
// way. It keeps it for the rest of the contact and the fling after it.
func (w *Window) startTouchPan(t *touchState, dx, dy float32) bool {
	ax, ay := abs32(dx), abs32(dy)
	onlyY := ax <= ay*touchAxisLock
	onlyX := ay <= ax*touchAxisLock
	for e := w.pointerHitTest(t.x0, t.y0); e != nil; e = e.parent {
		if e.state == nil || e.kind == kindInput {
			continue
		}
		s, n := e.state, &e.node
		maxX, maxY := n.ContentW-n.W, n.ContentH-n.H
		canX := e.style.ScrollX && maxX > 0 && !onlyY
		canY := e.style.ScrollY && maxY > 0 && !onlyX
		movesX := canX && (dx > 0 && s.scrollX > 0 || dx < 0 && s.scrollX < maxX)
		movesY := canY && (dy > 0 && s.scrollY > 0 || dy < 0 && s.scrollY < maxY)
		if movesX || movesY {
			t.phase, t.el, t.canX, t.canY = touchPanning, e.id, canX, canY
			return true
		}
	}
	return false
}

func (w *Window) onTouchUp(id uint32, x, y float32, at time.Time) {
	t := w.touch
	if t == nil || t.id != id {
		return
	}
	if t.phase == touchPanning {
		w.onTouchMove(id, x, y, at)
	}
	w.touch = nil
	w.killTimer(timerTouch)
	switch t.phase {
	case touchPressed:
		w.onButtonUp(x, y, 0)
	case touchPending:
		if !t.suppressTap {
			w.touchButtonDown(t.x0, t.y0, 0)
			w.onButtonUp(t.x0, t.y0, 0)
		}
	case touchPanning:
		if e := w.byID[t.el]; e != nil && e.state != nil {
			vx, vy := t.velocity()
			if !t.canX {
				vx = 0
			}
			if !t.canY {
				vy = 0
			}
			w.startFling(e, -vx, -vy)
		}
	}
	w.endTouchHover()
}

func (w *Window) onTouchCancel(id uint32) {
	t := w.touch
	if t == nil || t.id != id {
		return
	}
	w.touch = nil
	w.killTimer(timerTouch)
	if t.phase == touchPressed && w.press != nil {
		w.cancelPress()
		w.releasePointer()
	}
	w.endTouchHover()
}

// onTouchLongPress turns a contact held in place into a right click.
func (w *Window) onTouchLongPress() {
	t := w.touch
	if t == nil || t.phase != touchPending {
		return
	}
	t.phase = touchDone
	if t.suppressTap {
		return
	}
	w.touchButtonDown(t.x0, t.y0, 1)
	w.onButtonUp(t.x0, t.y0, 1)
}

// touchButtonDown is onButtonDown for a touch, which allows touchSlop of
// finger movement where the mouse allows a few pixels: between the taps of a
// double tap and before a press starts to drag.
func (w *Window) touchButtonDown(x, y float32, button int) {
	w.touchPress = true
	w.onButtonDown(x, y, button)
	w.touchPress = false
}

// endTouchHover clears the hover a touch left behind: no pointer stays over
// the window once the finger lifts.
func (w *Window) endTouchHover() {
	w.mouseIn = false
	w.hideTooltip()
	if w.press == nil && w.setHoverPath(nil) {
		w.invalidate()
	}
}

// startFling keeps a scroll container moving at (vx, vy) logical pixels per
// second once a pan lets go, slowing by flingFriction.
func (w *Window) startFling(e *El, vx, vy float32) {
	speed := float32(math.Hypot(float64(vx), float64(vy)))
	if speed < flingMinStart {
		return
	}
	if speed > flingMax {
		vx, vy = vx*flingMax/speed, vy*flingMax/speed
	}
	s := e.state
	s.scrollTX, s.scrollTY, s.smooth = s.scrollX, s.scrollY, false
	stopGlide(s)
	s.fling, s.flingVX, s.flingVY = true, vx, vy
	w.invalidate()
}

// stepFling advances a fling by the frame's dt. Integrating the exponential
// decay keeps the distance the same at any frame rate.
func (w *Window) stepFling(e *El, maxX, maxY float32) {
	s := e.state
	if w.dt > 0 {
		left := math.Exp(-flingDecay * float64(w.dt))
		travel := float32((1 - left) / flingDecay)
		x := s.scrollX + s.flingVX*travel
		y := s.scrollY + s.flingVY*travel
		s.flingVX *= float32(left)
		s.flingVY *= float32(left)
		// An edge stops the fling along its axis.
		if x <= 0 && s.flingVX < 0 || x >= maxX && s.flingVX > 0 {
			s.flingVX = 0
		}
		if y <= 0 && s.flingVY < 0 || y >= maxY && s.flingVY > 0 {
			s.flingVY = 0
		}
		s.scrollX, s.scrollY = min(max(x, 0), maxX), min(max(y, 0), maxY)
	}
	if math.Hypot(float64(s.flingVX), float64(s.flingVY)) < flingStop {
		s.fling = false
	} else {
		w.animating = true
	}
	s.scrollTX, s.scrollTY = s.scrollX, s.scrollY
	s.track(animScroll, 2, 0.1).Set([4]float32{s.scrollX, s.scrollY})
	if e.onScroll != nil {
		e.onScroll(s.scrollX, s.scrollY)
	}
}
