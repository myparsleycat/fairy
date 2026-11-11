package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/motion"
)

type virtualList struct {
	count    int
	rowH     float32 // line height (cell height for grids)
	cellW    float32 // 0 for a list
	fixed    int     // column count set by GridColumns
	fill     bool    // cells share each line's width
	center   bool    // lines of fixed-width cells are centered
	gap      float32
	estimate float32      // > 0 for rows of varying height, which are measured
	motion   *motion.Spec // AnimateRowHeights
	key      func(i int) string
	fn       func(i int) *El
	inh      textProps
	synced   bool // the measured heights were matched to this build's rows
	built    bool
	first    int
	last     int
	builtW   float32
	cols     int
}

// VirtualList is a vertical scroll container that builds only the rows in view.
// Every row is rowHeight tall; row(i) builds row i. Bound its height like any
// scroll container (H, MaxH, or Grow inside a bounded parent).
func VirtualList(count int, rowHeight float32, row func(i int) *El) *El {
	e := Box().ScrollY()
	e.virtual = &virtualList{count: count, rowH: rowHeight, fn: row}
	return e
}

// VirtualListMeasured is a VirtualList whose rows may differ in height. A row
// is measured when it is built; rows not built yet count as estimate tall, so
// the scroll range settles as rows come into view. When rows above the view
// change height, the scroll offset moves with them so that the rows in view
// stay in place.
//
// key(i) names row i: its measured height follows it when rows are inserted,
// removed or reordered, and it becomes the row's Key unless the row sets one.
// key is called for every row each time the view is built, so it should be
// cheap. With a nil key, heights belong to row indices. As in VirtualList, a
// row's own margin is ignored; space rows apart with padding inside the row.
func VirtualListMeasured(count int, estimate float32, key func(i int) string, row func(i int) *El) *El {
	e := Box().ScrollY()
	e.virtual = &virtualList{count: count, estimate: max(estimate, 1), key: key, fn: row}
	return e
}

// AnimateRowHeights makes the rows of a VirtualListMeasured that change height
// while in view, such as the last message of a chat as it streams in, grow
// and shrink to their new height with m, moving the rows below them. Rows
// out of view and rows measured for the first time take their height at
// once.
func (e *El) AnimateRowHeights(m motion.Spec) *El {
	if e.virtual != nil && e.virtual.estimate > 0 {
		e.virtual.motion = &m
	}
	return e
}

// VirtualGrid is a scroll container of fixed-size cells laid out in as many
// columns as fit, building only the cells in view.
func VirtualGrid(count int, cellW, cellH, gap float32, cell func(i int) *El) *El {
	e := Box().ScrollY()
	e.virtual = &virtualList{count: count, rowH: cellH, cellW: cellW, gap: gap, fn: cell}
	return e
}

// GridFill makes the cells of a VirtualGrid share each line's width; the
// grid's cell width becomes the narrowest a cell may be.
func (e *El) GridFill() *El {
	if e.virtual != nil {
		e.virtual.fill = true
	}
	return e
}

// GridColumns lays a VirtualGrid out in n columns that share each line's width.
func (e *El) GridColumns(n int) *El {
	if e.virtual != nil {
		e.virtual.fixed = max(n, 1)
	}
	return e
}

// GridCenter centers the lines of a VirtualGrid whose cells keep their width.
func (e *El) GridCenter() *El {
	if e.virtual != nil {
		e.virtual.center = true
	}
	return e
}

// columns returns how many cells fit in a line of width w.
func (v *virtualList) columns(w float32) int {
	if v.fixed > 0 {
		return v.fixed
	}
	if v.cellW <= 0 {
		return 1
	}
	return max(1, int(math.Floor(float64((w+v.gap)/(v.cellW+v.gap)))))
}

// lineCells returns the width of each cell in a line of width w, and how far
// the first cell starts from the line's left edge.
func (v *virtualList) lineCells(w float32) (cellW, x0 float32) {
	switch {
	case v.fixed > 0 || v.cellW > 0 && v.fill:
		return max((w-v.gap*float32(v.cols-1))/float32(v.cols), 0), 0
	case v.cellW > 0 && v.center:
		return v.cellW, max(w-float32(v.cols)*v.cellW-float32(v.cols-1)*v.gap, 0) / 2
	case v.cellW > 0:
		return v.cellW, 0
	}
	return w, 0
}

// lineHeight is the distance between the tops of consecutive lines.
func (v *virtualList) lineHeight() float32 { return v.rowH + v.gap }

func (v *virtualList) lines(cols int) int { return (v.count + cols - 1) / cols }

