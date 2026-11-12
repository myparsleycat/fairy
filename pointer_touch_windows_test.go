package fairy

import (
	"testing"
	"time"

	"github.com/myparsleycat/fairy/internal/win"
)

// touchPath runs one touch contact through the points, 10 ms apart, and
// lifts it at the last one.
func touchPath(w *Window, path ...[2]float32) {
	at := time.Unix(0, 0)
	w.onTouchDown(1, path[0][0], path[0][1], at)
	for _, p := range path[1:] {
		at = at.Add(10 * time.Millisecond)
		w.onTouchMove(1, p[0], p[1], at)
	}
	last := path[len(path)-1]
	w.onTouchUp(1, last[0], last[1], at)
	w.placeAll()
}

// A touch lifted within the slop is a click; one that moves further pans the
// view under it instead, without clicking, and leaves no hover behind. Two
// taps a finger's width apart are a double click.
func TestTouchTapAndPan(t *testing.T) {
	clicks, doubles := 0, 0
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 40).OnClick(func() { clicks++ }).OnDoubleClick(func() { doubles++ }).Tooltip("tip"),
			Box().Size(100, 400),
		).Size(200, 100).ScrollY()
	})
	s := w.root.state

	touchPath(w, [2]float32{10, 10}, [2]float32{14, 13})
	if clicks != 1 || s.scrollY != 0 {
		t.Fatalf("tap: clicks %d, scrollY %v", clicks, s.scrollY)
	}
	if len(w.hoverPath) != 0 || w.mouseIn {
		t.Fatal("a tap left the element hovered")
	}

	// The view follows from where the slop was crossed, so it does not jump.
	at := time.Unix(0, 0)
	w.onTouchDown(1, 10, 30, at)
	w.onTouchMove(1, 10, 10, at.Add(10*time.Millisecond))
	w.onTouchMove(1, 10, -20, at.Add(20*time.Millisecond))
	if s.scrollY != 30 {
		t.Fatalf("pan scrolled to %v, want 30", s.scrollY)
	}
	// Lifted after holding still: no fling.
	w.onTouchUp(1, 10, -20, at.Add(200*time.Millisecond))
	if clicks != 1 || s.fling {
		t.Fatalf("pan: clicks %d, fling %v", clicks, s.fling)
	}

	s.scrollY = 0
	w.placeAll()
	touchPath(w, [2]float32{60, 20})
	touchPath(w, [2]float32{68, 24})
	if clicks != 3 || doubles != 1 {
		t.Fatalf("double tap: clicks %d, double clicks %d", clicks, doubles)
	}
}

// A pan drives the innermost container that can move its way and keeps it:
// a vertical pan over a horizontal scroller moves the page, and a list at its
// end leaves the next pan to the page.
func TestTouchPanPicksScroller(t *testing.T) {
	w := newPointerWindow(t, func() *El {
		return Col(
			Row(Box().Size(400, 40)).Size(200, 40).ScrollX().Key("row"),
			Col(Box().Size(100, 200)).Size(200, 50).ScrollY().Key("list"),
			Box().Size(100, 400),
		).Size(200, 100).ScrollY()
	})
	page, row, list := w.root.state, w.findKey("row").state, w.findKey("list").state

	touchPath(w, [2]float32{150, 20}, [2]float32{120, 22}, [2]float32{100, 22})
	if row.scrollX != 20 || page.scrollY != 0 {
		t.Fatalf("horizontal pan: row %v, page %v", row.scrollX, page.scrollY)
	}

	touchPath(w, [2]float32{100, 25}, [2]float32{102, 5}, [2]float32{102, -5})
	if row.scrollX != 20 || page.scrollY != 10 {
		t.Fatalf("vertical pan over the row: row %v, page %v", row.scrollX, page.scrollY)
	}
	page.scrollY = 0
	w.placeAll()

	touchPath(w, [2]float32{100, 85}, [2]float32{100, 65}, [2]float32{100, 45})
	if list.scrollY != 20 || page.scrollY != 0 {
		t.Fatalf("pan over the list: list %v, page %v", list.scrollY, page.scrollY)
	}

	list.scrollY = 150
	w.placeAll()
	touchPath(w, [2]float32{100, 85}, [2]float32{100, 65}, [2]float32{100, 45})
	if list.scrollY != 150 || page.scrollY != 20 {
		t.Fatalf("pan over the list at its end: list %v, page %v", list.scrollY, page.scrollY)
	}
}

