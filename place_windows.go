package fairy

import (
	"math"

	"github.com/myparsleycat/fairy/layout"
	"github.com/myparsleycat/fairy/motion"
)

func (w *Window) snap(r rectF) [4]float32 {
	s := w.scale
	x0 := float32(math.Round(float64(r.x * s)))
	y0 := float32(math.Round(float64(r.y * s)))
	x1 := float32(math.Round(float64((r.x + r.w) * s)))
	y1 := float32(math.Round(float64((r.y + r.h) * s)))
	return [4]float32{x0, y0, x1 - x0, y1 - y0}
}

func (w *Window) devRect(r rectF) [4]float32 {
	s := w.scale
	return [4]float32{r.x * s, r.y * s, r.w * s, r.h * s}
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

func (w *Window) windowRect() rectF {
	return rectF{0, 0, float32(w.width) / w.scale, float32(w.height) / w.scale}
}

// placeAll computes absolute positions, clips, hit-test boxes, focus order and
// scroll bar geometry for the laid-out tree and its layers.
func (w *Window) placeAll() {
	w.placePass++
	w.hits = w.hits[:0]
	w.chromeHits = w.chromeHits[:0]
	w.bars = w.bars[:0]
	w.focusables = w.focusables[:0]
	if w.root == nil {
		return
	}
	wr := w.windowRect()
	if w.growing {
		w.growPass(w.root)
		for _, l := range w.layers {
			w.growPass(l.root)
		}
	}
	w.place(w.root, 0, 0, wr)
	for _, l := range w.layers {
		l.hidden = false
		n := &l.root.node
		if l.pop != nil {
			a := w.byID[l.anchor]
			if a == nil || a.culled {
				l.hidden = true
				continue
			}
			ar := rectF{a.absX, a.absY, a.w, a.h}
			if o := l.pop.opts; o.AtPoint {
				ar = rectF{a.absX + o.X, a.absY + o.Y, 0, 0}
			}
			// A scrolling popover is cut to the room on its roomier side, as
			// Floating UI's size middleware sets --available-height.
			if room := popoverRoom(ar, l.pop.opts, wr); l.root.style.ScrollY && n.H > room {
				layout.ComputeFit(n, wr.w, room)
			}
			var side Side
			n.X, n.Y, side = popoverPosition(ar, n.W, n.H, l.pop.opts, wr)
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
				root.popOX, root.popOY = frac(ar.x+ar.w/2, n.X, n.W), 0
			case SideTop:
				root.popOX, root.popOY = frac(ar.x+ar.w/2, n.X, n.W), 1
			case SideRight:
				root.popOX, root.popOY = 0, frac(ar.y+ar.h/2, n.Y, n.H)
			case SideLeft:
				root.popOX, root.popOY = 1, frac(ar.y+ar.h/2, n.Y, n.H)
			}
		} else {
			n.X, n.Y = 0, 0
		}
		l.focusStart = len(w.focusables)
		w.place(l.root, 0, 0, wr)
		l.focusEnd = len(w.focusables)
	}
	if k := w.intoViewKey; k != "" {
		w.intoViewKey = ""
		if e := w.findKey(k); e != nil {
			w.glideIntoView(e)
		}
	}
}

