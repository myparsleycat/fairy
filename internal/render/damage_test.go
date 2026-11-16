//go:build windows

package render

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

// A damage node is part of a random scene: a prim, or a clip, layer or
// backdrop around more nodes.
type damageNode struct {
	kind  int // nodePrim, nodeClip, nodeLayer, nodeBackdrop
	p     Prim
	tex   int // image prims: index into damageScene.texs, or -1
	glyph int // glyph prims: index into damageScene.glyphs, or -1
	clip  ClipRec
	sigma float32
	kids  []*damageNode
}

const (
	nodePrim = iota
	nodeClip
	nodeLayer
	nodeBackdrop
)

type damageGlyph struct {
	w, h int
	mask []byte
}

// damageScene is a random scene drawn by two renderers, one with damage
// tracking and one without, which must draw the same pixels.
type damageScene struct {
	t      *testing.T
	rnd    *rand.Rand
	w, h   int
	root   []*damageNode
	texs   []*Texture
	glyphs []damageGlyph
	tiles  [2][]Tile // per renderer
	clear  [4]float32
}

func (d *damageScene) color() [4]float32 {
	a := []float32{1, 1, 0.6, 0.25}[d.rnd.IntN(4)]
	return [4]float32{d.rnd.Float32() * a, d.rnd.Float32() * a, d.rnd.Float32() * a, a}
}

func (d *damageScene) box() [4]float32 {
	w, h := float32(4+d.rnd.IntN(d.w/2)), float32(4+d.rnd.IntN(d.h/2))
	return [4]float32{float32(d.rnd.IntN(d.w+8)) - w/2, float32(d.rnd.IntN(d.h+8)) - h/2, w, h}
}

func (d *damageScene) clipRect() [4]float32 {
	if d.rnd.IntN(3) > 0 {
		return NoClip
	}
	b := d.box()
	b[2], b[3] = b[2]*2, b[3]*2
	return b
}

func (d *damageScene) xform(p *Prim) {
	a := d.rnd.Float32()*0.8 - 0.4
	k := 0.6 + d.rnd.Float32()*0.8
	p.Flags |= FlagXform
	p.Xform = [4]float32{k, a, -a, k}
	p.Xlate = [2]float32{float32(d.rnd.IntN(d.w / 2)), float32(d.rnd.IntN(d.h / 2))}
	p.Clip2 = NoClip
	if d.rnd.IntN(2) == 0 {
		p.Clip2 = d.box()
	}
}

func (d *damageScene) prim() *damageNode {
	n := &damageNode{kind: nodePrim, tex: -1, glyph: -1}
	p := Prim{Bounds: d.box(), Clip: d.clipRect(), Color: d.color()}
	r := float32(d.rnd.IntN(8))
	p.Radii = [4]float32{r, r, r / 2, 0}
	switch d.rnd.IntN(8) {
	case 0, 1:
		p.Kind = KindRect
		if d.rnd.IntN(2) == 0 {
			p.Border = [4]float32{1, 2, 1, 2}
			p.BorderColor = d.color()
		}
		if d.rnd.IntN(3) == 0 {
			p.Flags |= FlagGradient
			p.Color2 = d.color()
		}
	case 2:
		p.Kind = KindShadow
		p.Params[0] = float32(1 + d.rnd.IntN(6))
		if d.rnd.IntN(3) == 0 {
			p.Flags |= FlagInset
			p.UV = p.Bounds
			p.Border = p.Radii
		}
	case 3:
		p.Kind = KindImage
		p.Flags |= FlagPremultiplied
		p.Color = [4]float32{1, 1, 1, p.Color[3]}
		p.UV = [4]float32{0, 0, 1, 1}
		n.tex = d.rnd.IntN(len(d.texs))
	case 4:
		p.Kind = KindGlyph
		n.glyph = d.rnd.IntN(len(d.glyphs))
		g := d.glyphs[n.glyph]
		p.Bounds[2], p.Bounds[3] = float32(g.w), float32(g.h)
	case 5:
		p.Kind = KindCallout
		p.Params = [4]float32{p.Bounds[0] + p.Bounds[2]/2, p.Bounds[1] - 3, 4, 1}
	case 6:
		p.Kind = KindArc
		p.Params = [4]float32{d.rnd.Float32() * 6, 1 + d.rnd.Float32()*5, 2, float32(d.rnd.IntN(2))}
	default:
		p.Kind = KindRect
	}
	if d.rnd.IntN(5) == 0 {
		d.xform(&p)
	}
	n.p = p
	return n
}

