package fairy

import (
	"sync"
	"time"

	"github.com/myparsleycat/fairy/internal/pancursor"
	"github.com/myparsleycat/fairy/internal/win"
)

// A middle press on a scroll container that no element under the pointer
// drags with the middle button starts autoscroll, as in browsers on Windows:
// the view scrolls toward the pointer at a speed set by its distance from the
// press point. Releasing the button after moving away ends it; releasing in
// place keeps it on until the next press, key or wheel step.

// autoScrollDeadZone is the distance from the press point, in logical pixels
// along each axis, within which the view does not scroll.
const autoScrollDeadZone = 10

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
		s.scrollTX, s.scrollTY, s.smooth = s.scrollX, s.scrollY, false
		stopGlide(s)
		w.autoScroll = &autoScroll{el: e.id, x: x, y: y, canX: canX, canY: canY, held: true}
		win.SetCapture(w.hwnd)
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
		win.ReleaseCapture()
	}
	if w.mouseIn {
		w.applyCursor()
	}
	w.invalidate()
}

// autoScrollMove follows the pointer while autoscroll is on.
func (w *Window) autoScrollMove(x, y float32) {
	a := w.autoScroll
	if a.held && !a.moved && (abs32(x-a.x) > autoScrollDeadZone || abs32(y-a.y) > autoScrollDeadZone) {
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
	win.ReleaseCapture()
	// Capture may have suppressed the leave notification; the next move
	// re-arms it so that leaving the window is noticed.
	w.tracking = false
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
	outside := !w.mouseIn && !w.scripted
	if outside {
		// No moves arrive outside the window without capture; the pointer is
		// read here and polled while the view stands still.
		p := win.GetCursorPos()
		win.ScreenToClient(w.hwnd, &p)
		x, y = float32(p.X)/w.scale, float32(p.Y)/w.scale
	}
	var vx, vy float32
	if a.canX {
		vx = autoScrollSpeed(x - a.x)
	}
	if a.canY {
		vy = autoScrollSpeed(y - a.y)
	}
	s, n := e.state, &e.node
	stuckX := vx == 0 || (vx < 0 && s.scrollX <= 0) || (vx > 0 && s.scrollX >= max(n.ContentW-n.W, 0))
	stuckY := vy == 0 || (vy < 0 && s.scrollY <= 0) || (vy > 0 && s.scrollY >= max(n.ContentH-n.H, 0))
	if stuckX && stuckY {
		if outside {
			w.frameAt(time.Now().Add(autoScrollPoll))
		}
		return
	}
	w.animating = true
	w.setScroll(e, s.scrollX+vx*dt, s.scrollY+vy*dt)
}

// autoScrollSpeed is the scroll speed in logical pixels per second for a
// pointer d pixels from the press point along one axis; it grows faster than
// the distance so that a long reach covers long content.
func autoScrollSpeed(d float32) float32 {
	m := abs32(d) - autoScrollDeadZone
	if m <= 0 {
		return 0
	}
	v := m * (5 + m/20)
	if d < 0 {
		return -v
	}
	return v
}

// panCursor is the cursor for the pointer's place relative to the press point.
func (w *Window) panCursor() pancursor.Kind {
	a := w.autoScroll
	var vx, vy float32
	if a.canX {
		vx = autoScrollSpeed(w.mouseX - a.x)
	}
	if a.canY {
		vy = autoScrollSpeed(w.mouseY - a.y)
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
	win.SetCursor(panCursorHandle(a.cursor, size))
}

var panCursors struct {
	sync.Mutex
	m map[[2]int]win.HANDLE
}

// panCursorHandle returns the cursor c at size pixels, made on first use and
// kept for the life of the process.
func panCursorHandle(c pancursor.Kind, size int) win.HANDLE {
	panCursors.Lock()
	defer panCursors.Unlock()
	key := [2]int{int(c), size}
	if h, ok := panCursors.m[key]; ok {
		return h
	}
	h := win.CreateCursorFromBGRA(pancursor.Pixels(c, size), size, size, size/2, size/2)
	if h == 0 {
		h = win.LoadCursor(win.IDC_SIZEALL)
	}
	if panCursors.m == nil {
		panCursors.m = map[[2]int]win.HANDLE{}
	}
	panCursors.m[key] = h
	return h
}
