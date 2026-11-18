//go:build windows

package fairy

import (
	"slices"
	"time"

	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/gesture"
	"github.com/myparsleycat/fairy/internal/scroll"
)

// hitTest returns the topmost element under (x, y).
func (w *Window) hitTest(x, y float32) *El {
	for i := len(w.hits) - 1; i >= 0; i-- {
		h := &w.hits[i]
		if h.contains(x, y) {
			return h.el
		}
	}
	return nil
}

func (h *hit) contains(x, y float32) bool {
	if h.el.passThrough {
		return false
	}
	cx, cy := x, y
	if h.clipTurned {
		cx, cy = h.clipInv.apply(x, y)
	}
	if !h.clip.Contains(cx, cy) {
		return false
	}
	px, py := x, y
	if h.turned {
		px, py = h.inv.apply(x, y)
	}
	return (geom.Rect{X: h.x, Y: h.y, W: h.w, H: h.h}).ContainsRounded(px, py, h.radii)
}

func (w *Window) chromeHitTest(x, y float32) *El {
	for i := len(w.chromeHits) - 1; i >= 0; i-- {
		h := &w.hits[w.chromeHits[i]]
		if !h.contains(x, y) {
			continue
		}
		// An exclusion is a hole in the whole priority region, including its
		// ancestors, so fall back to normal layers instead of trying them next.
		if h.el.excludeChrome {
			return nil
		}
		return h.el
	}
	return nil
}

func (w *Window) pointerHitTest(x, y float32) *El {
	if e := w.chromeHitTest(x, y); e != nil {
		return e
	}
	return w.hitTest(x, y)
}

type pressState struct {
	el       uint64 // element that receives click/right-click
	drag     uint64 // element that receives drag events
	input    uint64 // input whose selection follows the pointer
	button   int
	x, y     float32
	clicks   int
	dragging bool
	// inputMenu: a right press on an Input without OnRightClick opens its edit menu.
	inputMenu bool
	sel       bool // the press started a selection of read-only text
	touch     bool // a finger, not the mouse, holds the press
	bar       *scrollBar
	barStart  float32
	cursor    Cursor
}

type scrollBar struct {
	el       uint64
	track    geom.Rect
	thumb    geom.Rect
	vis      geom.Rect
	vertical bool
	// above is the index in hits where the elements placed after the scroll container's subtree start; the bar
	// is drawn over its own content but under those.
	above int
}

// barAt returns the scroll bar whose track, widened for an easier grab, is at (x, y), unless an element placed
// above it, such as a dialog or another layer, covers that point.
func (w *Window) barAt(x, y float32) *scrollBar {
	for i := len(w.bars) - 1; i >= 0; i-- {
		b := &w.bars[i]
		hitArea := geom.Rect{X: b.track.X - 3, Y: b.track.Y - 3, W: b.track.W + 6, H: b.track.H + 6}
		if b.vis.Contains(x, y) && hitArea.Contains(x, y) && !w.coveredFrom(b.above, x, y) {
			return b
		}
	}
	return nil
}

func (w *Window) coveredFrom(start int, x, y float32) bool {
	for i := start; i < len(w.hits); i++ {
		if w.hits[i].contains(x, y) {
			return true
		}
	}
	return false
}

func (w *Window) onMouseMove(x, y float32) {
	w.mouseX, w.mouseY, w.mouseIn = x, y, true
	w.trackMouseLeave()
	if w.autoScroll != nil {
		w.autoScrollMove(x, y)
		return
	}
	if p := w.press; p != nil {
		switch {
		case p.bar != nil:
			w.dragScrollBar(p, x, y)
			return
		case p.input != 0:
			if e := w.byID[p.input]; e != nil {
				w.moveCaretTo(e, x, y, true)
			}
			return
		case p.sel:
			if dx, dy := x-p.x, y-p.y; dx*dx+dy*dy >= 9 {
				p.el = 0 // a drag that selects text is not a click
			}
			w.dragSelection(x, y)
			return
		case p.drag != 0:
			dx, dy := x-p.x, y-p.y
			slop := float32(3)
			if p.touch {
				slop = gesture.Slop
			}
			if !p.dragging && dx*dx+dy*dy < slop*slop {
				break
			}
			e := w.byID[p.drag]
			if e == nil || e.onDrag == nil {
				break
			}
			phase := DragMove
			if !p.dragging {
				p.dragging = true
				phase = DragStart
			}
			if e.onDrag(w.dragEvent(e, p, phase, x, y)) {
				w.Update()
			} else {
				w.invalidate()
			}
			return
		}
	}
	if w.updateHover(x, y) {
		w.invalidate()
	}
	w.queuePointerMove()
}

