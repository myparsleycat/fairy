package fairy

import (
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
)

// stepScroll clamps the scroll offset and advances smooth wheel scrolling.
func (w *Window) stepScroll(e *El) {
	s, n := e.state, &e.node
	maxX, maxY := max(n.ContentW-n.W, 0), max(n.ContentH-n.H, 0)
	if e.stickBottom && maxY != s.endY {
		if _, by := scrollBase(s); by >= s.endY-0.5 {
			shiftScrollY(s, maxY-s.endY)
		}
	}
	if s.smooth {
		s.fling = false // the wheel or a glide took over
	}
	if s.fling {
		w.stepFling(e, maxX, maxY)
		return
	}
	if s.smooth {
		if s.bottomGlide {
			s.scrollTY = maxY // content that grows on the way extends the glide
		}
		s.scrollTX = min(max(s.scrollTX, 0), maxX)
		s.scrollTY = min(max(s.scrollTY, 0), maxY)
		m := motion.Snappy
		if s.glide {
			m = motionGlide
		}
		v := w.animate(s, animScroll, 2, 0.1, [4]float32{s.scrollTX, s.scrollTY}, m)
		s.scrollX, s.scrollY = min(max(v[0], 0), maxX), min(max(v[1], 0), maxY)
		if !s.anims[animScroll].Moving() {
			s.smooth = false
			stopGlide(s)
		}
		if e.onScroll != nil {
			e.onScroll(s.scrollX, s.scrollY)
		}
		return
	}
	s.scrollX = min(max(s.scrollX, 0), maxX)
	s.scrollY = min(max(s.scrollY, 0), maxY)
	s.scrollTX, s.scrollTY = s.scrollX, s.scrollY
	s.track(animScroll, 2, 0.1).Set([4]float32{s.scrollX, s.scrollY})
}

// virtualContent sets the scrollable size of a virtual list from its width.
func (w *Window) virtualContent(e *El) {
	v, n := e.virtual, &e.node
	pad := e.padding()
	n.ContentW = n.W
	if v.estimate > 0 {
		r := e.state.rows
		if r == nil {
			r = &rowHeights{}
			e.state.rows = r
		}
		if !v.synced {
			r.sync(v)
			v.synced = true
		}
		if v.motion == nil && r.shown != nil {
			r.shown = nil
			r.stale = true
		}
		n.ContentH = max(n.H, pad.Top+r.top(len(r.h), v.estimate)+pad.Bottom)
		return
	}
	v.cols = v.columns(max(n.W-pad.Left-pad.Right, 0))
	n.ContentH = max(n.H, pad.Top+v.contentHeight(v.cols)+pad.Bottom)
}

// virtualRows builds and lays out the lines in view, keeping them while the range holds.
func (w *Window) virtualRows(e *El) {
	if e.virtual.estimate > 0 {
		w.measuredRows(e)
		return
	}
	v, n := e.virtual, &e.node
	pad := e.padding()
	innerW := max(n.W-pad.Left-pad.Right, 0)
	lineH := v.lineHeight()
	if s := e.state; s.revealing {
		s.revealing = false
		if v.count > 0 {
			from := s.scrollY
			i := min(max(s.reveal, 0), v.count-1)
			w.revealSpan(e, pad.Top+float32(i/v.cols)*lineH, v.rowH)
			w.startGlide(e, from, s.revealInstant)
		}
	}
	overscan := n.H / 2
	top := e.state.scrollY - pad.Top - overscan
	bottom := e.state.scrollY + n.H + overscan
	first := max(int(top/lineH), 0)
	last := min(int(bottom/lineH)+1, v.lines(v.cols))
	if v.built && first == v.first && last == v.last && innerW == v.builtW {
		return
	}
	v.built, v.first, v.last, v.builtW = true, first, last, innerW
	w.clearRows(e)
	cellW, x0 := v.lineCells(innerW)
	x0 += pad.Left
	// The rows in range, built and overscan, keep their text layouts until the
	// range moves past them, though only some of them are painted.
	w.ts.BeginPass(e.id)
	defer w.ts.EndPass()
	for line := first; line < last; line++ {
		for col := 0; col < v.cols; col++ {
			i := line*v.cols + col
			if i >= v.count {
				break
			}
			c := w.buildRow(e, i)
			if c == nil {
				continue
			}
			layout.Compute(&c.node, cellW, v.rowH)
			c.node.X = x0 + float32(col)*(cellW+v.gap)
			c.node.Y = pad.Top + float32(line)*lineH
			e.children = append(e.children, c)
		}
	}
}

