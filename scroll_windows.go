package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/pancursor"
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/scroll"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

func (w *Window) placeScrollBar(e *El, vis geom.Rect, vertical bool) {
	n := &e.node
	st := e.effectiveScrollBarStyle()
	thick, inset := st.Thickness, st.Inset
	var track, thumb geom.Rect
	if s := e.state; s != nil {
		if vertical && s.ScrollY != s.BarScroll || !vertical && s.ScrollX != s.BarScrollX {
			s.ScrolledAt = w.now()
		}
		if !vertical {
			s.BarScrollX = s.ScrollX
		}
	}
	if vertical {
		end := min(st.TrackInset, max(0, (n.H-1)/2))
		track = geom.Rect{X: e.absX + n.W - thick - inset, Y: e.absY + end, W: thick, H: n.H - 2*end}
		th := scroll.ThumbLength(track.H, n.H, n.ContentH)
		// A thumb whose length changes while the view
		// holds still (rows measured, content added) glides to its new length;
		// while scrolling it follows at once.
		if s := e.state; s != nil {
			tr := s.track(animThumb, 1, 0.25)
			if s.ScrollY != s.BarScroll || !tr.Initialized() || reducedMotion.Load() {
				tr.Set([4]float32{th})
			} else {
				th = w.animate(s, animThumb, 1, 0.25, [4]float32{th}, scroll.GlideMotion)[0]
			}
			s.BarScroll = s.ScrollY
		}
		pos := scroll.ThumbPos(track.H, th, e.state.ScrollY, n.ContentH-n.H)
		thumb = geom.Rect{X: track.X, Y: track.Y + pos, W: thick, H: th}
	} else {
		end := min(st.TrackInset, max(0, (n.W-1)/2))
		track = geom.Rect{X: e.absX + end, Y: e.absY + n.H - thick - inset, W: n.W - 2*end, H: thick}
		tw := scroll.ThumbLength(track.W, n.W, n.ContentW)
		pos := scroll.ThumbPos(track.W, tw, e.state.ScrollX, n.ContentW-n.W)
		thumb = geom.Rect{X: track.X + pos, Y: track.Y, W: tw, H: thick}
	}
	w.bars = append(w.bars, scrollBar{
		el: e.id, track: track, thumb: thumb, vis: vis, vertical: vertical, above: len(w.hits),
	})
}

func (w *Window) paintScrollBars(e *El, color Color, opacity float32) {
	if !e.style.ScrollX && !e.style.ScrollY {
		return
	}
	dragging := w.press != nil && w.press.bar != nil && w.press.bar.el == e.id
	s := e.state
	if s == nil {
		return
	}
	st := e.effectiveScrollBarStyle()
	if st.Hidden {
		return
	}
	fade := float32(1)
	if !st.AlwaysVisible {
		show := w.hovered(e) || dragging
		if left := scrollBarLinger - w.now().Sub(s.ScrolledAt); left > 0 && !s.ScrolledAt.IsZero() {
			show = true
			w.frameAt(s.ScrolledAt.Add(scrollBarLinger))
		}
		to := float32(0)
		if show {
			to = 1
		}
		m := scrollBarFade
		if reducedMotion.Load() {
			m = reducedFade
		}
		fade = w.animate(s, animBar, 1, 0.002, [4]float32{to}, m)[0]
		if fade <= 0.001 {
			return
		}
	}
	for i := range w.bars {
		b := &w.bars[i]
		if b.el != e.id {
			continue
		}
		thumb := color.Alpha(0.3)
		switch {
		case dragging && st.DragColor.isSet():
			thumb = st.DragColor
		case st.Color.isSet():
			thumb = st.Color
		case dragging:
			thumb = color.Alpha(0.5)
		}
		rad := b.thumb.W * w.scale / 2
		if !b.vertical {
			rad = b.thumb.H * w.scale / 2
		}
		w.scene.Add(render.Prim{
			Bounds: w.snap(b.thumb),
			Clip:   w.snap(b.vis),
			Color:  thumb.arr(opacity * fade),
			Radii:  [4]float32{rad, rad, rad, rad},
			Kind:   render.KindRect,
		})
	}
}

