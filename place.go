//go:build windows

package fairy

import (
	"maps"
	"math"

	"github.com/myparsleycat/fairy/internal/frameprof"
	"github.com/myparsleycat/fairy/internal/geom"
	"github.com/myparsleycat/fairy/internal/scroll"
	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
)

func (w *Window) snap(r geom.Rect) [4]float32 {
	s := w.scale
	x0 := float32(math.Round(float64(r.X * s)))
	y0 := float32(math.Round(float64(r.Y * s)))
	x1 := float32(math.Round(float64((r.X + r.W) * s)))
	y1 := float32(math.Round(float64((r.Y + r.H) * s)))
	return [4]float32{x0, y0, x1 - x0, y1 - y0}
}

func (w *Window) devRect(r geom.Rect) [4]float32 {
	s := w.scale
	return [4]float32{r.X * s, r.Y * s, r.W * s, r.H * s}
}

func (w *Window) hovered(e *El) bool { return w.hoverIDs[e.id] }

// focusOf reports whether e shows its focus styles, and whether that focus is
// visible as CSS :focus-visible: the keyboard moved it, or it is in an Input.
func (w *Window) focusOf(e *El) (focused, visible bool) {
	if w.focused == 0 {
		return false, false
	}
	f := w.byID[w.focused]
	if f == nil {
		return false, false
	}
	if e.focusParent && e.parent != nil && f == e.parent {
		return true, w.keyFocus || f.kind == kindInput
	}
	if f == e {
		focused = e.focusable
	} else if e.focusWithin {
		for p := f.parent; p != nil; p = p.parent {
			if p == e {
				focused = true
				break
			}
		}
	}
	return focused, focused && (w.keyFocus || f.kind == kindInput)
}

func (w *Window) pressed(e *El) bool {
	return w.press != nil && w.press.el == e.id && w.hovered(e)
}

func hasAbsoluteChild(e *El) bool {
	for _, c := range e.children {
		if c.style.Absolute {
			return true
		}
	}
	return false
}

func (w *Window) windowRect() geom.Rect {
	return geom.Rect{X: 0, Y: 0, W: float32(w.width) / w.scale, H: float32(w.height) / w.scale}
}

// placeAll computes absolute positions, clips, hit-test boxes, focus order and
// scroll bar geometry for the laid-out tree and its layers.
func (w *Window) placeAll() {
	w.placePass++
	// Clear what this pass leaves of the last one's elements, which would
	// keep the tree they were built in alive.
	oldHits, oldFocus := len(w.hits), len(w.focusables)
	defer func() {
		if n := len(w.hits); n < oldHits {
			clear(w.hits[n:oldHits])
		}
		if n := len(w.focusables); n < oldFocus {
			clear(w.focusables[n:oldFocus])
		}
	}()
	w.hits = w.hits[:0]
	w.chromeHits = w.chromeHits[:0]
	w.bars = w.bars[:0]
	w.focusables = w.focusables[:0]
	if len(w.matched) > w.matchedPrune {
		// placeFlight takes no record from before the last frame.
		maps.DeleteFunc(w.matched, func(_ string, r matchRec) bool { return r.frame < w.frame-1 })
		w.matchedPrune = max(64, 2*len(w.matched))
	}
	if w.root == nil {
		return
	}
	wr := w.windowRect()
	if w.growing && (w.growStale || w.grew) {
		w.growStale, w.grew = false, false
		w.growPass(w.root)
		for _, l := range w.layers {
			w.growPass(l.root)
		}
	}
	w.place(w.root, 0, 0, wr)
	rootChrome := len(w.chromeHits)
	w.chromeTop = max(w.topChromeBottom(w.chromeHits, wr), w.layerChromeTop)
	area := geom.Rect{X: 0, Y: w.chromeTop, W: wr.W, H: wr.H - w.chromeTop}
	for _, l := range w.layers {
		l.hidden = false
		n := &l.root.node
		if l.pop != nil {
			a := w.byID[l.anchor]
			if a == nil || a.culled {
				l.hidden = true
				continue
			}
			ar := geom.Rect{X: a.absX, Y: a.absY, W: a.w, H: a.h}
			if o := l.pop.opts; o.AtPoint {
				ar = geom.Rect{X: a.absX + o.X, Y: a.absY + o.Y, W: 0, H: 0}
			}
			// A scrolling popover is cut to the room on its roomier side, as
			// Floating UI's size middleware sets --available-height.
			if room := l.pop.opts.geom().Room(ar, area); l.root.style.ScrollY && n.H > room {
				layout.ComputeFit(n, wr.W, room)
			}
			x, y, gs := l.pop.opts.geom().Position(ar, n.W, n.H, area)
			n.X, n.Y = x, y
			side := Side(gs)
			root := l.root
			root.popSide = uint8(side) + 1
			frac := func(v, start, size float32) float32 {
				if size <= 0 {
					return 0.5
				}
				return min(max((v-start)/size, 0), 1)
			}
			switch side {
			case SideBottom:
				root.popOX, root.popOY = frac(ar.X+ar.W/2, n.X, n.W), 0
			case SideTop:
				root.popOX, root.popOY = frac(ar.X+ar.W/2, n.X, n.W), 1
			case SideRight:
				root.popOX, root.popOY = 0, frac(ar.Y+ar.H/2, n.Y, n.H)
			case SideLeft:
				root.popOX, root.popOY = 1, frac(ar.Y+ar.H/2, n.Y, n.H)
			}
		} else {
			n.X, n.Y = 0, 0
		}
		l.focusStart = len(w.focusables)
		w.place(l.root, 0, 0, wr)
		l.focusEnd = len(w.focusables)
	}
	w.layerChromeTop = w.topChromeBottom(w.chromeHits[rootChrome:], wr)
	if r := w.scrollStepReq; r != nil {
		w.scrollStepReq = nil
		w.applyScrollStep(r)
	}
	if k := w.intoViewKey; k != "" {
		w.intoViewKey = ""
		if e := w.findKey(k); e != nil {
			w.glideIntoView(e)
		}
	}
}