// buildRows builds the rows of the virtual lists in a laid-out tree while the
// frame is built, so that popovers and focus inside rows take part in it. The
// rows are built again from place() only when scrolling moves their range;
// popovers met then stay attached to the layers collected here.
func (w *Window) buildRows(e *El) {
	if e.style.Hidden {
		return
	}
	if e.virtual != nil && e.state != nil {
		w.virtualContent(e)
		w.virtualRows(e)
	}
	for _, c := range e.children {
		w.buildRows(c)
	}
}

// clearRows drops the rows a virtual list built before.
func (w *Window) clearRows(e *El) {
	w.treeVer++
	for _, c := range e.children {
		w.unindexRow(c)
	}
	clear(e.children)
	e.children = e.children[:0]
}

func (w *Window) unindexRow(e *El) {
	if w.byID[e.id] == e {
		delete(w.byID, e.id)
	}
	for _, c := range e.children {
		w.unindexRow(c)
	}
}

func (w *Window) buildRow(e *El, i int) *El {
	v := e.virtual
	path := w.memoPath
	var key string
	if r := e.state.rows; r != nil && i < len(r.keys) {
		key = r.keys[i]
	}
	w.memoPath = rowMemoPath(e, i, key)
	c := w.withBuildWindow(func() *El { return v.fn(i) })
	w.memoPath = path
	if c == nil {
		return nil
	}
	if c.key == "" && v.key != nil {
		if r := e.state.rows; r != nil && i < len(r.keys) {
			c.key = r.keys[i]
		}
	}
	c.rowIndex = int32(i) + 1
	w.prepare(c, e, 0, i, v.inh)
	return c
}

// measuredRows builds the rows of a VirtualListMeasured that are in view,
// measuring each at the list's width. Heights that differ from what was
// assumed move the rows below them; when rows above the view change, the
// scroll offset moves by as much, so the rows in view stay put.
//
// A ScrollToRow request is placed from the estimates first, then again from
// the heights measured there, until the row stays put (at most 8 times); the
// view then glides to it from at most two views away, retargeted as rows on
// the way are measured.
func (w *Window) measuredRows(e *El) {
	v, s := e.virtual, e.state
	if v.count == 0 {
		s.revealing, s.revealGlide = false, false
		if !v.built || len(e.children) > 0 {
			v.built = true
			w.clearRows(e)
		}
		return
	}
	if s.revealing {
		s.revealing = false
		s.reveal = min(max(s.reveal, 0), v.count-1)
		from := s.scrollY
		for range 8 {
			w.revealMeasured(e)
			if !w.layoutMeasured(e, false) {
				break
			}
		}
		w.startGlide(e, from, s.revealInstant)
		s.revealGlide = s.glide
	}
	if s.toBottom {
		// Stand at the bottom so the last rows are measured; place() glides
		// there from where the view was.
		from := s.scrollY
		for range 3 {
			w.moveScrollY(e, max(e.node.ContentH-e.node.H, 0))
			if !w.layoutMeasured(e, false) {
				break
			}
		}
		w.moveScrollY(e, from)
	}
	w.layoutMeasured(e, true)
	if v.motion != nil {
		w.placeShownRows(e)
	}
	if s.revealGlide {
		s.reveal = min(s.reveal, v.count-1) // rows may have gone since the request
		s.scrollTY = w.revealTarget(e, s.rowTop(e), s.rowHeight(e))
	}
}

