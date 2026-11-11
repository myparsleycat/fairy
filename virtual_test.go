package fairy

import (
	"slices"
	"testing"
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