func (d *damageScene) container(depth int) *damageNode {
	n := &damageNode{kind: nodeClip + d.rnd.IntN(3)}
	switch n.kind {
	case nodeClip:
		r := float32(d.rnd.IntN(10))
		n.clip = ClipRec{Rect: d.box(), Radii: [4]float32{r, r, r, r}}
		n.clip.Rect[2], n.clip.Rect[3] = n.clip.Rect[2]*1.5, n.clip.Rect[3]*1.5
		if d.rnd.IntN(3) == 0 {
			n.clip.Feather = [4]float32{2, 2, 2, 2}
		}
	case nodeLayer:
		n.sigma = []float32{0, 0, 1.5, 4}[d.rnd.IntN(4)]
		n.p = Prim{Clip: d.clipRect(), Color: [4]float32{1, 1, 1, 0.5 + d.rnd.Float32()/2}}
	case nodeBackdrop:
		n.sigma = float32(1 + d.rnd.IntN(5))
		n.p = Prim{Bounds: d.box(), Clip: d.clipRect(), Color: [4]float32{1, 1, 1, 0.5 + d.rnd.Float32()/2}}
		r := float32(d.rnd.IntN(8))
		n.p.Radii = [4]float32{r, r, r, r}
		if d.rnd.IntN(5) == 0 {
			d.xform(&n.p)
		}
	}
	for range d.rnd.IntN(4) {
		n.kids = append(n.kids, d.child(n, depth+1))
	}
	return n
}

// child makes a node to go inside parent, nil for the root. As in paint, a
// backdrop's range holds only the element's own prims, such as its shadows.
func (d *damageScene) child(parent *damageNode, depth int) *damageNode {
	if parent != nil && parent.kind == nodeBackdrop {
		return d.prim()
	}
	return d.node(depth)
}

func (d *damageScene) node(depth int) *damageNode {
	if depth < 3 && d.rnd.IntN(4) == 0 {
		return d.container(depth)
	}
	return d.prim()
}

// nodeList is a list of nodes in the tree and the node holding it, nil for
// the root.
type nodeList struct {
	parent *damageNode
	nodes  *[]*damageNode
}

// lists returns every list of nodes in the tree, the root first.
func (d *damageScene) lists() []nodeList {
	out := []nodeList{{nil, &d.root}}
	var walk func(ns []*damageNode)
	walk = func(ns []*damageNode) {
		for _, n := range ns {
			if n.kind != nodePrim {
				out = append(out, nodeList{n, &n.kids})
				walk(n.kids)
			}
		}
	}
	walk(d.root)
	return out
}

func (d *damageScene) pick() *damageNode {
	var all []*damageNode
	var walk func(ns []*damageNode)
	walk = func(ns []*damageNode) {
		for _, n := range ns {
			all = append(all, n)
			walk(n.kids)
		}
	}
	walk(d.root)
	if len(all) == 0 {
		return nil
	}
	return all[d.rnd.IntN(len(all))]
}

