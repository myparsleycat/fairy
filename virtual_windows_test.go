package fairy

import (
	"strconv"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/motion"
)

// newListWindow returns a window without a native window or renderer that
// shows view at 200x100, enough to build, lay out and place the tree.
func newListWindow(t *testing.T, view func() *El) *Window {
	ts, err := text.NewSystem(&render.Atlas{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Release)
	return &Window{
		opts:      Options{View: func(*Cx) *El { return view() }, FontSize: 14, Font: "Segoe UI"},
		states:    map[uint64]*elState{},
		hoverIDs:  map[uint64]bool{},
		byID:      map[uint64]*El{},
		prevByID:  map[uint64]*El{},
		locals:    map[string]*localEntry{},
		ts:        ts,
		width:     200,
		height:    100,
		scale:     1,
		dirty:     true,
		needPaint: true, // keeps invalidate from reaching the missing native window
	}
}

func TestConcurrentViewsKeepTheirBuildContext(t *testing.T) {
	started, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first *Window
	first = newListWindow(t, func() *El {
		value := Local[int]("value")
		*value = 1
		close(started)
		<-resume
		if CurrentWindow() != first || Local[int]("value") != value {
			t.Error("first view lost its build context after the second view")
		}
		return Box()
	})
	var second *Window
	second = newListWindow(t, func() *El {
		if CurrentWindow() != second || *Local[int]("value") != 0 {
			t.Error("second view did not get its own build context")
		}
		return Box()
	})
	go func() {
		defer close(finished)
		first.build()
	}()
	<-started
	second.build()
	close(resume)
	<-finished
}

// settle runs 60 Hz frames until a measured list stops moving its rows and
// any glide has ended.
func settle(t *testing.T, w *Window) {
	t.Helper()
	for range 600 {
		if w.dirty {
			w.build()
		}
		w.dt = 1.0 / 60
		w.placeAll()
		w.dt = 0
		s := w.root.state
		y, h := s.scrollY, w.root.node.ContentH
		w.placeAll()
		if !w.dirty && s.scrollY == y && w.root.node.ContentH == h && !s.revealing && !s.smooth {
			return
		}
	}
	t.Fatal("list did not settle")
}

// rowSpan returns where row i is drawn relative to the top of the view.
func rowSpan(t *testing.T, w *Window, i int) (top, bottom float32) {
	t.Helper()
	for _, c := range w.root.children {
		if c.key == strconv.Itoa(i) {
			top = c.absY - w.root.absY
			return top, top + c.h
		}
	}
	t.Fatalf("row %d is not built", i)
	return
}

func measuredRowHeight(i int) float32 { return float32(20 + i*37%61) }

// A row revealed in a measured list ends up against the edge of the view it
// came in from once the rows around it have been measured, though their
// estimated heights put it elsewhere.
func TestScrollToRowMeasured(t *testing.T) {
	w := newListWindow(t, func() *El {
		return VirtualListMeasured(1000, 50, strconv.Itoa, func(i int) *El {
			return Box().H(measuredRowHeight(i))
		}).Key("list")
	})
	settle(t, w)

	for _, c := range []struct {
		row     int
		fromTop bool
	}{{700, false}, {690, true}, {5, true}, {999, false}} {
		w.ScrollToRow("list", c.row)
		settle(t, w)
		top, bottom := rowSpan(t, w, c.row)
		if c.fromTop && top != 0 || !c.fromTop && bottom != 100 {
			t.Fatalf("row %d drawn at %v..%v, want it against the %s of 0..100", c.row, top, bottom,
				map[bool]string{true: "top", false: "bottom"}[c.fromTop])
		}
	}
}

// A reveal far down a measured list glides only the last two views, and
// ScrollToBottom ends with the last row against the bottom.
func TestScrollGlideMeasured(t *testing.T) {
	w := newListWindow(t, func() *El {
		return VirtualListMeasured(1000, 50, strconv.Itoa, func(i int) *El {
			return Box().H(measuredRowHeight(i))
		}).Key("list")
	})
	settle(t, w)

	w.ScrollToRow("list", 900)
	w.dt = 1.0 / 60
	w.placeAll()
	s := w.root.state
	if !s.glide || s.scrollTY-s.scrollY > glideReach*100+0.5 {
		t.Fatalf("glide starts at %v toward %v (gliding %v), want within %d views", s.scrollY, s.scrollTY, s.glide, glideReach)
	}
	settle(t, w)
	if _, bottom := rowSpan(t, w, 900); bottom != 100 {
		t.Fatalf("row 900 ends at %v, want 100", bottom)
	}

	w.ScrollToRow("list", 0)
	settle(t, w)
	w.ScrollToBottom("list")
	settle(t, w)
	if _, bottom := rowSpan(t, w, 999); bottom != 100 {
		t.Fatalf("after ScrollToBottom the last row ends at %v, want 100", bottom)
	}
}

// A list that sticks to the bottom keeps its last row at the bottom as rows of
// unknown height are added, until it is scrolled up.
func TestStickToBottomMeasured(t *testing.T) {
	count := 3
	w := newListWindow(t, func() *El {
		return VirtualListMeasured(count, 50, strconv.Itoa, func(i int) *El {
			return Box().H(measuredRowHeight(i))
		}).StickToBottom()
	})
	settle(t, w)

	atBottom := func(when string) {
		t.Helper()
		if _, bottom := rowSpan(t, w, count-1); bottom != 100 {
			t.Fatalf("%s: last row ends at %v, want 100", when, bottom)
		}
	}
	for _, n := range []int{10, 11, 40} {
		count = n
		w.dirty = true
		settle(t, w)
		atBottom("after growing to " + strconv.Itoa(n))
	}

	w.scrollNow(w.root, 0, -30)
	settle(t, w)
	y := w.root.state.scrollY
	count = 60
	w.dirty = true
	settle(t, w)
	if got := w.root.state.scrollY; got != y {
		t.Fatalf("scrolled up: offset moved from %v to %v", y, got)
	}

	w.scrollNow(w.root, 0, 1e6)
	settle(t, w)
	count = 70
	w.dirty = true
	settle(t, w)
	atBottom("after scrolling back to the bottom")
}

// With AnimateRowHeights, a row in view that grows, as a streamed message does,
// is shown growing over several frames while a list stuck to the bottom keeps
// its end in view.
func TestAnimateRowHeightsMeasured(t *testing.T) {
	last := float32(30)
	w := newListWindow(t, func() *El {
		return VirtualListMeasured(5, 30, strconv.Itoa, func(i int) *El {
			h := float32(30)
			if i == 4 {
				h = last
			}
			return Box().H(h)
		}).StickToBottom().AnimateRowHeights(motion.Ease(0.2))
	})
	settle(t, w)
	w.scrollNow(w.root, 0, 1e6)
	settle(t, w)
	frame := func(dt float32) {
		if w.dirty {
			w.build()
		}
		w.dt = dt
		w.placeAll()
	}
	last = 90
	w.dirty = true
	frame(0)
	frame(0.1)
	top, bottom := rowSpan(t, w, 4)
	if h := bottom - top; h <= 30 || h >= 90 {
		t.Fatalf("growing row shown %v tall halfway, want between 30 and 90", h)
	}
	if bottom != 100 {
		t.Fatalf("halfway: last row ends at %v, want the bottom of the view at 100", bottom)
	}
	frame(0.1)
	frame(0)
	if top, bottom := rowSpan(t, w, 4); bottom-top != 90 || bottom != 100 {
		t.Fatalf("after growing: last row spans %v to %v, want 90 tall ending at 100", top, bottom)
	}
}

// A popover opened from a row is laid out and placed like any other, and keeps
// one layer across rebuilds.
func TestPopoverInVirtualRow(t *testing.T) {
	w := newListWindow(t, func() *El {
		return VirtualList(20, 20, func(i int) *El {
			row := Box().H(20)
			if i == 2 {
				row.Popover(Col(Box().W(60).H(30)), PopoverOptions{})
			}
			return row
		})
	})
	for range 2 {
		w.dirty = true
		settle(t, w)
		if len(w.layers) != 1 {
			t.Fatalf("%d layers, want 1", len(w.layers))
		}
		if l := w.layers[0]; l.hidden || l.root.node.W != 60 || l.root.node.H != 30 {
			t.Fatalf("popover hidden %v at %vx%v, want shown at 60x30", l.hidden, l.root.node.W, l.root.node.H)
		}
	}
}

func TestVirtualRowsRemoveDepartedDescendants(t *testing.T) {
	w := newListWindow(t, func() *El {
		return VirtualList(10000, 20, func(i int) *El {
			return Box(Box(Box().Size(10, 10).Focusable().Key("child"))).H(20)
		})
	})
	w.build()
	w.placeAll()
	for step := range 100 {
		w.scrollNow(w.root, 0, 200)
		w.placeAll()
		current := map[uint64]*El{}
		var walk func(*El)
		walk = func(e *El) {
			current[e.id] = e
			for _, c := range e.children {
				walk(c)
			}
		}
		walk(w.root)
		if len(w.byID) != len(current) {
			t.Fatalf("scroll %d: %d indexed elements for %d current elements", step, len(w.byID), len(current))
		}
		for id, e := range current {
			if w.byID[id] != e {
				t.Fatalf("scroll %d: current element %d is not indexed", step, id)
			}
		}
	}
}

func TestMeasuredListStopsHeightAnimation(t *testing.T) {
	last, animate := float32(30), true
	w := newListWindow(t, func() *El {
		e := VirtualListMeasured(5, 30, nil, func(i int) *El {
			h := float32(30)
			if i == 4 {
				h = last
			}
			return Box().H(h)
		}).StickToBottom()
		if animate {
			e.AnimateRowHeights(motion.Ease(0.2))
		}
		return e
	})
	settle(t, w)
	w.scrollNow(w.root, 0, 1000)
	settle(t, w)
	last = 90
	w.build()
	w.dt = 0.05
	w.placeAll()
	if h := w.root.state.rows.shown[4]; h <= 30 || h >= last {
		t.Fatalf("intermediate height %v, want between 30 and %v", h, last)
	}
	animate = false
	w.build()
	w.dt = 0
	w.placeAll()
	if got := w.root.node.ContentH; got != 210 {
		t.Fatalf("content height after disabling animation = %v, want 210", got)
	}
}