// stepRowHeights moves the shown heights of an AnimateRowHeights list toward
// the measured ones.
func (w *Window) stepRowHeights(e *El) {
	r, s, n := e.state.rows, e.state, &e.node
	if len(r.shown) != len(r.h) {
		r.shown = append(r.shown[:0], r.h...)
		r.tracks, r.changed = map[int]*motion.Track{}, r.changed[:0]
		r.stale = true
		return
	}
	for _, i := range r.changed {
		if i < len(r.h) && r.tracks[i] == nil {
			r.tracks[i] = motion.NewTrack(1, 0.1)
			r.tracks[i].Set([4]float32{r.shown[i]})
		}
	}
	r.changed = r.changed[:0]
	if len(r.tracks) == 0 {
		return
	}
	view := s.scrollY - e.padding().Top
	est := e.virtual.estimate
	first, last := r.rowAt(view, est), r.rowAt(view+n.H, est)
	if r.top(last, est) < view+n.H {
		last++
	}
	if view >= r.top(len(r.h), est) || view+n.H <= 0 {
		last = first
	}
	for i, tr := range r.tracks {
		if i >= len(r.h) {
			delete(r.tracks, i)
			continue
		}
		h := r.h[i]
		v := h
		if h >= 0 && i >= first && i < last && !reducedMotion.Load() {
			v = max(tr.Step([4]float32{h}, *e.virtual.motion, w.dt)[0], 0)
		}
		if v == h || !tr.Moving() {
			v = h
			delete(r.tracks, i)
		} else {
			w.animating = true
		}
		if r.shown[i] != v {
			r.setShown(i, v)
		}
	}
}

// placeShownRows moves the built rows of an AnimateRowHeights list to their
// shown positions, clipping rows shown shorter than they are.
func (w *Window) placeShownRows(e *El) {
	r := e.state.rows
	top := e.padding().Top
	for _, c := range e.children {
		i := int(c.rowIndex) - 1
		if i < 0 || i >= len(r.shown) {
			continue
		}
		c.node.Y = top + r.top(i, e.virtual.estimate)
		c.node.H = max(r.shown[i], 0)
		c.grow.clip = r.shown[i] < r.h[i]
	}
}

// layoutMeasured builds the rows around the view when the rows built no
// longer cover it, and measures them. With keep, when heights change, the
// scroll offset follows the first measured row in view, or the bottom when
// a StickToBottom list is there. It reports whether any height changed.
func (w *Window) layoutMeasured(e *El, keep bool) bool {
	v, n, s := e.virtual, &e.node, e.state
	r := s.rows
	pad := e.padding()
	innerW := max(n.W-pad.Left-pad.Right, 0)
	_, by := scrollBase(s)
	atEnd := e.stickBottom && by >= max(n.ContentH-n.H, 0)-0.5
	overscan := n.H / 2
	view := s.scrollY - pad.Top
	first := r.rowAt(view-overscan, v.estimate)
	bottom := view + n.H + overscan
	if v.built && first == v.first && innerW == v.builtW && (v.last >= v.count || r.top(v.last, v.estimate) >= bottom) {
		return false
	}
	// Hold the reader's place against a row that was already measured, so
	// measuring a neighbor cannot move it.
	anchor := r.rowAt(view, v.estimate)
	for i := anchor; i < v.count && r.top(i, v.estimate) < view+n.H; i++ {
		if r.h[i] >= 0 {
			anchor = i
			break
		}
	}
	anchorTop := r.top(anchor, v.estimate)
	w.clearRows(e)
	w.ts.BeginPass(e.id)
	defer w.ts.EndPass()
	changed := false
	y := r.top(first, v.estimate)
	i := first
	for ; i < v.count && y < bottom; i++ {
		var h float32
		if c := w.buildRow(e, i); c != nil {
			layout.ComputeAtWidth(&c.node, innerW)
			c.node.X, c.node.Y = pad.Left, pad.Top+y
			h = c.node.H
			e.children = append(e.children, c)
		}
		if r.h[i] != h {
			r.setMeasured(i, h)
			changed = true
			if r.shown != nil {
				if r.shown[i] < 0 || y+h <= view || y >= view+n.H {
					r.setShown(i, h)
				} else {
					r.changed = append(r.changed, i)
				}
			}
		}
		y += h
	}
	v.built, v.first, v.last, v.builtW = true, first, i, innerW
	if !changed {
		return false
	}
	oldMax := max(n.ContentH-n.H, 0)
	n.ContentH = max(n.H, pad.Top+r.top(len(r.h), v.estimate)+pad.Bottom)
	if keep {
		if atEnd {
			shiftScrollY(s, max(n.ContentH-n.H, 0)-oldMax)
		} else if d := r.top(anchor, v.estimate) - anchorTop; d != 0 {
			shiftScrollY(s, d)
		}
	}
	// The rows built may no longer cover the view, which the next frame fixes.
	w.invalidate()
	return true
}