// queuePointerMove has the next frame report the pointer to OnPointerMove
// handlers under it. A release queues one too: moves made while a button
// was held were not reported.
func (w *Window) queuePointerMove() {
	if w.hoverMoves && w.press == nil {
		w.moveDue = true
		w.invalidate()
	}
}

// flushPointerMove sends the pointer position to the OnPointerMove handlers
// under it, once per frame however many moves arrived since the last one.
func (w *Window) flushPointerMove() {
	if !w.moveDue {
		return
	}
	w.moveDue = false
	if w.press != nil || !w.mouseIn {
		return
	}
	x, y := w.mouseX, w.mouseY
	busy := w.hoverBusy
	w.hoverBusy = true
	defer func() { w.hoverBusy = busy }()
	for _, e := range w.hoverPath {
		if e.onPointerMove != nil &&
			e.onPointerMove(
				PointerEvent{
					X:      x,
					Y:      y,
					LocalX: x - e.absX,
					LocalY: y - e.absY,
					W:      e.w,
					H:      e.h,
					Button: -1,
					Mods:   w.mods(),
				},
			) {
			w.markDirty(frameprof.CausePointer)
		}
	}
}

func (w *Window) dragEvent(e *El, p *pressState, phase DragPhase, x, y float32) DragEvent {
	return DragEvent{Phase: phase, X: x, Y: y, DX: x - p.x, DY: y - p.y, LocalX: x - e.absX, LocalY: y - e.absY,
		W: e.w, H: e.h, Button: p.button, Mods: w.mods()}
}

func (w *Window) onMouseLeave() {
	w.mouseIn = false
	w.hideTooltip()
	if w.press != nil {
		return
	}
	if w.setHoverPath(nil) {
		w.invalidate()
	}
}

// updateHover resolves the hovered path at (x, y) and fires OnHover handlers.
// It reports whether the path changed.
func (w *Window) updateHover(x, y float32) bool {
	top := w.pointerHitTest(x, y)
	chrome := top != nil && top.inChrome && !top.excludeChrome
	path := w.hoverScratch[:0]
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		path = append(path, e)
	}
	changed := w.setHoverPath(path)
	clear(path)
	w.hoverScratch = path[:0]
	return changed
}

// setHoverPath makes path the hovered path. It does not keep path.
func (w *Window) setHoverPath(path []*El) bool {
	changed := false
	if slices.EqualFunc(path, w.hoverPath, func(a, b *El) bool { return a.id == b.id }) {
		// The same elements, rebuilt: only the pointers change. While
		// flushPointerMove ranges over hoverPath, handlers that render a
		// frame synchronously must not change the slice it holds.
		if w.hoverBusy {
			w.hoverPath = slices.Clone(path)
		} else {
			copy(w.hoverPath, path)
		}
	} else {
		path = slices.Clone(path)
		ids := make(map[uint64]bool, len(path))
		for _, e := range path {
			ids[e.id] = true
		}
		changed = len(ids) != len(w.hoverIDs)
		for id := range w.hoverIDs {
			if !ids[id] {
				changed = true
				if e := w.byID[id]; e != nil && e.onHover != nil && e.onHover(false) {
					w.markDirty(frameprof.CauseHover)
				}
			}
		}
		for _, e := range path {
			if !w.hoverIDs[e.id] && e.onHover != nil && e.onHover(true) {
				w.markDirty(frameprof.CauseHover)
			}
		}
		w.hoverPath, w.hoverIDs = path, ids
	}
	w.hoverMoves = false
	for _, e := range path {
		if e.onPointerMove != nil {
			w.hoverMoves = true
			break
		}
	}
	if changed {
		focusPath := path
		if len(path) > 0 && path[0].inChrome && !path[0].excludeChrome {
			focusPath = nil
		}
		w.syncHoverFocus(focusPath)
	}
	if changed {
		w.hideTooltip()
		for _, e := range path {
			if e.tooltip != "" || e.tipTruncated {
				delay := uint32(600)
				if ts := tooltipStyle.Load(); ts != nil {
					delay = uint32(max(ts.Delay.Milliseconds(), 0))
				}
				if delay == 0 {
					w.showTooltip()
				} else {
					w.setTimer(timerTip, delay)
				}
				break
			}
		}
	}
	if w.dirty {
		w.invalidate()
	}
	return changed
}

