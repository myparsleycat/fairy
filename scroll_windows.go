package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/win"
	"github.com/myparsleycat/fairy/keyboard"
)

func (w *Window) placeScrollBar(e *El, vis rectF, vertical bool) {
	n := &e.node
	st := e.effectiveScrollBarStyle()
	thick, inset := st.Thickness, st.Inset
	var track, thumb rectF
	if s := e.state; s != nil {
		if vertical && s.scrollY != s.barScroll || !vertical && s.scrollX != s.barScrollX {
			s.scrolledAt = w.now()
		}
		if !vertical {
			s.barScrollX = s.scrollX
		}
	}
	if vertical {
		end := min(st.TrackInset, max(0, (n.H-1)/2))
		track = rectF{e.absX + n.W - thick - inset, e.absY + end, thick, n.H - 2*end}
		th := min(track.h, max(track.h*n.H/n.ContentH, 24))
		// A thumb whose length changes while the view
		// holds still (rows measured, content added) glides to its new length;
		// while scrolling it follows at once.
		if s := e.state; s != nil {
			tr := s.track(animThumb, 1, 0.25)
			if s.scrollY != s.barScroll || !tr.Initialized() || reducedMotion.Load() {
				tr.Set([4]float32{th})
			} else {
				th = w.animate(s, animThumb, 1, 0.25, [4]float32{th}, motionGlide)[0]
			}
			s.barScroll = s.scrollY
		}
		pos := (track.h - th) * (e.state.scrollY / max(n.ContentH-n.H, 1))
		thumb = rectF{track.x, track.y + pos, thick, th}
	} else {
		end := min(st.TrackInset, max(0, (n.W-1)/2))
		track = rectF{e.absX + end, e.absY + n.H - thick - inset, n.W - 2*end, thick}
		tw := min(track.w, max(track.w*n.W/n.ContentW, 24))
		pos := (track.w - tw) * (e.state.scrollX / max(n.ContentW-n.W, 1))
		thumb = rectF{track.x + pos, track.y, tw, thick}
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
	fade := float32(1)
	if !st.AlwaysVisible {
		show := w.hovered(e) || dragging
		if left := scrollBarLinger - w.now().Sub(s.scrolledAt); left > 0 && !s.scrolledAt.IsZero() {
			show = true
			w.frameAt(s.scrolledAt.Add(scrollBarLinger))
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
		rad := b.thumb.w * w.scale / 2
		if !b.vertical {
			rad = b.thumb.h * w.scale / 2
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
		frac := (y - b.track.y - b.thumb.h/2) / max(b.track.h-b.thumb.h, 1)
		w.setScroll(e, e.state.scrollX, frac*(n.ContentH-n.H))
	} else {
		frac := (x - b.track.x - b.thumb.w/2) / max(b.track.w-b.thumb.w, 1)
		w.setScroll(e, frac*(n.ContentW-n.W), e.state.scrollY)
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
		ratio := (n.ContentH - n.H) / max(b.track.h-b.thumb.h, 1)
		w.setScroll(e, e.state.scrollX, p.barStart+(y-p.y)*ratio)
	} else {
		ratio := (n.ContentW - n.W) / max(b.track.w-b.thumb.w, 1)
		w.setScroll(e, p.barStart+(x-p.x)*ratio, e.state.scrollY)
	}
}

func (w *Window) setScroll(e *El, sx, sy float32) bool {
	n := &e.node
	sx = min(max(sx, 0), max(n.ContentW-n.W, 0))
	sy = min(max(sy, 0), max(n.ContentH-n.H, 0))
	if sx == e.state.scrollX && sy == e.state.scrollY {
		return false
	}
	e.state.scrollX, e.state.scrollY = sx, sy
	e.state.smooth = false
	stopGlide(e.state)
	if e.onScroll != nil {
		e.onScroll(sx, sy)
		w.markDirty(dirtyScroll)
	}
	w.invalidate()
	return true
}

// wheelScroll moves a scroll container's target offset; the offset follows it smoothly.
func (w *Window) wheelScroll(e *El, dx, dy float32) bool {
	s, n := e.state, &e.node
	bx, by := scrollBase(s)
	tx := min(max(bx+dx, 0), max(n.ContentW-n.W, 0))
	ty := min(max(by+dy, 0), max(n.ContentH-n.H, 0))
	if tx == bx && ty == by {
		return false
	}
	if s.glide || s.fling {
		// The wheel takes over from a glide or a fling, from where the view is now.
		s.scrollTX, s.scrollTY = s.scrollX, s.scrollY
		tx = min(max(s.scrollX+dx, 0), max(n.ContentW-n.W, 0))
		ty = min(max(s.scrollY+dy, 0), max(n.ContentH-n.H, 0))
		stopGlide(s)
	}
	s.scrollTX, s.scrollTY, s.smooth = tx, ty, true
	w.invalidate()
	return true
}

// scrollNow moves a scroll container at once, continuing from where smooth
// scrolling was heading.
func (w *Window) scrollNow(e *El, dx, dy float32) bool {
	bx, by := scrollBase(e.state)
	return w.setScroll(e, bx+dx, by+dy)
}

// scrollBase is the offset a new scroll step starts from: where smooth
// scrolling is heading while it runs, else the current offset.
func scrollBase(s *elState) (float32, float32) {
	if s.smooth {
		return s.scrollTX, s.scrollTY
	}
	return s.scrollX, s.scrollY
}

// wheelPageScroll is WHEEL_PAGESCROLL, the wheel setting that scrolls a
// screen at a time.
const wheelPageScroll = ^uint32(0)

const (
	// wheelLatchTimeout is the pause in wheel input that ends a scroll
	// gesture, as Chromium's wheel scroll latching transaction.
	wheelLatchTimeout = 500 * time.Millisecond
	// wheelLatchSlop is how far, in logical pixels, the pointer may move
	// during a gesture before the next wheel step starts a new one.
	wheelLatchSlop = 4
)

// wheelLatch is the scroll container a wheel gesture scrolled. Until the
// gesture pauses or the pointer moves, the wheel scrolls only it: an inner list
// that reaches its end swallows the rest of the gesture instead of handing it
// to the page, and an inner list that passes under the pointer while the page
// scrolls does not take it over. A press or key ends the gesture.
type wheelLatch struct {
	state      *elState
	x, y       float32 // pointer position when the gesture started
	horizontal bool
	mods       keyboard.Mods
	at         time.Time // last wheel step of the gesture
}

// holding returns the latched container's state when the gesture continues
// over it from target and it can still scroll along the gesture's axis, else
// nil.
func (l wheelLatch) holding(target *El, x, y float32, now time.Time, horizontal bool, mods keyboard.Mods) *elState {
	dx, dy := x-l.x, y-l.y
	if l.state == nil || l.horizontal != horizontal || l.mods != mods || now.Sub(l.at) >= wheelLatchTimeout ||
		dx*dx+dy*dy > wheelLatchSlop*wheelLatchSlop {
		return nil
	}
	for e := target; e != nil; e = e.parent {
		if e.state == l.state {
			n := &e.node
			canY := e.style.ScrollY && n.ContentH > n.H
			canX := e.style.ScrollX && n.ContentW > n.W
			if !horizontal && canY || (horizontal || !e.style.ScrollY) && canX {
				return l.state
			}
			return nil
		}
	}
	return nil
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
	step := func(view float32) float32 {
		if lines == wheelPageScroll {
			return delta * view
		}
		return delta * float32(lines) * 100 / 3
	}
	scroll := w.wheelScroll
	if precise {
		scroll = w.scrollNow
	}
	now := w.now()
	target := w.hitTest(x, y)
	latch := w.wheelLatch.holding(target, x, y, now, horizontal, mods)
	for e := target; e != nil; e = e.parent {
		if e.onWheel != nil {
			ev.LocalX, ev.LocalY, ev.W, ev.H = x-e.absX, y-e.absY, e.w, e.h
			handled := e.onWheel(ev)
			w.markDirty(dirtyWheel)
			if handled {
				break
			}
		}
		if e.state == nil || e.kind == kindInput || latch != nil && e.state != latch {
			continue
		}
		n := &e.node
		canY := e.style.ScrollY && n.ContentH > n.H
		canX := e.style.ScrollX && n.ContentW > n.W
		moved := false
		if !horizontal && canY {
			moved = scroll(e, 0, -step(n.H))
		} else if (horizontal || !e.style.ScrollY) && canX {
			moved = scroll(e, -step(n.W), 0)
		}
		if latch == nil && moved {
			w.wheelLatch = wheelLatch{state: e.state, x: x, y: y, horizontal: horizontal, mods: mods}
		}
		if latch != nil || moved {
			w.wheelLatch.at = now
		}
		if moved {
			break
		}
	}
	if w.dirty {
		w.Update()
	}
}
