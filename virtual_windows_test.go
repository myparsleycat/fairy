package fairy

import (
	"slices"
	"strconv"
	"testing"

	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/internal/scroll"
	"github.com/myparsleycat/fairy/internal/text"
	"github.com/myparsleycat/fairy/motion"
)

// Measured heights follow their keys when rows are inserted, removed or moved,
// so rows in view do not take the heights of the rows they replaced.
func TestRowHeightsFollowKeys(t *testing.T) {
	keys := []string{"a", "b", "c"}
	v := &virtualList{count: 3, estimate: 50, key: func(i int) string { return keys[i] }}
	r := &rowHeights{}
	r.sync(v)
	copy(r.h, []float32{10, 20, 30})
	r.stale = true

	keys = []string{"new", "c", "a"}
	r.sync(v)
	if want := []float32{-1, 30, 10}; !slices.Equal(r.h, want) {
		t.Fatalf("heights %v, want %v", r.h, want)
	}
	for i, want := range []float32{0, 50, 80, 90} {
		if got := r.top(i, 50); got != want {
			t.Fatalf("offset %d = %v, want %v", i, got, want)
		}
	}

	keys = []string{"new", "c", "a", "d"}
	v.count = 4
	r.sync(v)
	if want := []float32{-1, 30, 10, -1}; !slices.Equal(r.h, want) {
		t.Fatalf("after append: heights %v, want %v", r.h, want)
	}
}

func TestRowHeightsUpdatesAndSearch(t *testing.T) {
	r := &rowHeights{h: []float32{0, -1, 12.5, 0, 25.25}}
	check := func(est float32) {
		t.Helper()
		hs := r.h
		if len(r.shown) == len(r.h) {
			hs = r.shown
		}
		offs := make([]float32, len(hs)+1)
		var sum float64
		for i, h := range hs {
			if h < 0 {
				h = est
			}
			sum += float64(h)
			offs[i+1] = float32(sum)
		}
		for i, want := range offs {
			if got := r.top(i, est); got != want {
				t.Fatalf("offset %d = %v, want %v", i, got, want)
			}
		}
		for _, y := range []float32{-1, 0, 5, 10.5, 12.5, 23, 23.001, 40, 100, offs[len(hs)]} {
			want := 0
			for want < len(hs) && offs[want+1] <= y {
				want++
			}
			want = min(want, len(hs)-1)
			if got := r.rowAt(y, est); got != want {
				t.Fatalf("row at %v = %d, want %d", y, got, want)
			}
		}
	}
	check(10.5)
	r.setMeasured(1, 100)
	r.setMeasured(2, 0)
	r.setMeasured(4, 8.25)
	check(10.5)
	r.shown = slices.Clone(r.h)
	r.stale = true
	check(10.5)
	r.setMeasured(1, 120)
	r.setShown(1, 40)
	check(10.5)
	r.shown = nil
	r.stale = true
	check(10.5)
	r.setMeasured(1, -1)
	check(20)
	r.sync(&virtualList{count: 2})
	check(20)
	r.sync(&virtualList{count: 6})
	check(20)
}

// Grid cells either keep their width, share the line, or sit centered in it.
func TestGridLineCells(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v        virtualList
		w        float32
		cols     int
		cellW, x float32
	}{
		{"fixed width", virtualList{cellW: 100, gap: 10}, 350, 3, 100, 0},
		{"centered", virtualList{cellW: 100, gap: 10, center: true}, 350, 3, 100, 15},
		{"fill", virtualList{cellW: 100, gap: 10, fill: true}, 350, 3, 110, 0},
		{"columns", virtualList{fixed: 4, gap: 10}, 350, 4, 80, 0},
		{"list", virtualList{}, 350, 1, 350, 0},
	} {
		v := tc.v
		v.cols = v.columns(tc.w)
		cellW, x := v.lineCells(tc.w)
		if v.cols != tc.cols || cellW != tc.cellW || x != tc.x {
			t.Errorf("%s: %d columns of %v at %v, want %d of %v at %v", tc.name, v.cols, cellW, x, tc.cols, tc.cellW, tc.x)
		}
	}
}

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
		y, h := s.ScrollY, w.root.node.ContentH
		w.placeAll()
		if !w.dirty && s.ScrollY == y && w.root.node.ContentH == h && !s.Revealing && !s.Smooth {
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
	if !s.Glide || s.ScrollTY-s.ScrollY > scroll.GlideReach*100+0.5 {
		t.Fatalf("glide starts at %v toward %v (gliding %v), want within %d views", s.ScrollY, s.ScrollTY, s.Glide, scroll.GlideReach)
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
	shown := map[int]float32{}
	for i := range count {
		for _, c := range w.root.children {
			if top := c.absY - w.root.absY; c.key == strconv.Itoa(i) && top >= 0 && top < 100 {
				shown[i] = top
			}
		}
	}
	count = 60
	w.dirty = true
	settle(t, w)
	for i, top := range shown {
		if got, _ := rowSpan(t, w, i); got != top {
			t.Fatalf("scrolled up: row %d moved from %v to %v", i, top, got)
		}
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

// Scrolling a few pixels at a time keeps the rows built with overscan until
// the view reaches past them, while every row in view stays built.
func TestVirtualRowsHoldWhileViewInRange(t *testing.T) {
	calls := 0
	w := newListWindow(t, func() *El {
		return VirtualList(10000, 20, func(i int) *El {
			calls++
			return Box().H(20).Key(strconv.Itoa(i))
		})
	})
	w.build()
	w.placeAll()
	calls = 0
	for step := range 200 {
		w.scrollNow(w.root, 0, 1)
		w.placeAll()
		y := w.root.state.ScrollY
		for i := int(y / 20); float32(i*20) < y+w.root.node.H; i++ {
			rowSpan(t, w, i)
		}
		if t.Failed() {
			t.Fatalf("step %d", step)
		}
	}
	// 200px past a 100px view with 50px of overscan on each side needs a
	// new range about every 50px, of 11 rows each.
	if calls > 60 {
		t.Fatalf("built %d rows while scrolling 200px", calls)
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