func (w *Window) onButtonDown(x, y float32, button int) {
	w.hideTooltip()
	w.wheelLatch = scroll.Latch{}
	if w.autoScroll != nil {
		// Any press ends autoscroll and does nothing else.
		w.stopAutoScroll()
		return
	}
	if w.press != nil {
		return
	}
	// A left press in the content moves focus, which commits a composition.
	// Committing it first lets the press hit-test the view built with the
	// committed text, such as a submit button it enables.
	if button == 0 && w.chromeHitTest(x, y) == nil && w.barAt(x, y) == nil {
		w.commitCompositionOutside(x, y)
	}
	w.settleView()
	top := w.chromeHitTest(x, y)
	chrome := top != nil

	// Dismiss popovers from the top down until the press is inside one.
	for i := len(w.layers) - 1; !chrome && i >= 0; i-- {
		l := w.layers[i]
		if l.hidden || l.root.passThrough {
			continue
		}
		if l.pop == nil {
			break
		}
		r := l.root
		inside := geom.Rect{X: r.absX, Y: r.absY, W: r.w, H: r.h}.Contains(x, y)
		if !inside && !l.pop.opts.DismissOnAnchorPress {
			if a := w.byID[l.anchor]; a != nil {
				inside = geom.Rect{X: a.absX, Y: a.absY, W: a.w, H: a.h}.Contains(x, y)
			}
		}
		if inside {
			break
		}
		if l.onDismiss != nil {
			l.onDismiss()
			w.markDirty(frameprof.CauseDismiss)
		}
	}
	w.mouseX, w.mouseY = x, y

	now := uint32(time.Now().UnixMilli())
	dx, dy := x-w.lastClickX, y-w.lastClickY
	near := float32(4)
	if w.touchPress {
		near = gesture.Slop
	}
	if now-w.lastClickAt <= doubleClickMs() && dx*dx+dy*dy <= near*near {
		w.clickCount++
	} else {
		w.clickCount = 1
	}
	w.lastClickAt, w.lastClickX, w.lastClickY = now, x, y

	if b := w.barAt(x, y); b != nil && button == 0 && !chrome {
		if e := w.byID[b.el]; e != nil && e.state != nil {
			bar := *b
			p := &pressState{button: 0, x: x, y: y, bar: &bar}
			if !bar.thumb.Contains(x, y) {
				// Jump so the thumb centers on the pointer, then drag from there.
				w.scrollBarJump(e, &bar, x, y)
			}
			if bar.vertical {
				p.barStart = e.state.ScrollY
			} else {
				p.barStart = e.state.ScrollX
			}
			w.press = p
			w.setCapture()
			w.invalidate()
			return
		}
	}

	if top == nil {
		top = w.hitTest(x, y)
	}
	if !chrome {
		var focus *El
		for e := top; e != nil; e = e.parent {
			if e.focusable {
				focus = e
				break
			}
		}
		if button == 0 || focus != nil {
			w.setFocusBy(focus, false)
		}
	}

	pe := PointerEvent{X: x, Y: y, Button: button, Clicks: w.clickCount, Mods: w.mods()}
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		if e.onPointerDown != nil {
			pe.LocalX, pe.LocalY = x-e.absX, y-e.absY
			pe.W, pe.H = e.w, e.h
			e.onPointerDown(pe)
			w.markDirty(frameprof.CausePointer)
			break
		}
	}
	if !chrome && button == 2 && w.startAutoScroll(top, x, y) {
		if w.dirty {
			w.Update()
		}
		return
	}

	selecting := false
	if button == 0 && !chrome {
		inInput := false
		for e := top; e != nil; e = e.parent {
			if e.kind == kindInput {
				inInput = true
				break
			}
		}
		if inInput {
			w.ClearSelection()
		} else {
			selecting = w.pressSelection(top, x, y, w.clickCount)
		}
	}

	p := &pressState{button: button, x: x, y: y, clicks: w.clickCount, touch: w.touchPress}
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		if button == 0 && e.kind == kindInput {
			p.input = e.id
			w.pressInput(e, x, y, w.clickCount)
			break
		}
		if button == 0 && (e.onClick != nil || e.onDoubleClick != nil || e.dragsWith(0) || w.linkAt(e, x, y) != nil) {
			p.el = e.id
			break
		}
		if button == 1 && e.onRightClick != nil {
			p.el = e.id
			break
		}
		if button == 1 && e.kind == kindInput {
			p.el = e.id
			p.inputMenu = true
			break
		}
	}
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		if e.dragsWith(button) {
			p.drag = e.id
			p.cursor = e.cursor
			break
		}
	}
	p.sel = selecting && p.drag == 0 && p.input == 0
	w.press = p
	w.setCapture()
	if w.dirty {
		w.Update()
	} else {
		w.invalidate()
	}
}

