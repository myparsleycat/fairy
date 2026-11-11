package fairy

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func newPointerWindow(t *testing.T, view func() *El) *Window {
	w := newListWindow(t, view)
	w.tracking = true // keeps onMouseMove from arming TrackMouseEvent on the missing window
	w.build()
	w.placeAll()
	return w
}

func TestPopoverAnchorPress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     PopoverOptions
		toggle   bool
		wantOpen bool
	}{
		{
			name: "context menu",
			opts: PopoverOptions{AtPoint: true, X: 40, Y: 10, Side: SideRight, DismissOnAnchorPress: true},
		},
		{name: "dropdown toggle", toggle: true},
		{
			name:     "point placement with default dismissal",
			opts:     PopoverOptions{AtPoint: true, X: 40, Y: 10, Side: SideRight},
			wantOpen: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			open := false
			clicks := 0
			opts.OnDismiss = func() { open = false }
			w := newPointerWindow(t, func() *El {
				anchor := Box().Size(100, 30).OnClick(func() {
					clicks++
					if tc.toggle {
						open = !open
					}
				}).OnRightClick(func(PointerEvent) { open = true })
				if open {
					anchor.Popover(Box().Size(80, 30), opts)
				}
				return Col(anchor)
			})
			button := 1
			if tc.toggle {
				button = 0
			}
			w.onButtonDown(40, 10, button)
			w.onButtonUp(40, 10, button)
			w.build()
			w.placeAll()
			if !open {
				t.Fatal("opening click did not open the popover")
			}
			clicksBefore := clicks
			w.onButtonDown(10, 10, 0)
			w.build()
			w.placeAll()
			w.onButtonUp(10, 10, 0)
			if open != tc.wantOpen {
				t.Errorf("popover open after anchor click = %v, want %v", open, tc.wantOpen)
			}
			if clicks != clicksBefore+1 {
				t.Error("left click did not reach the anchor exactly once")
			}
		})
	}
}

// An OnWheel handler that takes the wheel stops the scroll container around
// it from scrolling; one that declines passes it on.
func TestWheelHandler(t *testing.T) {
	handled := true
	var got []WheelEvent
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 60).Key("view").OnWheel(func(ev WheelEvent) bool {
				got = append(got, ev)
				return handled
			}),
			Box().Size(100, 300),
		).Size(200, 100).ScrollY().Key("scroller")
	})
	scroller := w.root
	m := keyboard.ModCtrl
	w.scriptMods = &m

	w.onWheel(30, 20, -0.05, false, true) // a touchpad step towards the user
	if len(got) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.DY != 0.05 || ev.DX != 0 || ev.LocalX != 30 || ev.LocalY != 20 || ev.W != 100 || ev.H != 60 || ev.Mods != keyboard.ModCtrl || !ev.Precise {
		t.Fatalf("event = %+v", ev)
	}
	if scroller.state.scrollY != 0 {
		t.Fatalf("scrolled to %v although the handler took the wheel", scroller.state.scrollY)
	}

	handled = false
	w.onWheel(30, 20, -1, false, true)
	if scroller.state.scrollY <= 0 {
		t.Fatal("a declined wheel did not reach the scroll container")
	}

	// A tilt to the right is DX > 0.
	w.onWheel(30, 20, -0.5, true, true)
	if ev := got[len(got)-1]; ev.DX != 0.5 || ev.DY != 0 {
		t.Fatalf("tilt event = %+v", ev)
	}
}

type dragLog struct {
	drags  []DragEvent
	rights int
}

func dragTarget(l *dragLog, buttons MouseButtons) *El {
	return Box().Size(100, 60).DragButtons(buttons).
		OnDrag(func(d DragEvent) { l.drags = append(l.drags, d) }).
		OnRightClick(func(PointerEvent) { l.rights++ })
}

