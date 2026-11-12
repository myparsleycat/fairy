package fairy

import (
	"github.com/myparsleycat/fairy/internal/render"
	"github.com/myparsleycat/fairy/motion"
)

// collectGhosts keeps elements with an Exit transition drawn after they leave the tree.
func (w *Window) collectGhosts() {
	if w.gen <= 1 {
		return
	}
	for id, old := range w.prevByID {
		if old.exit == nil || old.culled || w.byID[id] != nil {
			continue
		}
		if c := w.matchClaims[old.matchID]; old.matchID != "" && c != 0 && c != id {
			continue // another element took its id and flies from its place
		}
		var parent uint64 // 0: a removed layer root, drawn above the layers
		if old.parent != nil {
			if w.byID[old.parent.id] == nil {
				continue // its parent left too; it goes with it
			}
			parent = old.parent.id
		}
		g := newGhost(old)
		g.parent = parent
		if old.parent != nil {
			g.next = nextStayingSibling(old, w.byID)
		}
		g.subs = exitingInside(old, nil)
		w.ghosts = append(w.ghosts, g)
	}
	if w.ghostIn == nil {
		w.ghostIn = map[uint64]bool{}
	}
	clear(w.ghostIn)
	for _, g := range w.ghosts {
		w.ghostIn[g.parent] = true
	}
}

func nextStayingSibling(e *El, live map[uint64]*El) uint64 {
	after := false
	for _, c := range e.parent.children {
		switch {
		case c == e:
			after = true
		case after && live[c.id] != nil:
			return c.id
		}
	}
	return 0
}

func newGhost(e *El) *ghost {
	tr := motion.NewTrack(4, 0.002)
	tr.Set([4]float32{e.fade, 0, 0, 1})
	return &ghost{el: e, tr: tr, to: e.exit.sided(e.popSide), alpha: e.fade}
}

// exitingInside appends the elements inside e that have an Exit transition.
func exitingInside(e *El, subs []*ghost) []*ghost {
	for _, c := range e.children {
		if c.culled {
			continue
		}
		if c.exit != nil {
			subs = append(subs, newGhost(c))
		}
		subs = exitingInside(c, subs)
	}
	return subs
}

type ghost struct {
	el     *El
	parent uint64
	// next is the first sibling after it that stayed, which is drawn above
	// it; 0 when none did.
	next   uint64
	tr     *motion.Track
	to     Transition
	dx, dy float32
	alpha  float32
	// subs are elements inside el with an Exit of their own, which runs
	// alongside el's.
	subs []*ghost
}

// matchRec is where an element with a MatchID was last placed.
type matchRec struct {
	r     rectF // as drawn, in flight or not
	el    uint64
	pass  int
	frame int
}

// flight is an element drawn after the rest of the window.
type flight struct {
	el      *El
	opacity float32
	color   Color
	clip    uint32 // the rounded clip it was met under
}

// placeFlight starts a MatchID element's flight when it takes its id from
// another element, and adds the flight to its own transform while it lasts.
func (w *Window) placeFlight(e *El) {
	e.flying = false
	s := e.state
	if s == nil {
		return
	}
	tr := s.track(animFlight, 4, 0.002)
	if !tr.Initialized() {
		tr.Set([4]float32{0, 0, 1, 1})
	}
	rec, ok := w.matched[e.matchID]
	if ok && rec.el != e.id && rec.pass != w.placePass && rec.frame >= w.frame-1 && e.w > 0 && e.h > 0 &&
		(e.parent == nil || e.parent.xf.identity()) && !reducedMotion.Load() {
		tr.Set([4]float32{rec.r.x - e.absX, rec.r.y - e.absY, rec.r.w / e.w, rec.r.h / e.h})
	}
	m := motion.Default
	if e.layoutMotion != nil {
		m = *e.layoutMotion
	}
	v := w.animate(s, animFlight, 4, 0.002, [4]float32{0, 0, 1, 1}, m)
	if w.matched == nil {
		w.matched = map[string]matchRec{}
	}
	w.matched[e.matchID] = matchRec{r: rectF{e.absX + v[0], e.absY + v[1], e.w * v[2], e.h * v[3]}, el: e.id, pass: w.placePass, frame: w.frame}
	if !tr.Moving() {
		return
	}
	e.flying = true
	f := xform{a: v[2], d: v[3], tx: e.absX - v[2]*e.absX + v[0], ty: e.absY - v[3]*e.absY + v[1]}
	if e.xfOwn {
		f = f.then(e.ownXf)
	}
	e.ownXf, e.xfOwn = f, true
}