// mutate changes the scene a little, as one frame of an app might.
func (d *damageScene) mutate(r [2]*Renderer) {
	switch op := d.rnd.IntN(16); {
	case op < 4: // recolor
		if n := d.pick(); n != nil && n.kind == nodePrim {
			n.p.Color = d.color()
		} else if n != nil && n.kind != nodeClip {
			n.p.Color[3] = 0.4 + d.rnd.Float32()*0.6
		}
	case op < 6: // move
		if n := d.pick(); n != nil {
			dx, dy := float32(d.rnd.IntN(9)-4), float32(d.rnd.IntN(9)-4)
			switch n.kind {
			case nodeClip:
				n.clip.Rect[0] += dx
				n.clip.Rect[1] += dy
			default:
				n.p.Bounds[0] += dx
				n.p.Bounds[1] += dy
				n.p.Xlate[0] += dx
			}
		}
	case op < 8: // insert
		ls := d.lists()
		l := ls[d.rnd.IntN(len(ls))]
		at := d.rnd.IntN(len(*l.nodes) + 1)
		*l.nodes = append((*l.nodes)[:at], append([]*damageNode{d.child(l.parent, 1)}, (*l.nodes)[at:]...)...)
	case op < 10: // delete
		ls := d.lists()
		if l := ls[d.rnd.IntN(len(ls))].nodes; len(*l) > 0 {
			at := d.rnd.IntN(len(*l))
			*l = append((*l)[:at], (*l)[at+1:]...)
		}
	case op == 10: // swap neighbors
		ls := d.lists()
		if l := ls[d.rnd.IntN(len(ls))].nodes; len(*l) > 1 {
			at := d.rnd.IntN(len(*l) - 1)
			(*l)[at], (*l)[at+1] = (*l)[at+1], (*l)[at]
		}
		d.crossMark()
	case op == 11: // a container's settings
		if n := d.pick(); n != nil && n.kind != nodePrim {
			switch n.kind {
			case nodeClip:
				n.clip.Radii[0] = float32(d.rnd.IntN(12))
			case nodeLayer:
				n.sigma = []float32{0, 1, 3}[d.rnd.IntN(3)]
			case nodeBackdrop:
				n.sigma = float32(1 + d.rnd.IntN(5))
				n.p.Bounds[2] += float32(d.rnd.IntN(9) - 4)
			}
		}
	case op == 12: // new pixels in a texture
		t := d.texs[d.rnd.IntN(len(d.texs))]
		t.Update(r[0].ctx, d.pixels(t.Width, t.Height))
	case op == 13: // mipmaps
		t := d.texs[d.rnd.IntN(len(d.texs))]
		if _, err := t.PromoteMips(r[0].ds); err != nil {
			d.t.Fatal(err)
		}
	case op == 14: // the atlas grows, so glyphs move
		if d.rnd.IntN(4) == 0 {
			for i := range r {
				if err := r[i].Atlas.Grow(); err != nil {
					d.t.Fatal(err)
				}
			}
			d.insertGlyphs(r)
		}
	default: // nothing changes
	}
}

// crossMark moves a prim across a backdrop's mark, into its range from just
// before it or out of it to just before it, leaving the prims in order.
func (d *damageScene) crossMark() {
	for _, l := range d.lists() {
		ns := *l.nodes
		for i, b := range ns {
			if b.kind != nodeBackdrop {
				continue
			}
			if i > 0 && ns[i-1].kind == nodePrim && d.rnd.IntN(2) == 0 {
				b.kids = append([]*damageNode{ns[i-1]}, b.kids...)
				*l.nodes = append(ns[:i-1], ns[i:]...)
				return
			}
			if len(b.kids) > 0 {
				*l.nodes = append(ns[:i], append([]*damageNode{b.kids[0]}, ns[i:]...)...)
				b.kids = b.kids[1:]
				return
			}
		}
	}
}

func (d *damageScene) pixels(w, h int) []byte {
	px := make([]byte, w*h*4)
	for i := 0; i < len(px); i += 4 {
		a := byte(d.rnd.IntN(2) * 255)
		px[i], px[i+1], px[i+2], px[i+3] = byte(d.rnd.IntN(int(a)+1)), byte(d.rnd.IntN(int(a)+1)), 0, a
	}
	return px
}