func (w *Window) scrollBarJump(e *El, b *scrollBar, x, y float32) {
	n := &e.node
	if b.vertical {
		w.setScroll(e, e.state.ScrollX, scroll.TrackOffset(y-b.track.Y, b.track.H, b.thumb.H, n.ContentH-n.H))
	} else {
		w.setScroll(e, scroll.TrackOffset(x-b.track.X, b.track.W, b.thumb.W, n.ContentW-n.W), e.state.ScrollY)
	}
}

func (w *Window) dragScrollBar(p *pressState, x, y float32) {
	e := w.byID[p.bar.el]
	if e == nil || e.state == nil {
		return
	}
	n := &e.node
	b := p.bar
	if b.vertical {
		ratio := scroll.DragRatio(b.track.H, b.thumb.H, n.ContentH-n.H)
		w.setScroll(e, e.state.ScrollX, p.barStart+(y-p.y)*ratio)
	} else {
		ratio := scroll.DragRatio(b.track.W, b.thumb.W, n.ContentW-n.W)
		w.setScroll(e, p.barStart+(x-p.x)*ratio, e.state.ScrollY)
	}
}

type scrollStepRequest struct {
	key string
	up  bool
}

// applyScrollStep carries out a ScrollStep request.
func (w *Window) applyScrollStep(r *scrollStepRequest) {
	e := w.findKey(r.key)
	for e != nil && (e.state == nil || !e.style.ScrollY) {
		e = e.parent
	}
	if e == nil {
		return
	}
	var spans [][2]float32
	for _, c := range e.children {
		if !c.style.Absolute && !c.style.Hidden {
			spans = append(spans, [2]float32{c.node.Y, c.node.Y + c.node.H})
		}
	}
	n := &e.node
	inset := e.clipInset[2]
	if r.up {
		inset = e.clipInset[0]
	}
	y := scroll.StepTarget(spans, e.state.ScrollY, n.H, max(n.ContentH-n.H, 0), inset, r.up)
	w.setScroll(e, e.state.ScrollX, y)
}

// wheel handles a wheel step of delta notches at (x, y), signed as
// WM_MOUSEWHEEL and WM_MOUSEHWHEEL. fractional marks a step that is not a
// whole notch: precision touchpads and high-resolution wheels send them, and
// a whole notch in the middle of such a stream is treated alike.
func (w *Window) setScroll(e *El, sx, sy float32) bool {
	n := &e.node
	if !e.state.Set(sx, sy, n.ContentW-n.W, n.ContentH-n.H) {
		return false
	}
	if e.onScroll != nil {
		e.onScroll(e.state.ScrollX, e.state.ScrollY)
		w.markDirty(frameprof.CauseScroll)
	}
	w.invalidate()
	return true
}

// wheelScroll moves a scroll container's target offset; the offset follows it smoothly.
func (w *Window) wheelScroll(e *El, dx, dy float32) bool {
	n := &e.node
	if !e.state.Wheel(dx, dy, n.ContentW-n.W, n.ContentH-n.H) {
		return false
	}
	w.invalidate()
	return true
}

// scrollNow moves a scroll container at once, continuing from where smooth
// scrolling was heading.
func (w *Window) scrollNow(e *El, dx, dy float32) bool {
	bx, by := e.state.Base()
	return w.setScroll(e, bx+dx, by+dy)
}

// latchHolding returns the latched container's state when a wheel step
// continues the gesture over it from target and it can still scroll along the
// gesture's axis, else nil.
func (w *Window) latchHolding(target *El, x, y float32, now time.Time, horizontal bool, mods keyboard.Mods) *scroll.State {
	l := w.wheelLatch
	if !l.Continues(x, y, now, horizontal, mods) {
		return nil
	}
	for e := target; e != nil; e = e.parent {
		if e.state != nil && &e.state.State == l.State {
			n := &e.node
			canY := e.style.ScrollY && n.ContentH > n.H
			canX := e.style.ScrollX && n.ContentW > n.W
			if !horizontal && canY || (horizontal || !e.style.ScrollY) && canX {
				return l.State
			}
			return nil
		}
	}
	return nil
}