func (w *Window) paintFlights() bool {
	ok := true
	w.paintingFlight = true
	for _, f := range w.flights {
		w.scene.Clip = f.clip
		ok = w.paint(f.el, f.opacity, f.color) && ok
	}
	w.paintingFlight = false
	w.scene.Clip = 0
	clear(w.flights)
	w.flights = w.flights[:0]
	return ok
}

// placeTransform works out the element's own scale and turn, springing
// toward Scale, Rotate and PressScale and multiplied by the scale of its
// enter transition, and the map from its layout space to the window. The
// press scale is drawn but left out of that map, so hit testing keeps the
// resting size.
func (w *Window) placeTransform(e *El, enterScale float32) {
	e.xf, e.xfOwn, e.xfK, e.xfRot = identityXform, false, 1, 0
	if e.parent != nil {
		e.xf = e.parent.xf
	}
	k, rot, press := float32(1), float32(0), float32(1)
	var pdx, pdy float32
	ox, oy := e.origin()
	if p := e.xfp; p != nil {
		k, rot = p.scale, p.rotate
		held := w.pressed(e) && !reducedMotion.Load()
		if p.pressScale > 0 && held {
			press = p.pressScale
		}
		if s := e.state; s != nil {
			m := motion.Default
			switch {
			case p.motion != nil:
				m = *p.motion
			case e.layoutMotion != nil:
				m = *e.layoutMotion
			case p.pressScale > 0:
				m = motion.Snappy
			}
			v := w.animate(s, animXform, 3, 0.0005, [4]float32{k, rot, press}, m)
			k, rot, press = max(v[0], 0), v[1], max(v[2], 0)
			if p.pressX != 0 || p.pressY != 0 {
				var to [4]float32
				if held {
					to = [4]float32{p.pressX, p.pressY}
				}
				v := w.animate(s, animPress, 2, 0.01, to, motion.Fast)
				pdx, pdy = v[0], v[1]
			}
		}
	}
	k *= enterScale
	e.xfK, e.xfRot = k*press, rot
	if k*press == 1 && rot == 0 && pdx == 0 && pdy == 0 {
		return
	}
	px, py := e.absX+ox*e.w, e.absY+oy*e.h
	e.ownXf = turnAbout(px, py, k*press, rot)
	e.ownXf.tx += pdx
	e.ownXf.ty += pdy
	e.xfOwn = true
	if k != 1 || rot != 0 {
		e.xf = e.xf.then(turnAbout(px, py, k, rot))
	}
}

// transformPrims maps the prims drawn for e and its subtree, from start on,
// through e's own transform. Clips set inside the subtree move into the
// prims' own space (Clip2); the clip from e's ancestors stays where it is
// drawn.
func (w *Window) transformPrims(e *El, start int) {
	m := e.ownXf.scaled(w.scale)
	outer := w.snap(e.clipR)
	prims := w.scene.Prims[start:]
	// Prims drawn into a layer stay where they are; the layer's composite
	// prim carries the transform.
	skip := w.layerRanges[:0]
	for _, l := range w.scene.Layers {
		if l.Begin >= start && !l.Backdrop {
			skip = append(skip, [2]int{l.Begin - start, l.End - start})
		}
	}
	w.layerRanges = skip
	for i := range prims {
		inLayer := false
		for _, r := range skip {
			if i >= r[0] && i < r[1] {
				inLayer = true
				break
			}
		}
		if inLayer {
			continue
		}
		p := &prims[i]
		if p.Flags&render.FlagXform == 0 {
			p.Flags |= render.FlagXform
			p.Xform, p.Xlate = [4]float32{1, 0, 0, 1}, [2]float32{}
			p.Clip2 = render.NoClip
		}
		if p.Clip != outer {
			c := rectF{p.Clip2[0], p.Clip2[1], p.Clip2[2], p.Clip2[3]}.intersect(rectF{p.Clip[0], p.Clip[1], p.Clip[2], p.Clip[3]})
			p.Clip2, p.Clip = [4]float32{c.x, c.y, c.w, c.h}, outer
		}
		n := m.then(xform{p.Xform[0], p.Xform[1], p.Xform[2], p.Xform[3], p.Xlate[0], p.Xlate[1]})
		p.Xform, p.Xlate = [4]float32{n.a, n.b, n.c, n.d}, [2]float32{n.tx, n.ty}
	}
}