// An element that drags with the left button takes the touch instead of the
// scroll container around it. A tap that wobbles is still a click, the mouse
// cursor does not move the drag, and a canceled contact cancels it.
func TestTouchDragTarget(t *testing.T) {
	var l dragLog
	clicks := 0
	w := newPointerWindow(t, func() *El {
		return Col(dragTarget(&l, 0).OnClick(func() { clicks++ }), Box().Size(100, 400)).Size(200, 100).ScrollY()
	})

	touchPath(w, [2]float32{10, 10}, [2]float32{15, 13}, [2]float32{16, 12})
	if clicks != 1 || len(l.drags) != 0 {
		t.Fatalf("wobbly tap: clicks %d, drag events %+v", clicks, l.drags)
	}

	at := time.Unix(0, 0)
	w.onTouchDown(1, 10, 10, at)
	w.onTouchMove(1, 10, 30, at.Add(10*time.Millisecond))
	w.handle(win.WM_MOUSEMOVE, 0, 150|10<<16) // the cursor, elsewhere
	w.onTouchMove(1, 10, 50, at.Add(20*time.Millisecond))
	w.onTouchUp(1, 10, 50, at.Add(20*time.Millisecond))
	w.placeAll()
	if len(l.drags) != 3 || l.drags[0].Phase != DragStart || l.drags[2].Phase != DragEnd || l.drags[1].DY != 40 ||
		l.drags[1].DX != 0 {
		t.Fatalf("drag events = %+v", l.drags)
	}
	if w.root.state.scrollY != 0 {
		t.Fatal("dragging also scrolled the container")
	}

	l.drags = nil
	w.onTouchDown(1, 10, 10, time.Unix(0, 0))
	w.onTouchMove(1, 10, 40, time.Unix(0, 0))
	w.onTouchCancel(1)
	if w.press != nil || w.touch != nil {
		t.Fatal("a canceled contact left its press active")
	}
	if len(l.drags) != 2 || !l.drags[1].Canceled {
		t.Fatalf("canceled drag events = %+v", l.drags)
	}
}

// A pan let go while moving coasts on, slowing down, and stops at the end of
// the content.
func TestTouchFling(t *testing.T) {
	w := newPointerWindow(t, func() *El {
		return Col(Box().Size(100, 600)).Size(200, 100).ScrollY()
	})
	s := w.root.state

	touchPath(w, [2]float32{10, 90}, [2]float32{10, 70}, [2]float32{10, 50}, [2]float32{10, 30}, [2]float32{10, 10})
	if s.scrollY != 60 || !s.fling {
		t.Fatalf("after a 2000 px/s pan: scrollY %v, fling %v", s.scrollY, s.fling)
	}
	var steps []float32
	for range 600 {
		w.dt = 1.0 / 60
		w.placeAll()
		steps = append(steps, s.scrollY)
		if !s.fling {
			break
		}
	}
	if s.fling || s.scrollY != 500 {
		t.Fatalf("fling ended at %v (still moving %v), want the end at 500", s.scrollY, s.fling)
	}
	if len(steps) < 3 || steps[1]-steps[0] >= steps[0]-60 {
		t.Fatalf("fling did not slow down: %v", steps)
	}
}

// A contact held in place is a right click, and lifting it does not click.
func TestTouchLongPress(t *testing.T) {
	clicks, rights := 0, 0
	w := newPointerWindow(t, func() *El {
		return Col(Box().Size(100, 40).OnClick(func() { clicks++ }).OnRightClick(func(PointerEvent) { rights++ }))
	})

	w.onTouchDown(1, 10, 10, time.Unix(0, 0))
	w.onTouchLongPress()
	w.onTouchUp(1, 10, 10, time.Unix(1, 0))
	if rights != 1 || clicks != 0 {
		t.Fatalf("long press: %d right clicks, %d clicks", rights, clicks)
	}
}