// Right and middle drags start only on elements that ask for them; a right
// press that does not move far enough is still a right click.
func TestButtonDrags(t *testing.T) {
	var l dragLog
	buttons := MouseLeft | MouseRight | MouseMiddle
	w := newPointerWindow(t, func() *El { return Col(dragTarget(&l, buttons)) })
	press := func(b int, path ...[2]float32) {
		w.onMouseMove(path[0][0], path[0][1])
		w.onButtonDown(path[0][0], path[0][1], b)
		for _, p := range path[1:] {
			w.onMouseMove(p[0], p[1])
		}
		last := path[len(path)-1]
		w.onButtonUp(last[0], last[1], b)
	}

	press(1, [2]float32{10, 10}, [2]float32{11, 11})
	if l.rights != 1 || len(l.drags) != 0 {
		t.Fatalf("right click without moving: %d right clicks, %d drag events", l.rights, len(l.drags))
	}

	press(1, [2]float32{10, 10}, [2]float32{20, 10}, [2]float32{30, 15})
	if l.rights != 1 {
		t.Fatal("a right drag also ran OnRightClick")
	}
	if len(l.drags) != 3 || l.drags[0].Phase != DragStart || l.drags[2].Phase != DragEnd {
		t.Fatalf("right drag events = %+v", l.drags)
	}
	for _, d := range l.drags {
		if d.Button != 1 {
			t.Fatalf("right drag event button = %d", d.Button)
		}
	}
	if d := l.drags[2]; d.DX != 20 || d.DY != 5 || d.Canceled {
		t.Fatalf("right drag end = %+v", d)
	}

	l.drags = nil
	m := keyboard.ModShift
	w.scriptMods = &m
	press(2, [2]float32{10, 10}, [2]float32{10, 30})
	w.scriptMods = nil
	if len(l.drags) != 2 || l.drags[0].Button != 2 || l.drags[0].Mods != keyboard.ModShift {
		t.Fatalf("middle drag events = %+v", l.drags)
	}

	// Escape cancels a right drag like a left one.
	l.drags = nil
	w.onMouseMove(10, 10)
	w.onButtonDown(10, 10, 1)
	w.onMouseMove(40, 10)
	w.onKeyDown(keyboard.KeyEscape, false, "")
	if w.press != nil {
		t.Fatal("Escape left the press active")
	}
	if n := len(l.drags); n != 2 || !l.drags[1].Canceled || l.drags[1].Button != 1 {
		t.Fatalf("canceled right drag events = %+v", l.drags)
	}
	w.onButtonUp(40, 10, 1)
	if l.rights != 1 {
		t.Fatal("releasing after a canceled right drag ran OnRightClick")
	}
}

// Without DragButtons, only the left button drags, as before.
func TestDragDefaultsToLeft(t *testing.T) {
	var l dragLog
	w := newPointerWindow(t, func() *El { return Col(dragTarget(&l, 0)) })
	for _, b := range []int{1, 2} {
		w.onMouseMove(10, 10)
		w.onButtonDown(10, 10, b)
		w.onMouseMove(40, 10)
		w.onButtonUp(40, 10, b)
	}
	if len(l.drags) != 0 {
		t.Fatalf("right and middle presses dragged: %+v", l.drags)
	}
	if l.rights != 1 {
		t.Fatalf("right clicks = %d, want 1", l.rights)
	}
	w.onMouseMove(10, 10)
	w.onButtonDown(10, 10, 0)
	w.onMouseMove(40, 10)
	w.onButtonUp(40, 10, 0)
	if len(l.drags) != 2 || l.drags[0].Button != 0 {
		t.Fatalf("left drag events = %+v", l.drags)
	}
}

