package fairy

import (
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/win"
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
	if !h.clip.contains(cx, cy) {
		return false
	}
	px, py := x, y
	if h.turned {
		px, py = h.inv.apply(x, y)
	}
	return (rectF{h.x, h.y, h.w, h.h}).containsRounded(px, py, h.radii)
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
	track    rectF
	thumb    rectF
	vis      rectF
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
		hitArea := rectF{b.track.x - 3, b.track.y - 3, b.track.w + 6, b.track.h + 6}
		if b.vis.contains(x, y) && hitArea.contains(x, y) && !w.coveredFrom(b.above, x, y) {
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
	if !w.tracking && !w.ncHover {
		t := win.TRACKMOUSEEVENT{Size: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), Flags: win.TME_LEAVE, Hwnd: w.hwnd}
		win.TrackMouseEvent(&t)
		w.tracking = true
	}
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
				slop = touchSlop
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
			e.onDrag(w.dragEvent(e, p, phase, x, y))
			w.Update()
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
			w.markDirty(dirtyPointer)
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
	var path []*El
	for e := top; e != nil && (!chrome || e.inChrome); e = e.parent {
		path = append(path, e)
	}
	return w.setHoverPath(path)
}

func (w *Window) setHoverPath(path []*El) bool {
	ids := make(map[uint64]bool, len(path))
	for _, e := range path {
		ids[e.id] = true
	}
	changed := len(ids) != len(w.hoverIDs)
	for id := range w.hoverIDs {
		if !ids[id] {
			changed = true
			if e := w.byID[id]; e != nil && e.onHover != nil {
				e.onHover(false)
				w.markDirty(dirtyHover)
			}
		}
	}
	for _, e := range path {
		if !w.hoverIDs[e.id] && e.onHover != nil {
			e.onHover(true)
			w.markDirty(dirtyHover)
		}
	}
	w.hoverPath, w.hoverIDs = path, ids
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
					win.SetTimer(w.hwnd, timerTip, delay)
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
	if w.autoScroll != nil {
		// Any press ends autoscroll and does nothing else.
		w.stopAutoScroll()
		return
	}
	if w.press != nil {
		return
	}
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
		inside := rectF{r.absX, r.absY, r.w, r.h}.contains(x, y)
		if !inside && !l.pop.opts.DismissOnAnchorPress {
			if a := w.byID[l.anchor]; a != nil {
				inside = rectF{a.absX, a.absY, a.w, a.h}.contains(x, y)
			}
		}
		if inside {
			break
		}
		if l.onDismiss != nil {
			l.onDismiss()
			w.markDirty(dirtyDismiss)
		}
	}
	w.mouseX, w.mouseY = x, y

	now := uint32(time.Now().UnixMilli())
	dx, dy := x-w.lastClickX, y-w.lastClickY
	near := float32(4)
	if w.touchPress {
		near = touchSlop
	}
	if now-w.lastClickAt <= win.GetDoubleClickTime() && dx*dx+dy*dy <= near*near {
		w.clickCount++
	} else {
		w.clickCount = 1
	}
	w.lastClickAt, w.lastClickX, w.lastClickY = now, x, y

	if b := w.barAt(x, y); b != nil && button == 0 && !chrome {
		if e := w.byID[b.el]; e != nil && e.state != nil {
			bar := *b
			p := &pressState{button: 0, x: x, y: y, bar: &bar}
			if !bar.thumb.contains(x, y) {
				// Jump so the thumb centers on the pointer, then drag from there.
				w.scrollBarJump(e, &bar, x, y)
			}
			if bar.vertical {
				p.barStart = e.state.scrollY
			} else {
				p.barStart = e.state.scrollX
			}
			w.press = p
			win.SetCapture(w.hwnd)
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
			w.markDirty(dirtyPointer)
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
	win.SetCapture(w.hwnd)
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
	win.ReleaseCapture()
	if p.bar != nil {
		w.invalidate()
		return
	}
	if p.dragging {
		if e := w.byID[p.drag]; e != nil && e.onDrag != nil {
			e.onDrag(w.dragEvent(e, p, DragEnd, x, y))
			w.Update()
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
			e.onDrag(ev)
			w.Update()
		}
	}
	w.invalidate()
}
