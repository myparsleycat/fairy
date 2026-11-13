//go:build darwin

package render

import (
	"slices"
	"sort"
)

// layerPlan is how the renderer draws one layer this frame.
type layerPlan struct {
	src          *Target // the layer drawn, or the backdrop copied
	h1, h2       *Target // horizontal and vertical blur passes at half size
	out          *Texture
	blurH, blurV int // indices of the blur prims in r.frame
	erase        int // a backdrop's prim in r.frame that clears its box before the composite
	kids         []int
	skip         bool // a backdrop left out: nothing is copied and its composite draws nothing
}

// planLayers takes targets for the scene's layers, points each composite
// prim at its texture and appends the blur prims to r.frame.
func (r *Renderer) planLayers(s *Scene) ([]int, error) {
	r.plan = r.plan[:0]
	var top []int
	for range s.Layers {
		r.plan = append(r.plan, layerPlan{})
	}
	budget := float32(backdropBudget * r.width * r.height)
	// Backdrops are drawn one after another, each composited before the next
	// copies, so a later backdrop takes the targets of earlier ones again.
	var spare []*Target
	acquire := func(backdrop bool, w, h int) (*Target, error) {
		if backdrop {
			w, h := targetSize(w, h)
			for i, t := range spare {
				if t.Width == w && t.Height == h {
					spare = slices.Delete(spare, i, i+1)
					return t, nil
				}
			}
		}
		return r.pool.acquire(r.ds, w, h)
	}
	for i := range s.Layers {
		l := &s.Layers[i]
		parent := -1
		for j := i + 1; j < len(s.Layers); j++ {
			if p := &s.Layers[j]; !p.Backdrop && p.Begin <= l.Begin && l.End < p.End {
				parent = j
				break
			}
		}
		if parent < 0 {
			top = append(top, i)
		} else {
			r.plan[parent].kids = append(r.plan[parent].kids, i)
		}
		pl := &r.plan[i]
		var box [4]float32
		if l.Backdrop {
			target := [4]float32{0, 0, float32(r.width), float32(r.height)}
			if parent >= 0 {
				target = s.Layers[parent].Bounds
			}
			var ok bool
			box, ok = backdropRegion(l, &s.Prims[l.End], target)
			area := l.Bounds[2] * l.Bounds[3]
			if !ok || area > budget {
				pl.skip, pl.out = true, r.emptyImage
				continue
			}
			budget -= area
		}
		bw, bh := int(l.Bounds[2]), int(l.Bounds[3])
		src, err := acquire(l.Backdrop, bw, bh)
		if err != nil {
			return nil, err
		}
		pl.src = src
		uw, uh := float32(bw)/float32(src.Width), float32(bh)/float32(src.Height)
		pl.out = &src.Texture
		// kx and ky are the output's texels per device pixel.
		kx, ky := float32(1), float32(1)
		if l.Sigma > 0 {
			hw, hh := max((bw+1)/2, 1), max((bh+1)/2, 1)
			if pl.h1, err = acquire(l.Backdrop, hw, hh); err != nil {
				return nil, err
			}
			if pl.h2, err = acquire(l.Backdrop, hw, hh); err != nil {
				return nil, err
			}
			pl.blurH = len(r.frame)
			r.frame = append(r.frame, blurPrim(hw, hh, uw, uh, &src.Texture, l.Sigma, true))
			pl.blurV = len(r.frame)
			r.frame = append(r.frame, blurPrim(hw, hh, float32(hw)/float32(pl.h1.Width), float32(hh)/float32(pl.h1.Height), &pl.h1.Texture, l.Sigma/2, false))
			pl.out = &pl.h2.Texture
			uw, uh = float32(hw)/float32(pl.h2.Width), float32(hh)/float32(pl.h2.Height)
			kx, ky = float32(hw)/float32(bw), float32(hh)/float32(bh)
		}
		if l.Backdrop {
			ow, oh := float32(pl.out.Width), float32(pl.out.Height)
			r.frame[l.End].UV = [4]float32{
				(box[0] - l.Bounds[0]) * kx / ow, (box[1] - l.Bounds[1]) * ky / oh, box[2] * kx / ow, box[3] * ky / oh,
			}
			// The same box as a plain rect covers exactly the pixels the composite does.
			e := r.frame[l.End]
			e.Kind, e.Flags, e.UV = KindRect, e.Flags&^FlagPremultiplied, [4]float32{}
			pl.erase = len(r.frame)
			r.frame = append(r.frame, e)
			spare = append(spare, pl.src, pl.h1, pl.h2)
		} else {
			r.frame[l.End].UV = [4]float32{0, 0, uw, uh}
		}
	}
	return top, nil
}