func (w *Window) wheel(x, y, delta float32, fractional, hwheel bool) {
	if hwheel {
		delta = -delta
	}
	now := time.Now()
	if fractional {
		w.preciseWheelAt = now
	}
	precise := now.Sub(w.preciseWheelAt) < 100*time.Millisecond
	w.onWheel(x, y, delta, hwheel, precise)
}

// onWheel offers a wheel step to the elements under the pointer, innermost
// first: an OnWheel handler that returns true takes it, otherwise the first
// scroll container that can move scrolls. While a gesture continues over the
// container it scrolled (wheelLatch), only that container scrolls; OnWheel
// handlers outside it still see the steps it cannot take. delta is in
// notches, positive away from the user or, for a tilt wheel (hwheel), to the
// left. Precise input, already smooth, is applied at once; whole notches
// animate.
func (w *Window) onWheel(x, y, delta float32, hwheel, precise bool) {
	w.hideTooltip()
	w.stopAutoScroll()
	mods := w.mods()
	horizontal := hwheel || mods&keyboard.ModShift != 0
	ev := WheelEvent{X: x, Y: y, Mods: mods, Precise: precise}
	if hwheel {
		ev.DX = -delta
	} else {
		ev.DY = -delta
	}
	lines := uint32(3)
	if w.scriptWheelLines != nil {
		lines = *w.scriptWheelLines
	} else {
		win.SystemParametersInfo(win.SPI_GETWHEELSCROLLLINES, 0, unsafe.Pointer(&lines))
	}
	// step is how far a notch scrolls a view of the given size. A setting of
	// 0 lines scrolls nothing.
	step := func(view float32) float32 { return scroll.WheelStep(delta, lines, view) }
	move := w.wheelScroll
	if precise {
		move = w.scrollNow
	}
	now := w.now()
	target := w.hitTest(x, y)
	latch := w.latchHolding(target, x, y, now, horizontal, mods)
	for e := target; e != nil; e = e.parent {
		if e.onWheel != nil {
			ev.LocalX, ev.LocalY, ev.W, ev.H = x-e.absX, y-e.absY, e.w, e.h
			handled := e.onWheel(ev)
			w.markDirty(frameprof.CauseWheel)
			if handled {
				break
			}
		}
		if e.state == nil || e.kind == kindInput || latch != nil && &e.state.State != latch {
			continue
		}
		n := &e.node
		canY := e.style.ScrollY && n.ContentH > n.H
		canX := e.style.ScrollX && n.ContentW > n.W
		moved := false
		if !horizontal && canY {
			moved = move(e, 0, -step(n.H))
		} else if (horizontal || !e.style.ScrollY) && canX {
			moved = move(e, -step(n.W), 0)
		}
		if latch == nil && moved {
			w.wheelLatch = scroll.Latch{State: &e.state.State, X: x, Y: y, Horizontal: horizontal, Mods: mods}
		}
		if latch != nil || moved {
			w.wheelLatch.At = now
		}
		if moved {
			break
		}
	}
	if w.dirty {
		w.Update()
	}
}

// A middle press on a scroll container that no element under the pointer
// drags with the middle button starts autoscroll, as in browsers on Windows:
// the view scrolls toward the pointer at a speed set by its distance from the
// press point. Releasing the button after moving away ends it; releasing in
// place keeps it on until the next press, key or wheel step.

// autoScrollPoll is how often the pointer is read while it is outside the
// window and the view is not moving.
const autoScrollPoll = 50 * time.Millisecond

type autoScroll struct {
	el         uint64
	x, y       float32 // the press point
	canX, canY bool
	held       bool // the middle button is still down
	moved      bool // the pointer left the dead zone while the button was held
	cursor     pancursor.Kind
}

