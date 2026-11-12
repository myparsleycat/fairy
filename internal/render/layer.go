//go:build windows

package render

import (
	"math"
	"slices"
	"sort"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
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

// renderTarget is where draws go.
type renderTarget struct {
	rtv    d3d.RenderTargetView
	tex    func() (d3d.Texture2D, func())
	w, h   int
	origin [2]float32
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

// backdropRegion sets the bounds of backdrop l to what it copies: the part of
// target its composite prim p shows in, with room for the blur around it. It
// returns the box p is drawn over, and false when none of p shows.
func backdropRegion(l *Layer, p *Prim, target [4]float32) ([4]float32, bool) {
	box, shown := p.Bounds, p.Bounds
	if p.Flags&FlagXform != 0 {
		box = xformBounds(p.Bounds, p.Xform, p.Xlate)
		shown = xformBounds(intersect(p.Bounds, p.Clip2), p.Xform, p.Xlate)
	}
	shown = intersect(shown, p.Clip)
	if shown[2] <= 0 || shown[3] <= 0 {
		return box, false
	}
	m := float32(math.Ceil(float64(3 * l.Sigma)))
	b := wholeRect(intersect([4]float32{shown[0] - m, shown[1] - m, shown[2] + 2*m, shown[3] + 2*m}, target))
	b[2], b[3] = min(b[2], MaxLayerSize), min(b[3], MaxLayerSize)
	l.Bounds = b
	return box, b[2] >= 1 && b[3] >= 1
}

// blurPrim is a pass that fills a w x h target with the source region
// (0, 0, uw, uh) of src, blurred along one axis by sigma texels.
func blurPrim(w, h int, uw, uh float32, src *Texture, sigma float32, horizontal bool) Prim {
	size := src.Height
	if horizontal {
		size = src.Width
	}
	// Taps reach 3 sigma, at most 64 to a side; wider blurs space them out.
	step := max(float32(math.Ceil(float64(3*sigma/64))), 1)
	d := step / float32(size)
	// Taps stay on the region's texels, so its edge pixels repeat instead of
	// fading into the cleared texels past it.
	hx, hy := 0.5/float32(src.Width), 0.5/float32(src.Height)
	p := Prim{
		Bounds: [4]float32{0, 0, float32(w), float32(h)},
		Clip:   NoClip,
		Color:  [4]float32{1, 1, 1, 1},
		Color2: [4]float32{hx, hy, uw - hx, uh - hy},
		UV:     [4]float32{0, 0, uw, uh},
		Params: [4]float32{0, d, sigma / step},
		Kind:   KindImage,
		Flags:  FlagPremultiplied | FlagBlur,
	}
	if horizontal {
		p.Params[0], p.Params[1] = d, 0
	}
	return p
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
		r.ctx.ClearRenderTargetView(pl.src.rtv, [4]float32{})
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
	r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
	r.ctx.ClearRenderTargetView(pl.src.rtv, [4]float32{})
	x0, y0 := int(l.Bounds[0]-cur.origin[0]), int(l.Bounds[1]-cur.origin[1])
	x1, y1 := x0+int(l.Bounds[2]), y0+int(l.Bounds[3])
	cx0, cy0, cx1, cy1 := max(x0, 0), max(y0, 0), min(x1, cur.w), min(y1, cur.h)
	if cx1 > cx0 && cy1 > cy0 {
		if tex, done := cur.tex(); tex.P != nil {
			box := d3d.Box{Left: uint32(cx0), Top: uint32(cy0), Right: uint32(cx1), Bottom: uint32(cy1), Back: 1}
			r.ctx.CopySubresourceRegion(pl.src.tex, 0, uint32(cx0-x0), uint32(cy0-y0), 0, tex, 0, &box)
			done()
		}
	}
	r.blur(pl)
	r.setTarget(cur)
}

// composite draws a backdrop's blur, the prim at index at, in place of what
// is under its box rather than over it: the box is first cleared by its
// coverage and then the blur is added. Drawn over, a translucent frame, as
// under a system backdrop, would show its sharp pixels through the blur.
func (r *Renderer) composite(pl *layerPlan, at int) {
	r.ctx.OMSetBlendState(r.blendErase, [4]float32{}, 0xFFFFFFFF)
	r.drawBatch(pl.erase, pl.erase+1)
	r.ctx.OMSetBlendState(r.blendAdd, [4]float32{}, 0xFFFFFFFF)
	r.ctx.PSSetShaderResources(2, pl.out.srv)
	r.drawBatch(at, at+1)
	r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
	r.ctx.OMSetBlendState(r.blend, [4]float32{}, 0xFFFFFFFF)
}

// blur runs the two blur passes of a layer.
func (r *Renderer) blur(pl *layerPlan) {
	for _, pass := range [2]struct {
		dst, src *Target
		prim     int
	}{{pl.h1, pl.src, pl.blurH}, {pl.h2, pl.h1, pl.blurV}} {
		r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
		r.setTarget(r.targetOf(pass.dst, [2]float32{}))
		r.ctx.ClearRenderTargetView(pass.dst.rtv, [4]float32{})
		r.ctx.PSSetShaderResources(2, pass.src.srv)
		r.drawBatch(pass.prim, pass.prim+1)
	}
	r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
}

func (r *Renderer) targetOf(t *Target, origin [2]float32) renderTarget {
	return renderTarget{
		rtv:    t.rtv,
		tex:    func() (d3d.Texture2D, func()) { return t.tex, func() {} },
		w:      t.Width,
		h:      t.Height,
		origin: origin,
	}
}

func (r *Renderer) setTarget(t renderTarget) {
	r.cur = t
	r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
	r.ctx.OMSetRenderTargets([]d3d.RenderTargetView{t.rtv}, d3d.DepthStencilView{})
	r.ctx.RSSetViewports(d3d.Viewport{Width: float32(t.w), Height: float32(t.h), MaxDepth: 1})
	r.glob.Viewport = [2]float32{float32(t.w), float32(t.h)}
	r.ctx.UpdateSubresource(r.globalsBuf, nil, unsafe.Pointer(&r.glob), 0, 0)
}

// drawSeg draws prims a to b, binding each image prim's texture.
func (r *Renderer) drawSeg(s *Scene, a, b int) {
	if b <= a {
		return
	}
	refs := s.images[sort.Search(len(s.images), func(i int) bool { return s.images[i].index >= a }):]
	bound := r.emptyImage.srv
	r.ctx.PSSetShaderResources(2, bound)
	start := a
	for _, ref := range refs {
		if ref.index >= b {
			break
		}
		srv := r.emptyImage.srv
		if ref.layer > 0 {
			srv = r.plan[ref.layer-1].out.srv
		} else if ref.tex != nil {
			srv = ref.tex.shaderView()
		}
		if srv == bound {
			continue
		}
		if ref.index > start {
			r.drawBatch(start, ref.index)
			start = ref.index
		}
		bound = srv
		r.ctx.PSSetShaderResources(2, bound)
	}
	r.drawBatch(start, b)
	// Leave no app texture bound, so the app may render into it next.
	r.ctx.PSSetShaderResources(2, r.emptyImage.srv)
}

// targetPool keeps offscreen layer targets between frames.
type targetPool struct {
	free, used []*Target
}

const poolKeep = 12

// targetSize is the size of the pooled target for a w x h layer.
func targetSize(w, h int) (int, int) { return (w + 63) / 64 * 64, (h + 63) / 64 * 64 }

func (p *targetPool) acquire(d *Device, w, h int) (*Target, error) {
	w, h = targetSize(w, h)
	for i, t := range p.free {
		if t.Width == w && t.Height == h {
			p.free = append(p.free[:i], p.free[i+1:]...)
			p.used = append(p.used, t)
			return t, nil
		}
	}
	t, err := NewTarget(d, w, h)
	if err != nil {
		return nil, err
	}
	p.used = append(p.used, t)
	return t, nil
}

// endFrame returns the targets in use to the pool, keeping the most recent:
// at least those this frame used, so a frame with many backdrops does not
// create them all again the next.
func (p *targetPool) endFrame() {
	keep := max(poolKeep, len(p.used))
	p.free = append(p.free, p.used...)
	clear(p.used)
	p.used = p.used[:0]
	if n := len(p.free) - keep; n > 0 {
		for _, t := range p.free[:n] {
			t.Release()
		}
		p.free = append(p.free[:0], p.free[n:]...)
	}
}

func (p *targetPool) release() {
	p.endFrame()
	for _, t := range p.free {
		t.Release()
	}
	p.free = nil
}