// glideIntoView scrolls the containers around e as the accessibility
// ScrollIntoView does, gliding the vertical ones.
func (w *Window) glideIntoView(e *El) {
	type start struct {
		el *El
		y  float32
	}
	var from []start
	for a := e.parent; a != nil; a = a.parent {
		if a.state != nil && a.style.ScrollY {
			from = append(from, start{a, a.state.ScrollY})
		}
	}
	w.scrollIntoView(e)
	for _, f := range from {
		w.startGlide(f.el, f.y, false)
	}
}

// findKey returns the last element with key in the placed tree and layers.
func (w *Window) findKey(key string) *El {
	var found *El
	var walk func(*El)
	walk = func(e *El) {
		if e.key == key && !e.style.Hidden {
			found = e
		}
		for _, c := range e.children {
			walk(c)
		}
	}
	if w.root != nil {
		walk(w.root)
	}
	for _, l := range w.layers {
		walk(l.root)
	}
	return found
}

// growDelta is what growPass added to an element's laid-out node.
type growDelta struct {
	dy, dh, ch float32 // to Y, H and ContentH
	clip       bool    // the shown height is short of the content
}

// growPass moves the laid-out tree by the heights AnimateHeight shows: an
// animating element takes its shown height, the auto-height boxes around it
// grow or shrink with it, the elements after it in a column move, and a
// vertical scroll container's content height follows. It first undoes the
// last frame's changes, since the tree is laid out again only when the view
// is built. It returns how much e's height changed.
func (w *Window) growPass(e *El) float32 {
	n := &e.node
	n.H -= e.grow.dh
	n.ContentH -= e.grow.ch
	e.grow.dh, e.grow.ch, e.grow.clip = 0, 0, false
	if e.style.Hidden || e.virtual != nil {
		return 0
	}
	column := e.style.Direction == layout.Column && !e.style.Wrap
	row := e.style.Direction == layout.Row && !e.style.Wrap
	var shift, before, after, bottom float32
	for _, c := range e.children {
		c.node.Y -= c.grow.dy
		c.grow.dy = 0
		if c.style.Absolute {
			w.growPass(c)
			continue
		}
		// c's height still holds its last frame's growth until growPass(c) undoes it.
		ch := c.node.H - c.grow.dh
		bottom = max(bottom, c.node.Y+ch+c.style.Margin.Bottom)
		if column && shift != 0 {
			c.node.Y += shift
			c.grow.dy = shift
		}
		b := c.node.Y + ch
		d := w.growPass(c)
		if column {
			shift += d
		} else if row {
			before, after = max(before, b), max(after, b+d)
		}
	}
	inner := shift
	if row {
		inner = after - before
	}
	switch {
	case e.heightMotion != nil && e.state != nil:
		target := n.H + inner
		shown := max(w.animate(e.state, animHeight, 1, 0.1, [4]float32{target}, *e.heightMotion)[0], 0)
		e.grow.dh = shown - n.H
		e.grow.clip = shown != target
	case e.parent == nil || e.style.Height.Unit != layout.Auto || e.style.ScrollY ||
		e.style.Grow > 0 && e.parent.style.Direction == layout.Column:
		if e.style.ScrollY && column && inner != 0 {
			content := n.ContentH
			if content <= n.H+0.5 {
				// Layout raised the content height to the box's own.
				content = bottom + e.padding().Bottom
			}
			e.grow.ch = max(n.H, content+inner) - n.ContentH
		}
	default:
		e.grow.dh = inner
	}
	n.H += e.grow.dh
	n.ContentH += e.grow.ch
	if e.grow.dh != 0 || e.grow.ch != 0 || e.grow.clip || shift != 0 {
		w.grew = true
	}
	return e.grow.dh
}