// contentHeight is the height of all lines, without padding.
func (v *virtualList) contentHeight(cols int) float32 {
	n := v.lines(cols)
	if n == 0 {
		return 0
	}
	return float32(n)*v.lineHeight() - v.gap
}

// rowHeights keeps the measured heights of a VirtualListMeasured across builds.
type rowHeights struct {
	h []float32 // measured heights; unmeasured rows are negative
	// With AnimateRowHeights, shown holds the heights rows are drawn at and
	// offsets are computed from; tracks animate the rows in moving, and changed
	// lists rows whose measured height moved away from the shown one.
	shown   []float32
	tracks  map[int]*motion.Track
	changed []int
	keys    []string // the key of each row when the list has keys
	spare   []string
	sums    []float64 // Fenwick sums keep height changes and row lookup logarithmic.
	stale   bool
	est     float32
}

// sync matches the measured heights to the rows of v.
func (r *rowHeights) sync(v *virtualList) {
	if v.key == nil {
		r.keys = r.keys[:0]
		if len(r.h) != v.count {
			r.h = resizeHeights(r.h, v.count)
			r.stale = true
			r.snapShown()
		}
		return
	}
	keys := r.spare[:0]
	same := len(r.keys) == v.count
	for i := 0; i < v.count; i++ {
		k := v.key(i)
		keys = append(keys, k)
		if same && r.keys[i] != k {
			same = false
		}
	}
	if !same {
		old := make(map[string]float32, len(r.keys))
		for i, k := range r.keys {
			if i < len(r.h) && r.h[i] >= 0 {
				old[k] = r.h[i]
			}
		}
		h := resizeHeights(r.h[:0], v.count)
		for i, k := range keys {
			if m, ok := old[k]; ok {
				h[i] = m
			}
		}
		r.h = h
		r.stale = true
		r.snapShown()
	}
	r.keys, r.spare = keys, r.keys
}

// snapShown drops any height animation, showing the measured heights.
func (r *rowHeights) snapShown() {
	if r.shown == nil {
		return
	}
	r.shown = append(r.shown[:0], r.h...)
	clear(r.tracks)
	r.changed = r.changed[:0]
}

// resizeHeights returns h with n entries, marking added ones unmeasured.
func resizeHeights(h []float32, n int) []float32 {
	if n <= len(h) {
		return h[:n]
	}
	k := len(h)
	h = append(h, make([]float32, n-k)...)
	for i := k; i < n; i++ {
		h[i] = -1
	}
	return h
}

func (r *rowHeights) prepare(est float32) {
	if !r.stale && r.est == est && len(r.sums) == len(r.h)+1 {
		return
	}
	n := len(r.h)
	if cap(r.sums) < n+1 {
		r.sums = make([]float64, n+1)
	} else {
		r.sums = r.sums[:n+1]
		clear(r.sums)
	}
	hs := r.h
	if len(r.shown) == len(r.h) {
		hs = r.shown
	}
	for i, h := range hs {
		if h < 0 {
			h = est
		}
		j := i + 1
		r.sums[j] += float64(h)
		if p := j + j&-j; p <= n {
			r.sums[p] += r.sums[j]
		}
	}
	r.stale, r.est = false, est
}

func (r *rowHeights) top(i int, est float32) float32 {
	r.prepare(est)
	var y float64
	for ; i > 0; i -= i & -i {
		y += r.sums[i]
	}
	return float32(y)
}

func (r *rowHeights) setMeasured(i int, h float32) {
	old := r.h[i]
	r.h[i] = h
	if len(r.shown) != len(r.h) {
		r.adjust(i, old, h)
	}
}

func (r *rowHeights) setShown(i int, h float32) {
	old := r.shown[i]
	r.shown[i] = h
	r.adjust(i, old, h)
}

func (r *rowHeights) adjust(i int, old, h float32) {
	if r.stale || len(r.sums) != len(r.h)+1 {
		return
	}
	if old < 0 {
		old = r.est
	}
	if h < 0 {
		h = r.est
	}
	d := float64(h) - float64(old)
	for j := i + 1; j < len(r.sums); j += j & -j {
		r.sums[j] += d
	}
}

// rowAt returns the row that covers y, or the last row past the end.
func (r *rowHeights) rowAt(y, est float32) int {
	r.prepare(est)
	n, i := len(r.h), 0
	var sum float64
	bit := 1
	for bit <= n/2 {
		bit <<= 1
	}
	for ; bit > 0; bit >>= 1 {
		if next := i + bit; next <= n && float32(sum+r.sums[next]) <= y {
			sum += r.sums[next]
			i = next
		}
	}
	return min(i, max(n-1, 0))
}