// A middle press on a scroll container autoscrolls it toward the pointer.
// Releasing after a move ends it; releasing in place keeps it on until the
// next press. An element that drags with the middle button keeps its drag.
func TestMiddleAutoScroll(t *testing.T) {
	var l dragLog
	w := newPointerWindow(t, func() *El {
		return Col(
			dragTarget(&l, MouseMiddle),
			Box().Size(100, 400),
		).Size(200, 100).ScrollY()
	})
	scroller := w.root
	frames := func(n int) {
		for range n {
			w.stepAutoScroll(1.0 / 60)
		}
	}

	w.onMouseMove(10, 30)
	w.onButtonDown(10, 30, 2)
	w.onMouseMove(10, 40)
	w.onButtonUp(10, 40, 2)
	if w.autoScroll != nil || len(l.drags) != 2 || scroller.state.scrollY != 0 {
		t.Fatalf("middle drag target: autoscroll %v, drags %+v, scrollY %v", w.autoScroll != nil, l.drags, scroller.state.scrollY)
	}

	// Held: the view follows the pointer and the release ends it.
	w.onMouseMove(150, 50)
	w.onButtonDown(150, 50, 2)
	frames(10)
	if scroller.state.scrollY != 0 {
		t.Fatalf("scrolled to %v with the pointer on the press point", scroller.state.scrollY)
	}
	w.onMouseMove(150, 90)
	frames(10)
	down := scroller.state.scrollY
	if down <= 0 {
		t.Fatal("pointer below the press point did not scroll down")
	}
	w.onMouseMove(150, 10)
	frames(5)
	if scroller.state.scrollY >= down {
		t.Fatal("pointer above the press point did not scroll up")
	}
	w.onButtonUp(150, 10, 2)
	if w.autoScroll != nil {
		t.Fatal("release after moving left autoscroll on")
	}

	// Clicked in place: it stays on after the release; the next press ends it
	// without reaching anything else.
	w.onMouseMove(150, 50)
	w.onButtonDown(150, 50, 2)
	w.onButtonUp(150, 50, 2)
	if w.autoScroll == nil {
		t.Fatal("middle click in place did not keep autoscroll on")
	}
	w.onMouseMove(150, 90)
	from := scroller.state.scrollY
	frames(5)
	if scroller.state.scrollY <= from {
		t.Fatal("autoscroll after the release did not scroll")
	}
	w.onButtonDown(150, 90, 0)
	w.onButtonUp(150, 90, 0)
	if w.autoScroll != nil || w.press != nil {
		t.Fatal("a left press did not end autoscroll on its own")
	}
}

// Pointer moves reach OnPointerMove once per frame, only while no button is held.
func TestPointerMove(t *testing.T) {
	var moves []PointerEvent
	changed := false
	w := newPointerWindow(t, func() *El {
		return Col(
			Box().Size(100, 40),
			Box().Size(100, 50).OnPointerMove(func(p PointerEvent) bool { moves = append(moves, p); return changed }),
		)
	})

	w.onMouseMove(10, 10)
	w.flushPointerMove()
	if len(moves) != 0 || w.hoverMoves {
		t.Fatal("a move outside the element reached OnPointerMove")
	}

	w.onMouseMove(10, 50)
	w.onMouseMove(12, 55)
	w.dirty = false
	w.flushPointerMove()
	w.flushPointerMove()
	if len(moves) != 1 {
		t.Fatalf("moves in one frame = %d, want 1", len(moves))
	}
	if w.dirty {
		t.Fatal("a move whose handler changed nothing rebuilds the view")
	}
	if p := moves[0]; p.X != 12 || p.Y != 55 || p.LocalX != 12 || p.LocalY != 15 || p.Button != -1 {
		t.Fatalf("move = %+v", p)
	}

	w.onButtonDown(12, 55, 0)
	w.onMouseMove(20, 60)
	w.flushPointerMove()
	if len(moves) != 1 {
		t.Fatal("a move with a button held reached OnPointerMove")
	}
	// The release reports where the pointer ended up.
	changed = true
	w.onButtonUp(20, 60, 0)
	w.dirty = false
	w.flushPointerMove()
	if !w.dirty {
		t.Fatal("a move whose handler changed the view does not rebuild it")
	}
	if len(moves) != 2 || moves[1].LocalX != 20 || moves[1].LocalY != 20 {
		t.Fatalf("moves after the release = %+v", moves)
	}
}

// A press outside a rounded corner misses the element there.
func TestHitTestRoundedCorner(t *testing.T) {
	var round *El
	w := newPointerWindow(t, func() *El {
		round = Box().Size(40, 40).Radius(20).OnClick(func() {})
		return Col(round)
	})
	x, y := round.absX, round.absY
	for _, tc := range []struct {
		name   string
		dx, dy float32
		hit    bool
	}{
		{"center", 20, 20, true},
		{"top-left corner", 2, 2, false},
		{"top-right corner", 38, 2, false},
		{"bottom-right corner", 38, 38, false},
		{"bottom-left corner", 2, 38, false},
		{"edge midpoint", 20, 1, true},
	} {
		if got := w.hitTest(x+tc.dx, y+tc.dy) == round; got != tc.hit {
			t.Errorf("%s: hit = %v, want %v", tc.name, got, tc.hit)
		}
	}
}