// drawRange draws prims begin to end into the current target, drawing the
// layers among them, listed in kids, offscreen first.
func (r *Renderer) drawRange(s *Scene, begin, end int, kids []int) {
	i := begin
	for _, k := range kids {
		l := &s.Layers[k]
		if l.Backdrop {
			r.drawSeg(s, i, l.Begin)
			i = l.Begin
			if pl := &r.plan[k]; !pl.skip {
				r.backdrop(s, k)
				r.drawSeg(s, l.Begin, l.End)
				r.composite(pl, l.End)
				i = l.End + 1
			}
			continue
		}
		r.drawSeg(s, i, l.Begin)
		saved := r.cur
		pl := &r.plan[k]
		r.setTarget(r.targetOf(pl.src, [2]float32{l.Bounds[0], l.Bounds[1]}))
		r.clearCur([4]float32{})
		r.drawRange(s, l.Begin, l.End, pl.kids)
		if l.Sigma > 0 {
			r.blur(pl)
		}
		r.setTarget(saved)
		i = l.End
	}
	r.drawSeg(s, i, end)
}

// backdrop copies what the current target holds under layer k and blurs it.
func (r *Renderer) backdrop(s *Scene, k int) {
	l, pl := &s.Layers[k], &r.plan[k]
	cur := r.cur
	r.setImage(r.emptyImage.tex)
	r.clearTexture(pl.src.tex)
	x0, y0 := int(l.Bounds[0]-cur.origin[0]), int(l.Bounds[1]-cur.origin[1])
	x1, y1 := x0+int(l.Bounds[2]), y0+int(l.Bounds[3])
	cx0, cy0, cx1, cy1 := max(x0, 0), max(y0, 0), min(x1, cur.w), min(y1, cur.h)
	if cx1 > cx0 && cy1 > cy0 {
		r.copyTexture(cur.tex, cx0, cy0, cx1-cx0, cy1-cy0, pl.src.tex, cx0-x0, cy0-y0)
	}
	r.blur(pl)
	r.setTarget(cur)
}

// composite draws a backdrop's blur, the prim at index at, in place of what
// is under its box rather than over it: the box is first cleared by its
// coverage and then the blur is added. Drawn over, a translucent frame, as
// under a system backdrop, would show its sharp pixels through the blur.
func (r *Renderer) composite(pl *layerPlan, at int) {
	r.setBlend(blendErase)
	r.drawBatch(pl.erase, pl.erase+1)
	r.setBlend(blendAdd)
	r.setImage(pl.out.tex)
	r.drawBatch(at, at+1)
	r.setImage(r.emptyImage.tex)
	r.setBlend(blendOver)
}

// blur runs the two blur passes of a layer.
func (r *Renderer) blur(pl *layerPlan) {
	for _, pass := range [2]struct {
		dst, src *Target
		prim     int
	}{{pl.h1, pl.src, pl.blurH}, {pl.h2, pl.h1, pl.blurV}} {
		r.setTarget(r.targetOf(pass.dst, [2]float32{}))
		r.clearCur([4]float32{})
		r.setImage(pass.src.tex)
		r.drawBatch(pass.prim, pass.prim+1)
	}
	r.setImage(r.emptyImage.tex)
}

func (r *Renderer) targetOf(t *Target, origin [2]float32) renderTarget {
	return renderTarget{tex: t.tex, w: t.Width, h: t.Height, origin: origin}
}

// drawSeg draws prims a to b, binding each image prim's texture.
func (r *Renderer) drawSeg(s *Scene, a, b int) {
	if b <= a {
		return
	}
	refs := s.images[sort.Search(len(s.images), func(i int) bool { return s.images[i].index >= a }):]
	empty := r.emptyImage.tex
	bound := empty
	r.setImage(bound)
	start := a
	for _, ref := range refs {
		if ref.index >= b {
			break
		}
		tex := empty
		if ref.layer > 0 {
			tex = r.plan[ref.layer-1].out.tex
		} else if ref.tex != nil {
			if tex = ref.tex.resource(); tex == 0 {
				tex = empty
			}
		}
		if tex == bound {
			continue
		}
		if ref.index > start {
			r.drawBatch(start, ref.index)
			start = ref.index
		}
		bound = tex
		r.setImage(bound)
	}
	r.drawBatch(start, b)
	r.setImage(empty)
}