func (d *damageScene) insertGlyphs(r [2]*Renderer) {
	for i := range r {
		d.tiles[i] = d.tiles[i][:0]
		for _, g := range d.glyphs {
			t, ok := r[i].Atlas.Insert(g.w, g.h, g.mask)
			if !ok {
				d.t.Fatal("glyph does not fit the atlas")
			}
			d.tiles[i] = append(d.tiles[i], t)
		}
	}
}

func (d *damageScene) build(s *Scene, ri int, r *Renderer) {
	s.Reset()
	var add func(ns []*damageNode)
	add = func(ns []*damageNode) {
		for _, n := range ns {
			switch n.kind {
			case nodePrim:
				p := n.p
				switch {
				case n.tex >= 0:
					s.AddImage(p, d.texs[n.tex])
				case n.glyph >= 0:
					t, size := d.tiles[ri][n.glyph], float32(r.Atlas.Size())
					p.UV = [4]float32{float32(t.X) / size, float32(t.Y) / size, float32(t.W) / size, float32(t.H) / size}
					s.Add(p)
				default:
					s.Add(p)
				}
			case nodeClip:
				prev := s.PushClip(n.clip)
				add(n.kids)
				s.Clip = prev
			case nodeLayer:
				mark := s.BeginLayer()
				add(n.kids)
				s.EndLayer(mark, n.p, n.sigma)
			case nodeBackdrop:
				mark := s.BeginBackdrop()
				add(n.kids)
				s.EndBackdrop(mark, n.p, n.sigma)
			}
		}
	}
	add(d.root)
}

// TestDamageMatchesFullRedraw draws random scene sequences with damage
// tracking and without, and requires the same pixels after every frame.
func TestDamageMatchesFullRedraw(t *testing.T) {
	const w, h = 160, 96
	dev := warpDevice(t)
	newRenderer := func() *Renderer {
		r := &Renderer{ds: dev, dev: dev.Dev, ctx: dev.Ctx, width: w, height: h}
		t.Cleanup(func() { r.ds = nil; r.Release() })
		var err error
		if r.target, err = NewTarget(dev, w, h); err != nil {
			t.Fatal(err)
		}
		r.rtv = r.target.rtv
		if err := r.init(); err != nil {
			t.Fatal(err)
		}
		return r
	}
	var partial, multi, unchanged int
	for seed := range uint64(24) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			rs := [2]*Renderer{newRenderer(), newRenderer()}
			if !rs[0].SetDamageTracking(true) {
				t.Fatal("damage tracking unavailable")
			}
			d := &damageScene{t: t, rnd: rand.New(rand.NewPCG(seed, 7)), w: w, h: h, clear: [4]float32{0.9, 0.9, 0.92, 1}}
			for range 3 {
				tex, err := NewTexture(dev, 16, 16, d.pixels(16, 16), true)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(tex.Release)
				d.texs = append(d.texs, tex)
			}
			for range 4 {
				g := damageGlyph{w: 3 + d.rnd.IntN(8), h: 4 + d.rnd.IntN(8)}
				g.mask = make([]byte, g.w*g.h)
				for i := range g.mask {
					g.mask[i] = byte(d.rnd.IntN(256))
				}
				d.glyphs = append(d.glyphs, g)
			}
			d.insertGlyphs(rs)
			for range 12 {
				d.root = append(d.root, d.node(0))
			}
			var scenes [2]Scene
			bufs := [2][]byte{make([]byte, w*h*4), make([]byte, w*h*4)}
			for step := range 40 {
				if step > 0 {
					for range 1 + d.rnd.IntN(2) {
						d.mutate(rs)
					}
					if d.rnd.IntN(20) == 0 {
						d.clear = d.color()
					}
				}
				for i, r := range rs {
					d.build(&scenes[i], i, r)
					if err := r.Draw(&scenes[i], d.clear); err != nil {
						t.Fatal(err)
					}
					if err := r.ReadPixels(bufs[i]); err != nil {
						t.Fatal(err)
					}
				}
				st := rs[0].Stats()
				if st.DamageReason == "" {
					if st.Damage.Empty() {
						unchanged++
					} else {
						partial++
					}
					if st.DamageRects > 1 {
						multi++
					}
				}
				if !bytes.Equal(bufs[0], bufs[1]) {
					x, y := firstDiff(bufs[0], bufs[1], w)
					t.Fatalf("step %d: pixel (%d, %d) differs from a full redraw; damage %v reason %q",
						step, x, y, st.Damage, st.DamageReason)
				}
			}
		})
	}
	if partial == 0 || multi == 0 || unchanged == 0 {
		t.Fatalf("%d frames drawn in part, %d of them in several rects, and %d unchanged, want some of each",
			partial, multi, unchanged)
	}
	t.Logf("%d frames drawn in part, %d of them in several rects, %d unchanged", partial, multi, unchanged)
}