// stepGhosts advances exit transitions of removed elements. An element that
// comes back cancels its exit.
func (w *Window) stepGhosts() {
	keep := w.ghosts[:0]
	for _, g := range w.ghosts {
		if w.byID[g.el.id] != nil {
			continue
		}
		to := g.to.effective()
		v := g.tr.Step([4]float32{to.Opacity, to.X, to.Y, to.scale()}, to.Motion, w.dt)
		shiftTree(g.el, v[1]-g.dx, v[2]-g.dy)
		g.dx, g.dy = v[1], v[2]
		g.alpha = min(max(v[0], 0), 1)
		g.el.ghostTransform(max(v[3], 0))
		moving := g.tr.Moving()
		for _, sub := range g.subs {
			to := sub.to.effective()
			v := sub.tr.Step([4]float32{to.Opacity, to.X, to.Y, to.scale()}, to.Motion, w.dt)
			shiftTree(sub.el, v[1]-sub.dx, v[2]-sub.dy)
			sub.dx, sub.dy = v[1], v[2]
			sub.el.fade = min(max(v[0], 0), 1)
			sub.el.ghostTransform(max(v[3], 0))
			moving = moving || sub.tr.Moving()
		}
		if moving {
			w.animating = true
			keep = append(keep, g)
		}
	}
	clear(w.ghosts[len(keep):])
	w.ghosts = keep
}

// ghostTransform scales a leaving element by k about its origin, on top of
// the scale and turn it was last drawn with.
func (e *El) ghostTransform(k float32) {
	if k := e.xfK * k; k != 1 || e.xfRot != 0 {
		ox, oy := e.origin()
		e.ownXf = turnAbout(e.absX+ox*e.w, e.absY+oy*e.h, k, e.xfRot)
		e.xfOwn = true
	} else {
		e.xfOwn = false
	}
}

// paintGhostsOf draws the exiting children of parent that go just below its
// live child before, so they keep the stacking order they left in, as the
// front toast of a stack stays in front while it leaves. A nil before draws
// those that no live child of parent follows. Inside a parent they keep to
// the clip its children have now, as a parent that shrinks while they leave
// would otherwise show them past its edge.
func (w *Window) paintGhostsOf(parent, before *El, opacity float32, color Color) bool {
	ok := true
	var id uint64
	if parent != nil {
		id = parent.id
	}
	for _, g := range w.ghosts {
		if g.parent != id {
			continue
		}
		if before != nil {
			if g.next != before.id {
				continue
			}
		} else if n := w.byID[g.next]; g.next != 0 && n != nil && n.parent == parent {
			continue
		}
		start := len(w.scene.Prims)
		g.el.fade = 1
		ok = w.paint(g.el, opacity*g.alpha, color) && ok
		if parent == nil {
			continue
		}
		pc := w.snap(parent.kidClip)
		c := rectF{pc[0], pc[1], pc[2], pc[3]}
		for i := range w.scene.Prims[start:] {
			p := &w.scene.Prims[start+i]
			r := rectF{p.Clip[0], p.Clip[1], p.Clip[2], p.Clip[3]}.intersect(c)
			p.Clip = [4]float32{r.x, r.y, r.w, r.h}
		}
	}
	return ok
}

func shiftTree(e *El, dx, dy float32) {
	if dx == 0 && dy == 0 {
		return
	}
	e.absX += dx
	e.absY += dy
	for _, c := range e.children {
		shiftTree(c, dx, dy)
	}
}

// effective returns the transition to run, which only fades when motion is reduced.
func (t Transition) effective() Transition {
	if reducedMotion.Load() {
		return Transition{Opacity: t.Opacity, Motion: reducedFade}
	}
	return t
}

var reducedFade = motion.Ease(0.15)

// scale returns the transition's scale, 1 when unset.
func (t Transition) scale() float32 {
	if t.Scale == 0 || !finite(t.Scale) || t.Scale < 0 {
		return 1
	}
	return t.Scale
}