// topChromeBottom returns the bottom of the WindowChrome boxes among hits placed
// against the top edge of wr. Exclusions and boxes taller than half the window,
// which mark more than a title bar, do not count.
func (w *Window) topChromeBottom(hits []int, wr geom.Rect) float32 {
	var bottom float32
	for _, i := range hits {
		h := &w.hits[i]
		if h.el.excludeChrome || h.y > wr.Y+0.5 || h.h > wr.H/2 {
			continue
		}
		r := geom.Rect{X: h.x, Y: h.y, W: h.w, H: h.h}.Intersect(h.clip)
		if !r.Empty() {
			bottom = max(bottom, r.Y+r.H)
		}
	}
	return bottom
}

// animate advances an element's track toward target and returns its value.
func (w *Window) animate(s *elState, p animProp, n int, eps float32, target [4]float32, m motion.Spec) [4]float32 {
	if (p == animRect || p == animXform || p == animHeight) && reducedMotion.Load() {
		m = motion.Instant
	}
	tr := s.track(p, n, eps)
	v := tr.Step(target, m, w.dt)
	if tr.Moving() {
		w.animating = true
	}
	return v
}

func (w *Window) place(e *El, ox, oy float32, clip geom.Rect) {
	e.culled = true
	e.inChrome, e.excludeChrome = e.windowChrome, e.chromeExclude
	if p := e.parent; p != nil {
		e.inChrome = e.inChrome || p.inChrome
		e.excludeChrome = e.excludeChrome || p.excludeChrome
	}
	if e.style.Hidden {
		return
	}
	n := &e.node
	x, y, ew, eh := n.X, n.Y, n.W, n.H
	s := e.state
	if s != nil && e.layoutMotion != nil {
		v := w.animate(s, animRect, 4, 0.05, [4]float32{x, y, ew, eh}, *e.layoutMotion)
		// Springs and retargeted eases can overshoot below zero.
		x, y, ew, eh = v[0], v[1], max(v[2], 0), max(v[3], 0)
	}
	e.fade = 1
	enterScale := float32(1)
	if s != nil && e.enter != nil {
		t := e.enter.sided(e.popSide).effective()
		tr := s.track(animEnter, 4, 0.002)
		if !tr.Initialized() {
			if s.born == w.gen && w.gen > 1 {
				tr.Set([4]float32{t.Opacity, t.X, t.Y, t.scale()})
			} else {
				tr.Set([4]float32{1, 0, 0, 1})
			}
		}
		v := w.animate(s, animEnter, 4, 0.002, [4]float32{1, 0, 0, 1}, t.Motion)
		e.fade = min(max(v[0], 0), 1)
		x += v[1]
		y += v[2]
		enterScale = max(v[3], 0)
	}
	if e.live != nil && e.live.offset != nil {
		d := e.live.offset.read(w)
		x += d.X
		y += d.Y
	}
	e.absX, e.absY, e.w, e.h = ox+x, oy+y, ew, eh
	w.placeTransform(e, enterScale)
	if e.matchID != "" {
		w.placeFlight(e)
	}
	r := geom.Rect{X: e.absX, Y: e.absY, W: ew, H: eh}
	vis := r.Intersect(clip)
	// Exiting children are drawn where they were, so a box that shrank to
	// nothing as its last child left still has to draw them.
	keep := !e.clip && (hasAbsoluteChild(e) || (ew <= 0 || eh <= 0) && w.ghostIn[e.id])
	if vis.Empty() && !keep {
		w.placeCulled(e)
		return
	}
	e.culled = false
	e.clipR = clip
	if e.onLayout != nil && (n.W != s.layoutW || n.H != s.layoutH || !s.laidOut) {
		s.layoutW, s.layoutH, s.laidOut = n.W, n.H, true
		if e.onLayout(n.W, n.H) {
			w.markDirty(frameprof.CauseLayout)
		}
	}
	h := hit{el: e, x: r.X, y: r.Y, w: r.W, h: r.H, clip: clip, radii: e.radii}
	if !e.xf.identity() {
		h.inv, h.turned = e.xf.inverse(), true
	}
	if p := e.parent; p != nil && !p.xf.identity() {
		// The clip comes from the parent's layout space.
		h.clipInv, h.clipTurned = p.xf.inverse(), true
	}
	if e.inChrome {
		w.chromeHits = append(w.chromeHits, len(w.hits))
	}
	w.hits = append(w.hits, h)
	if e.focusable {
		w.focusables = append(w.focusables, e)
	}
	childClip := clip
	pinClip := clip
	if e.clip || e.grow.clip {
		in := w.innerBox(e, w.snap(r))
		s := w.scale
		box := geom.Rect{X: in[0] / s, Y: in[1] / s, W: in[2] / s, H: in[3] / s}
		childClip = vis.Intersect(box)
		pinClip = childClip
		if ci := e.clipInset; ci != [4]float32{} {
			childClip = childClip.Intersect(geom.Rect{
				X: box.X + ci[3], Y: box.Y + ci[0], W: max(box.W-ci[1]-ci[3], 0), H: max(box.H-ci[0]-ci[2], 0),
			})
		}
	}
	e.kidClip, e.pinClip = childClip, pinClip
	if e.virtual != nil {
		w.virtualContent(e)
		if e.virtual.motion != nil {
			// Only here, once a frame, so building the view does not step it too.
			w.stepRowHeights(e)
			w.virtualContent(e)
		}
		if w.revealKey != "" && e.key == w.revealKey {
			s.Reveal, s.Revealing, s.RevealEdge = w.revealRow, true, scroll.EdgeUndecided
			if w.revealTop {
				s.RevealEdge = scroll.EdgeTop
			}
			s.RevealInstant = w.revealInstant
			w.revealKey = ""
		}
	}
	if w.bottomKey != "" && e.key == w.bottomKey && s != nil && e.style.ScrollY {
		s.ToBottom = true
		w.bottomKey = ""
	}
	if s != nil && e.kind != kindInput {
		w.stepScroll(e)
	}
	if e.virtual != nil {
		// A measured list may move the scroll offset to keep its rows in place.
		w.virtualRows(e)
	}
	if s != nil && s.ToBottom {
		// A measured list has measured its last rows by now.
		s.ToBottom = false
		from := s.ScrollY
		w.moveScrollY(e, max(n.ContentH-n.H, 0))
		w.startGlide(e, from, false)
		s.BottomGlide = s.Glide
		if e.virtual != nil && e.virtual.estimate == 0 {
			w.virtualRows(e)
		}
		if e.onScroll != nil && s.ScrollY != from {
			e.onScroll(s.ScrollX, s.ScrollY)
			w.markDirty(frameprof.CauseScroll)
		}
	}
	if e.stickBottom && s != nil {
		s.EndY = max(n.ContentH-n.H, 0)
	}
	if e.onScrollEdges != nil && s != nil {
		top, bottom := s.ScrollY <= 0.5, s.ScrollY >= n.ContentH-n.H-0.5
		if !s.edgesSeen || top != s.atTop || bottom != s.atBottom {
			s.edgesSeen, s.atTop, s.atBottom = true, top, bottom
			e.onScrollEdges(top, bottom)
			w.markDirty(frameprof.CauseScroll)
		}
	}
	cx, cy := e.absX, e.absY
	if s != nil && e.kind != kindInput {
		cx -= s.ScrollX
		cy -= s.ScrollY
	}
	for _, c := range e.children {
		if c.pinToView {
			w.place(c, e.absX, e.absY, pinClip)
			continue
		}
		w.place(c, cx, cy, childClip)
	}
	overY := e.style.ScrollY && n.ContentH > n.H+0.5
	overX := e.style.ScrollX && n.ContentW > n.W+0.5
	if (overY || overX) && !e.effectiveScrollBarStyle().Hidden {
		if overY {
			w.placeScrollBar(e, vis, true)
		}
		if overX {
			w.placeScrollBar(e, vis, false)
		}
	}
}

// placeCulled gives the subtree of a culled e its layout boxes and its place
// in the focus order, without painting or hit-testing it, so the keyboard
// reaches elements scrolled out of sight and can scroll them into view.
func (w *Window) placeCulled(e *El) {
	if e.focusable {
		w.focusables = append(w.focusables, e)
	}
	cx, cy := e.absX, e.absY
	if s := e.state; s != nil && e.kind != kindInput {
		cx -= s.ScrollX
		cy -= s.ScrollY
	}
	for _, c := range e.children {
		c.culled = true
		if c.style.Hidden {
			continue
		}
		n := &c.node
		c.absX, c.absY, c.w, c.h = cx+n.X, cy+n.Y, n.W, n.H
		if c.pinToView {
			c.absX, c.absY = e.absX+n.X, e.absY+n.Y
		}
		w.placeCulled(c)
	}
}

type animProp uint8

const (
	animFill animProp = iota
	animBorder
	animText
	animOpacity
	animRect
	animEnter
	animXform
	animThumb
	animRing
	animShadow0
	animShadow1
	animPress
	animBar
	animArc
	animHeight
	animFlight
	animCount
)
