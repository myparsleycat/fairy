//go:build windows

package fairy

import (
	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
)

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
	if s := e.state; s.Revealing {
		s.Revealing = false
		if v.count > 0 {
			from := s.ScrollY
			i := min(max(s.Reveal, 0), v.count-1)
			w.revealSpan(e, pad.Top+float32(i/v.cols)*lineH, v.rowH)
			w.startGlide(e, from, s.RevealInstant)
		}
	}
	lines := v.lines(v.cols)
	view := e.state.ScrollY - pad.Top
	// Rows built with overscan stay until the view reaches past them, so a
	// scroll of a few pixels does not build the whole range again.
	if v.built && innerW == v.builtW && v.last <= lines &&
		v.first <= max(int(view/lineH), 0) && min(int((view+n.H)/lineH)+1, lines) <= v.last {
		return
	}
	overscan := n.H / 2
	top := view - overscan
	bottom := e.state.ScrollY + n.H + overscan
	first := max(int(top/lineH), 0)
	last := min(int(bottom/lineH)+1, lines)
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
		s.Revealing, s.RevealGlide = false, false
		if !v.built || len(e.children) > 0 {
			v.built = true
			w.clearRows(e)
		}
		return
	}
	if s.Revealing {
		s.Revealing = false
		s.Reveal = min(max(s.Reveal, 0), v.count-1)
		from := s.ScrollY
		for range 8 {
			w.revealMeasured(e)
			if !w.layoutMeasured(e, false) {
				break
			}
		}
		w.startGlide(e, from, s.RevealInstant)
		s.RevealGlide = s.Glide
	}
	if s.ToBottom {
		// Stand at the bottom so the last rows are measured; place() glides
		// there from where the view was.
		from := s.ScrollY
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
	if s.RevealGlide {
		s.Reveal = min(s.Reveal, v.count-1) // rows may have gone since the request
		s.ScrollTY = w.revealTarget(e, s.rowTop(e), s.rowHeight(e))
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
	view := s.ScrollY - e.padding().Top
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
	_, by := s.Base()
	atEnd := e.stickBottom && by >= max(n.ContentH-n.H, 0)-0.5
	overscan := n.H / 2
	view := s.ScrollY - pad.Top
	first := r.rowAt(view-overscan, v.estimate)
	bottom := view + n.H + overscan
	if v.built && innerW == v.builtW && r.top(v.first, v.estimate) <= max(view, 0) &&
		(v.last >= v.count || r.top(v.last, v.estimate) >= view+n.H) {
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
			s.ShiftY(max(n.ContentH-n.H, 0) - oldMax)
		} else if d := r.top(anchor, v.estimate) - anchorTop; d != 0 {
			s.ShiftY(d)
		}
	}
	// The rows built may no longer cover the view, which the next frame fixes.
	w.invalidate()
	return true
}

// rowTop and rowHeight return where the row being revealed lies in a
// VirtualListMeasured, by its estimated height until it has been measured.
func (s *elState) rowTop(e *El) float32 {
	return e.padding().Top + s.rows.top(s.Reveal, e.virtual.estimate)
}

func (s *elState) rowHeight(e *El) float32 {
	if h := s.rows.h[s.Reveal]; h >= 0 {
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
		e.onScroll(s.ScrollX, y)
		w.markDirty(frameprof.CauseScroll)
	}
}

// stepScroll clamps the scroll offset and advances smooth wheel scrolling or a fling.
func (w *Window) stepScroll(e *El) {
	s, n := e.state, &e.node
	moved, moving := s.Step(n.ContentW-n.W, n.ContentH-n.H, w.dt, e.stickBottom, reducedMotion.Load())
	if moving {
		w.animating = true
	}
	if moved && e.onScroll != nil {
		e.onScroll(s.ScrollX, s.ScrollY)
	}
}

// revealTarget returns the scroll offset that shows content from top to
// top+h in e; see scroll.State.RevealTarget.
func (w *Window) revealTarget(e *El, top, h float32) float32 {
	n := &e.node
	return e.state.RevealTarget(top, h, n.H, n.ContentH-n.H)
}

// moveScrollY is jumpScrollY without OnScroll, for offsets the view passes
// through within a frame. It reports whether the offset changed.
func (w *Window) moveScrollY(e *El, y float32) bool {
	if !e.state.MoveY(y) {
		return false
	}
	w.invalidate()
	return true
}

// startGlide turns a jump of e's vertical scroll offset, from from to where
// it is now, into a glide.
func (w *Window) startGlide(e *El, from float32, instant bool) {
	if !e.state.StartGlide(from, e.node.H, instant || reducedMotion.Load()) {
		return
	}
	if e.virtual != nil && e.virtual.estimate > 0 {
		w.layoutMeasured(e, true)
	}
	w.invalidate()
}