func (w *Window) onButtonUp(x, y float32, button int) {
	if button == 2 && w.autoScrollRelease() {
		return
	}
	p := w.press
	if p == nil || p.button != button {
		return
	}
	w.press = nil
	w.releaseCapture()
	if p.bar != nil {
		w.invalidate()
		return
	}
	if p.dragging {
		if e := w.byID[p.drag]; e != nil && e.onDrag != nil {
			if e.onDrag(w.dragEvent(e, p, DragEnd, x, y)) {
				w.Update()
			}
		}
		w.updateHover(x, y)
		w.queuePointerMove()
		w.invalidate()
		return
	}
	if p.el != 0 {
		e := w.byID[p.el]
		inside := false
		for t := w.pointerHitTest(x, y); t != nil; t = t.parent {
			if t == e {
				inside = true
				break
			}
		}
		if e != nil && inside {
			switch button {
			case 0:
				if e.onClick != nil {
					e.onClick()
				}
				if sp := w.linkAt(e, x, y); sp != nil {
					sp.OnClick()
				}
				if p.clicks == 2 && e.onDoubleClick != nil {
					e.onDoubleClick()
				}
				w.Update()
			case 1:
				if p.inputMenu {
					w.showInputMenu(e, x, y)
				} else if e.onRightClick != nil {
					e.onRightClick(
						PointerEvent{
							X:      x,
							Y:      y,
							LocalX: x - e.absX,
							LocalY: y - e.absY,
							W:      e.w,
							H:      e.h,
							Button: 1,
							Clicks: 1,
							Mods:   w.mods(),
						},
					)
					w.Update()
				}
			}
		}
	}
	w.updateHover(x, y)
	w.queuePointerMove()
	w.invalidate()
}

func (w *Window) cancelPress() {
	p := w.press
	if p == nil {
		return
	}
	w.press = nil
	if p.dragging {
		if e := w.byID[p.drag]; e != nil && e.onDrag != nil {
			ev := w.dragEvent(e, p, DragEnd, w.mouseX, w.mouseY)
			ev.Canceled = true
			if e.onDrag(ev) {
				w.Update()
			}
		}
	}
	w.invalidate()
}

// Touch follows one contact, as browsers do on Windows. A touch on an element
// that drags with the left button, or on a scroll bar, is a left press held
// for as long as the finger stays down. Anywhere else it does nothing until it
// is recognized: lifted within gesture.Slop it is a left click, held for
// gesture.LongPress a right click, and moved further it pans the innermost
// scroll container that can move that way, then keeps going with inertia when
// lifted while still moving. Pen input stays with Windows, which turns it into
// mouse messages.