// glideIntoView scrolls the containers around e as the accessibility
// ScrollIntoItem does, gliding the vertical ones.
func (w *Window) glideIntoView(e *El) {
	type start struct {
		el *El
		y  float32
	}
	var from []start
	for a := e.parent; a != nil; a = a.parent {
		if a.state != nil && a.style.ScrollY {
			from = append(from, start{a, a.state.scrollY})
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
	return e.grow.dh
}

// popoverRoom returns the height a popover next to a can take inside wr, keeping
// the 4 pixel margin popoverPosition keeps: below or above a, whichever is
// taller, for SideBottom and SideTop.
func popoverRoom(a rectF, o PopoverOptions, wr rectF) float32 {
	if o.Side == SideLeft || o.Side == SideRight {
		return max(wr.h-8, 0)
	}
	below := wr.h - (a.y + a.h + o.Gap) - 4
	above := a.y - o.Gap - 4
	return max(below, above, 0)
}

// popoverPosition places a popover of size cw×ch next to a, and returns the
// side it ended up on after flipping to fit.
func popoverPosition(a rectF, cw, ch float32, o PopoverOptions, wr rectF) (float32, float32, Side) {
	along := func(start, length, size float32) float32 {
		switch o.Align {
		case AlignCenter:
			return start + (length-size)/2 + o.AlignOffset
		case AlignEnd:
			return start + length - size - o.AlignOffset
		}
		return start + o.AlignOffset
	}
	var x, y float32
	side := o.Side
	switch o.Side {
	case SideTop, SideBottom:
		x = along(a.x, a.w, cw)
		below, above := a.y+a.h+o.Gap, a.y-o.Gap-ch
		y = below
		if o.Side == SideTop {
			y = above
		}
		if o.Side == SideBottom && below+ch > wr.h && above >= 0 {
			y, side = above, SideTop
		} else if o.Side == SideTop && above < 0 && below+ch <= wr.h {
			y, side = below, SideBottom
		}
	default:
		y = along(a.y, a.h, ch)
		right, left := a.x+a.w+o.Gap, a.x-o.Gap-cw
		x = right
		if o.Side == SideLeft {
			x = left
		}
		if o.Side == SideRight && right+cw > wr.w && left >= 0 {
			x, side = left, SideLeft
		} else if o.Side == SideLeft && left < 0 && right+cw <= wr.w {
			x, side = right, SideRight
		}
	}
	x = min(max(x, 4), max(wr.w-cw-4, 4))
	y = min(max(y, 4), max(wr.h-ch-4, 4))
	return x, y, side
}

// animate advances an element's track toward target and returns its value.
func (w *Window) animate(s *elState, p animProp, n int, eps float32, target [4]float32, m motion.Spec) [4]float32 {
	if (p == animRect || p == animScroll || p == animXform || p == animHeight) && reducedMotion.Load() {
		m = motion.Instant
	}
	tr := s.track(p, n, eps)
	v := tr.Step(target, m, w.dt)
	if tr.Moving() {
		w.animating = true
	}
	return v
}

func (w *Window) place(e *El, ox, oy float32, clip rectF) {
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
	r := rectF{e.absX, e.absY, ew, eh}
	vis := r.intersect(clip)
	if vis.empty() && (e.clip || !hasAbsoluteChild(e)) {
		w.placeCulled(e)
		return
	}
	e.culled = false
	e.clipR = clip
	if e.onLayout != nil && (n.W != s.layoutW || n.H != s.layoutH || !s.laidOut) {
		s.layoutW, s.layoutH, s.laidOut = n.W, n.H, true
		e.onLayout(n.W, n.H)
	}
	h := hit{el: e, x: r.x, y: r.y, w: r.w, h: r.h, clip: clip, radii: e.radii}
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
	if e.clip || e.grow.clip {
		in := w.innerBox(e, w.snap(r))
		s := w.scale
		childClip = vis.intersect(rectF{in[0] / s, in[1] / s, in[2] / s, in[3] / s})
	}
	e.kidClip = childClip
	if e.virtual != nil {
		w.virtualContent(e)
		if e.virtual.motion != nil {
			// Only here, once a frame, so building the view does not step it too.
			w.stepRowHeights(e)
			w.virtualContent(e)
		}
		if w.revealKey != "" && e.key == w.revealKey {
			s.reveal, s.revealing, s.revealEdge = w.revealRow, true, revealUndecided
			if w.revealTop {
				s.revealEdge = revealTop
			}
			s.revealInstant = w.revealInstant
			w.revealKey = ""
		}
	}
	if w.bottomKey != "" && e.key == w.bottomKey && s != nil && e.style.ScrollY {
		s.toBottom = true
		w.bottomKey = ""
	}
	if s != nil && e.kind != kindInput {
		w.stepScroll(e)
	}
	if e.virtual != nil {
		// A measured list may move the scroll offset to keep its rows in place.
		w.virtualRows(e)
	}
	if s != nil && s.toBottom {
		// A measured list has measured its last rows by now.
		s.toBottom = false
		from := s.scrollY
		w.moveScrollY(e, max(n.ContentH-n.H, 0))
		w.startGlide(e, from, false)
		s.bottomGlide = s.glide
		if e.virtual != nil && e.virtual.estimate == 0 {
			w.virtualRows(e)
		}
		if e.onScroll != nil && s.scrollY != from {
			e.onScroll(s.scrollX, s.scrollY)
			w.markDirty(dirtyScroll)
		}
	}
	if e.stickBottom && s != nil {
		s.endY = max(n.ContentH-n.H, 0)
	}
	cx, cy := e.absX, e.absY
	if s != nil && e.kind != kindInput {
		cx -= s.scrollX
		cy -= s.scrollY
	}
	for _, c := range e.children {
		w.place(c, cx, cy, childClip)
	}
	if e.style.ScrollY && n.ContentH > n.H+0.5 {
		w.placeScrollBar(e, vis, true)
	}
	if e.style.ScrollX && n.ContentW > n.W+0.5 {
		w.placeScrollBar(e, vis, false)
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
		cx -= s.scrollX
		cy -= s.scrollY
	}
	for _, c := range e.children {
		c.culled = true
		if c.style.Hidden {
			continue
		}
		n := &c.node
		c.absX, c.absY, c.w, c.h = cx+n.X, cy+n.Y, n.W, n.H
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
	animScroll
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