// rowTop and rowHeight return where the row being revealed lies in a
// VirtualListMeasured, by its estimated height until it has been measured.
func (s *elState) rowTop(e *El) float32 {
	return e.padding().Top + s.rows.top(s.reveal, e.virtual.estimate)
}

func (s *elState) rowHeight(e *El) float32 {
	if h := s.rows.h[s.reveal]; h >= 0 {
		return h
	}
	return e.virtual.estimate
}

// revealMeasured shows the row of a VirtualListMeasured being revealed, at
// its estimated position and height until it has been measured.
func (w *Window) revealMeasured(e *El) {
	s := e.state
	w.revealSpan(e, s.rowTop(e), s.rowHeight(e))
}

// revealTarget returns the scroll offset that shows content from top to
// top+h. The first call for a ScrollToRow request picks the least distance,
// lining the span up with the top or bottom edge of e, or leaving it where it
// is; later calls, made as the rows around it are measured, keep that.
func (w *Window) revealTarget(e *El, top, h float32) float32 {
	s, n := e.state, &e.node
	if s.revealEdge == revealUndecided {
		_, y := scrollBase(s)
		switch {
		case top < y || h > n.H:
			s.revealEdge = revealTop
		case top+h > y+n.H:
			s.revealEdge = revealBottom
		default:
			s.revealEdge, s.revealOff = revealKeep, top-y
		}
	}
	var y float32
	switch s.revealEdge {
	case revealTop:
		y = top
	case revealBottom:
		y = top + h - n.H
	default:
		y = top - s.revealOff
	}
	return min(max(y, 0), max(n.ContentH-n.H, 0))
}

// revealSpan moves e's vertical scroll offset at once to show content from
// top to top+h; see revealTarget.
func (w *Window) revealSpan(e *El, top, h float32) {
	w.jumpScrollY(e, w.revealTarget(e, top, h))
}

// jumpScrollY moves e's vertical scroll offset to y at once.
func (w *Window) jumpScrollY(e *El, y float32) {
	if !w.moveScrollY(e, y) {
		return
	}
	s := e.state
	if e.onScroll != nil {
		e.onScroll(s.scrollX, y)
		w.markDirty(dirtyScroll)
	}
}

// moveScrollY is jumpScrollY without OnScroll, for offsets the view passes
// through within a frame. It reports whether the offset changed.
func (w *Window) moveScrollY(e *El, y float32) bool {
	s := e.state
	if y == s.scrollY && !s.smooth {
		return false
	}
	s.scrollY, s.scrollTX, s.scrollTY, s.smooth = y, s.scrollX, y, false
	stopGlide(s)
	s.track(animScroll, 2, 0.1).Set([4]float32{s.scrollX, y})
	w.invalidate()
	return true
}

// motionGlide carries the view to a row or the bottom; it is gentler than
// the wheel's motion.Snappy.
var motionGlide = motion.Spring(0.5, 0)

// glideReach is how many views away from its end a glide starts at most, so a
// jump across a long list glides only the last stretch.
const glideReach = 2

// startGlide turns a jump of e's vertical scroll offset, from from to where
// it is now, into a glide that starts at most glideReach views away.
func (w *Window) startGlide(e *El, from float32, instant bool) {
	s, n := e.state, &e.node
	to := s.scrollY
	if instant || reducedMotion.Load() || abs32(to-from) < 1 {
		return
	}
	reach := glideReach * n.H
	start := min(max(from, to-reach), to+reach)
	s.scrollY, s.scrollTY, s.smooth, s.glide = start, to, true, true
	s.track(animScroll, 2, 0.1).Set([4]float32{s.scrollX, start})
	if e.virtual != nil && e.virtual.estimate > 0 {
		w.layoutMeasured(e, true)
	}
	w.invalidate()
}

func stopGlide(s *elState) {
	s.glide, s.revealGlide, s.bottomGlide, s.fling = false, false, false, false
}

// shiftScrollY moves a scroll offset, and any wheel scrolling toward a target, by d.
func shiftScrollY(s *elState, d float32) {
	s.scrollY += d
	s.scrollTY += d
	if t := s.anims[animScroll]; t != nil && t.Initialized() {
		t.Shift([4]float32{0, d})
	}
}