func (w *Window) onTouchDown(id uint32, x, y float32, at time.Time) {
	w.wheelLatch = scroll.Latch{}
	if w.touch != nil {
		// A new primary contact means the last one ended without its up
		// reaching the window.
		w.onTouchCancel(w.touch.ID)
	}
	w.hideTooltip()
	w.stopAutoScroll()
	t := gesture.New(id, x, y, at)
	w.touch = t
	for e := w.pointerHitTest(x, y); e != nil; e = e.parent {
		if e.state != nil && e.state.Fling {
			e.state.Fling = false
			t.SuppressTap = true
		}
	}
	if w.touchPresses(x, y) {
		t.Phase = gesture.Pressed
		w.touchButtonDown(x, y, 0)
		if w.press == nil {
			t.Phase = gesture.Done
		}
		return
	}
	w.setTimer(timerTouch, uint32(gesture.LongPress.Milliseconds()))
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
	if t == nil || t.ID != id {
		return
	}
	t.Record(x, y, at)
	switch t.Phase {
	case gesture.Pressed:
		w.onMouseMove(x, y)
	case gesture.Pending:
		if !t.PastSlop(x, y) {
			return
		}
		w.killTimer(timerTouch)
		if !w.startTouchPan(t, x-t.X0, y-t.Y0) {
			t.Phase = gesture.Done
			return
		}
		// The view starts following from here, so it does not jump by the slop.
		t.LX, t.LY = x, y
	case gesture.Panning:
		dx, dy := t.Follow(x, y)
		e := w.byID[t.Target]
		if e == nil || e.state == nil {
			t.Phase = gesture.Done
			return
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
func (w *Window) startTouchPan(t *gesture.Touch, dx, dy float32) bool {
	onlyX, onlyY := gesture.Axes(dx, dy)
	for e := w.pointerHitTest(t.X0, t.Y0); e != nil; e = e.parent {
		if e.state == nil || e.kind == kindInput {
			continue
		}
		s, n := e.state, &e.node
		maxX, maxY := n.ContentW-n.W, n.ContentH-n.H
		canX := e.style.ScrollX && maxX > 0 && !onlyY
		canY := e.style.ScrollY && maxY > 0 && !onlyX
		movesX := canX && (dx > 0 && s.ScrollX > 0 || dx < 0 && s.ScrollX < maxX)
		movesY := canY && (dy > 0 && s.ScrollY > 0 || dy < 0 && s.ScrollY < maxY)
		if movesX || movesY {
			t.Phase, t.Target, t.CanX, t.CanY = gesture.Panning, e.id, canX, canY
			return true
		}
	}
	return false
}

func (w *Window) onTouchUp(id uint32, x, y float32, at time.Time) {
	t := w.touch
	if t == nil || t.ID != id {
		return
	}
	if t.Phase == gesture.Panning {
		w.onTouchMove(id, x, y, at)
	}
	w.touch = nil
	w.killTimer(timerTouch)
	switch t.Phase {
	case gesture.Pressed:
		w.onButtonUp(x, y, 0)
	case gesture.Pending:
		if !t.SuppressTap {
			w.touchButtonDown(t.X0, t.Y0, 0)
			w.onButtonUp(t.X0, t.Y0, 0)
		}
	case gesture.Panning:
		if e := w.byID[t.Target]; e != nil && e.state != nil {
			vx, vy := t.ReleaseVelocity()
			if e.state.StartFling(-vx, -vy) {
				w.invalidate()
			}
		}
	}
	w.endTouchHover()
}

func (w *Window) onTouchCancel(id uint32) {
	t := w.touch
	if t == nil || t.ID != id {
		return
	}
	w.touch = nil
	w.killTimer(timerTouch)
	if t.Phase == gesture.Pressed && w.press != nil {
		w.cancelPress()
		w.releaseCapture()
	}
	w.endTouchHover()
}

// onTouchLongPress turns a contact held in place into a right click.
func (w *Window) onTouchLongPress() {
	t := w.touch
	if t == nil || t.Phase != gesture.Pending {
		return
	}
	t.Phase = gesture.Done
	if t.SuppressTap {
		return
	}
	w.touchButtonDown(t.X0, t.Y0, 1)
	w.onButtonUp(t.X0, t.Y0, 1)
}

// touchButtonDown is onButtonDown for a touch, which allows gesture.Slop of
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