// A prim that moves from before a backdrop's mark to after it leaves every
// prim as it was, but the backdrop no longer blurs it.
func TestDamageBackdropMarkMove(t *testing.T) {
	const w, h = 64, 32
	rs := [2]*Renderer{backdropRenderer(t, w, h), backdropRenderer(t, w, h)}
	if !rs[0].SetDamageTracking(true) {
		t.Fatal("damage tracking unavailable")
	}
	red := Prim{Bounds: [4]float32{20, 8, 8, 16}, Clip: NoClip, Color: [4]float32{1, 0, 0, 1}, Kind: KindRect}
	box := Prim{Bounds: [4]float32{8, 4, 48, 24}, Clip: NoClip, Color: [4]float32{1, 1, 1, 1}}
	bufs := [2][]byte{make([]byte, w*h*4), make([]byte, w*h*4)}
	for frame, before := range []bool{true, false} {
		for i, r := range rs {
			var s Scene
			mark := s.BeginBackdrop()
			if before {
				s.Prims = append(s.Prims, red)
				mark = s.BeginBackdrop()
			} else {
				s.Add(red)
			}
			s.EndBackdrop(mark, box, 4)
			if err := r.Draw(&s, [4]float32{0, 0, 0, 1}); err != nil {
				t.Fatal(err)
			}
			if err := r.ReadPixels(bufs[i]); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(bufs[0], bufs[1]) {
			x, y := firstDiff(bufs[0], bufs[1], w)
			t.Fatalf("frame %d: pixel (%d, %d) differs from a full redraw; damage %v", frame, x, y, rs[0].Stats().Damage)
		}
	}
}

// A target replaced by a resize holds none of the frame drawn before it, even
// when it is back at the size that frame was drawn at.
func TestDamageResizeBack(t *testing.T) {
	const w, h = 64, 32
	rs := [2]*Renderer{backdropRenderer(t, w, h), backdropRenderer(t, w, h)}
	if !rs[0].SetDamageTracking(true) {
		t.Fatal("damage tracking unavailable")
	}
	var s Scene
	s.Add(Prim{Bounds: [4]float32{4, 4, 40, 20}, Clip: NoClip, Color: [4]float32{1, 0, 0, 1}, Kind: KindRect})
	if err := rs[0].Draw(&s, [4]float32{0, 0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{2 * w, w} {
		if err := rs[0].Resize(width, h); err != nil {
			t.Fatal(err)
		}
	}
	bufs := [2][]byte{make([]byte, w*h*4), make([]byte, w*h*4)}
	for i, r := range rs {
		if err := r.Draw(&s, [4]float32{0, 0, 0, 1}); err != nil {
			t.Fatal(err)
		}
		if err := r.ReadPixels(bufs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(bufs[0], bufs[1]) {
		x, y := firstDiff(bufs[0], bufs[1], w)
		t.Fatalf("pixel (%d, %d) differs from a full redraw; damage %v reason %q",
			x, y, rs[0].Stats().Damage, rs[0].Stats().DamageReason)
	}
}

func firstDiff(a, b []byte, w int) (int, int) {
	for i := range a {
		if a[i] != b[i] {
			return i / 4 % w, i / 4 / w
		}
	}
	return -1, -1
}