// startAutoScroll starts autoscroll in the innermost scroll container that
// can move above top, and reports whether it did.
func (w *Window) startAutoScroll(top *El, x, y float32) bool {
	for e := top; e != nil; e = e.parent {
		if e.dragsWith(2) {
			return false
		}
		if e.state == nil || e.kind == kindInput {
			continue
		}
		n := &e.node
		canY := e.style.ScrollY && n.ContentH > n.H
		canX := e.style.ScrollX && n.ContentW > n.W
		if !canX && !canY {
			continue
		}
		s := e.state
		s.ScrollTX, s.ScrollTY, s.Smooth = s.ScrollX, s.ScrollY, false
		s.StopGlide()
		w.autoScroll = &autoScroll{el: e.id, x: x, y: y, canX: canX, canY: canY, held: true}
		w.setCapture()
		w.applyCursor()
		w.invalidate()
		return true
	}
	return false
}

func (w *Window) stopAutoScroll() {
	a := w.autoScroll
	if a == nil {
		return
	}
	w.autoScroll = nil
	if a.held {
		w.releaseCapture()
	}
	if w.mouseIn {
		w.applyCursor()
	}
	w.invalidate()
}

// autoScrollMove follows the pointer while autoscroll is on.
func (w *Window) autoScrollMove(x, y float32) {
	a := w.autoScroll
	if a.held && !a.moved && (abs32(x-a.x) > scroll.AutoDeadZone || abs32(y-a.y) > scroll.AutoDeadZone) {
		a.moved = true
	}
	if w.panCursor() != a.cursor {
		w.applyCursor()
	}
	w.invalidate()
}

// autoScrollRelease handles a middle release and reports whether autoscroll
// took it.
func (w *Window) autoScrollRelease() bool {
	a := w.autoScroll
	if a == nil || !a.held {
		return false
	}
	a.held = false
	w.releaseCapture()
	// Capture may have suppressed the leave notification; the next move
	// re-arms it so that leaving the window is noticed. A headless window is
	// never under the cursor and must not track it.
	w.tracking = w.hl != nil
	if a.moved {
		w.stopAutoScroll()
	}
	return true
}

// stepAutoScroll moves the autoscrolled container by dt seconds of its
// current speed and keeps frames coming while it moves.
func (w *Window) stepAutoScroll(dt float32) {
	a := w.autoScroll
	if a == nil {
		return
	}
	e := w.byID[a.el]
	if e == nil || e.state == nil {
		w.stopAutoScroll()
		return
	}
	x, y := w.mouseX, w.mouseY
	outside := !w.mouseIn && !w.scripted && w.hl == nil
	if outside {
		// No moves arrive outside the window without capture; the pointer is
		// read here and polled while the view stands still.
		p := win.GetCursorPos()
		win.ScreenToClient(w.hwnd, &p)
		x, y = float32(p.X)/w.scale, float32(p.Y)/w.scale
	}
	var vx, vy float32
	if a.canX {
		vx = scroll.AutoSpeed(x - a.x)
	}
	if a.canY {
		vy = scroll.AutoSpeed(y - a.y)
	}
	s, n := e.state, &e.node
	stuckX := vx == 0 || (vx < 0 && s.ScrollX <= 0) || (vx > 0 && s.ScrollX >= max(n.ContentW-n.W, 0))
	stuckY := vy == 0 || (vy < 0 && s.ScrollY <= 0) || (vy > 0 && s.ScrollY >= max(n.ContentH-n.H, 0))
	if stuckX && stuckY {
		if outside {
			w.frameAt(time.Now().Add(autoScrollPoll))
		}
		return
	}
	w.animating = true
	w.setScroll(e, s.ScrollX+vx*dt, s.ScrollY+vy*dt)
}

// panCursor is the cursor for the pointer's place relative to the press point.
func (w *Window) panCursor() pancursor.Kind {
	a := w.autoScroll
	var vx, vy float32
	if a.canX {
		vx = scroll.AutoSpeed(w.mouseX - a.x)
	}
	if a.canY {
		vy = scroll.AutoSpeed(w.mouseY - a.y)
	}
	return pancursor.At(vx, vy, a.canX, a.canY)
}

func (w *Window) setPanCursor() {
	a := w.autoScroll
	a.cursor = w.panCursor()
	size := int(win.GetSystemMetricsForDpi(win.SM_CXCURSOR, uint32(w.scale*96+0.5)))
	if size <= 0 {
		size = 32
	}
	win.SetCursor(pancursor.Handle(a.cursor, size))
}
